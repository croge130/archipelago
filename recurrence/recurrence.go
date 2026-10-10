package recurrence

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule yields occurrences in increasing order.
type Schedule interface {
	// Next is the first occurrence strictly after the given time, or false if
	// there is none (a one-shot already past, or a calendar that can never
	// match, like the 31st of February).
	Next(after time.Time) (time.Time, bool)
}

// Between lists the occurrences in (since, until], oldest first, stopping at
// max. It is how a caller works out what it missed while it was not running.
func Between(s Schedule, since, until time.Time, max int) []time.Time {
	var out []time.Time
	t := since
	for len(out) < max {
		next, ok := s.Next(t)
		if !ok || next.After(until) {
			break
		}
		out = append(out, next)
		t = next
	}
	return out
}

// Every fires at a fixed interval of real elapsed time. With a non-zero
// anchor the occurrences are anchor + k*every, so they do not drift or shift
// when the process restarts; with a zero anchor each is every after the time
// asked from.
func Every(every time.Duration, anchor time.Time) (Schedule, error) {
	if every <= 0 {
		return nil, errors.New("recurrence: an interval must be positive")
	}
	return interval{every: every, anchor: anchor}, nil
}

type interval struct {
	every  time.Duration
	anchor time.Time
}

func (i interval) Next(after time.Time) (time.Time, bool) {
	if i.anchor.IsZero() {
		return after.Add(i.every), true
	}
	if after.Before(i.anchor) {
		return i.anchor, true
	}
	k := after.Sub(i.anchor)/i.every + 1
	return i.anchor.Add(k * i.every), true
}

// Once fires a single time.
func Once(at time.Time) Schedule { return once{at: at} }

type once struct{ at time.Time }

func (o once) Next(after time.Time) (time.Time, bool) {
	if o.at.After(after) {
		return o.at, true
	}
	return time.Time{}, false
}

// Calendar parses a cron expression to be matched on the wall clock of loc.
//
// Accepted: five fields (minute hour day-of-month month day-of-week), or six
// with a leading seconds field, with lists, ranges, steps and month/weekday
// names; and the descriptors @yearly, @monthly, @weekly, @daily, @hourly.
// Day-of-month and day-of-week combine the standard way: if both are
// restricted, a day matching either fires. Not accepted: "@every" (use Every)
// and any "TZ=" or "CRON_TZ=" prefix (the zone is the loc argument, and the
// cron library panics on malformed prefixes).
func Calendar(spec string, loc *time.Location) (Schedule, error) {
	if loc == nil {
		return nil, errors.New("recurrence: a calendar schedule needs a location")
	}
	trimmed := strings.TrimSpace(spec)
	upper := strings.ToUpper(trimmed)
	switch {
	case trimmed == "":
		return nil, errors.New("recurrence: empty calendar expression")
	case strings.HasPrefix(upper, "TZ=") || strings.HasPrefix(upper, "CRON_TZ="):
		return nil, errors.New("recurrence: put the time zone in the location, not in the expression")
	case strings.HasPrefix(upper, "@EVERY"):
		return nil, errors.New("recurrence: use Every for fixed intervals")
	case strings.Contains(trimmed, ",,") || strings.HasPrefix(trimmed, ",") || strings.Contains(trimmed, ", ") || strings.Contains(trimmed, " ,") || strings.HasSuffix(trimmed, ","):
		return nil, errors.New("recurrence: empty element in a list")
	}
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	sched, err := safeParse(parser, trimmed)
	if err != nil {
		return nil, fmt.Errorf("recurrence: %w", err)
	}
	return calendar{civil: sched, loc: loc}, nil
}

// safeParse guards the library: it is unmaintained and known to panic on
// some malformed input, and a bad expression must be an error, not a crash.
func safeParse(p cron.Parser, spec string) (s cron.Schedule, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("unparseable expression (%v)", r)
		}
	}()
	return p.Parse(spec)
}

type calendar struct {
	civil cron.Schedule
	loc   *time.Location
}

// maxCivilSteps bounds the search past occurrences that map to or before the
// time asked from (the repeated hour of a fall-back, at one-second density).
const maxCivilSteps = 20000

func (c calendar) Next(after time.Time) (time.Time, bool) {
	// Work in civil time: the wall clock in loc, labelled UTC so the library
	// sees a calendar with no daylight saving in it.
	civil := toCivil(after.In(c.loc))
	for i := 0; i < maxCivilSteps; i++ {
		n := c.civil.Next(civil)
		if n.IsZero() {
			return time.Time{}, false
		}
		// Mapping a repeated civil time to its first occurrence can land at or
		// before `after` when asked from inside the repeat; that occurrence
		// has already happened, so move on.
		if inst := fromCivil(n, c.loc); inst.After(after) {
			return inst, true
		}
		civil = n
	}
	return time.Time{}, false
}

// toCivil relabels t's wall-clock fields as UTC.
func toCivil(t time.Time) time.Time {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, mo, d, h, mi, s, t.Nanosecond(), time.UTC)
}

// fromCivil maps a civil time to a real instant in loc under the package's DST
// rule: the first of two if it happens twice, the first instant after the gap
// if it never happens.
func fromCivil(civil time.Time, loc *time.Location) time.Time {
	// The zone offsets in force a day and a half either side bracket any
	// transition near this civil time; trying each and keeping those that
	// really read back as this civil time finds one, two, or no instants.
	around := time.Date(civil.Year(), civil.Month(), civil.Day(), 12, 0, 0, 0, loc)
	lo, hi := around.Add(-36*time.Hour), around.Add(36*time.Hour)
	_, before := lo.Zone()
	_, after := hi.Zone()

	var found []time.Time
	for _, off := range []int{before, after} {
		inst := civil.Add(-time.Duration(off) * time.Second).In(loc)
		if toCivil(inst).Equal(civil) {
			dup := false
			for _, f := range found {
				dup = dup || f.Equal(inst)
			}
			if !dup {
				found = append(found, inst)
			}
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 2:
		if found[0].Before(found[1]) {
			return found[0]
		}
		return found[1]
	}
	return transitionAfter(lo, hi, before, loc)
}

// transitionAfter finds the instant in (lo, hi] at which loc's offset stops
// being `before`, by bisection to the second: the end of a gap.
func transitionAfter(lo, hi time.Time, before int, loc *time.Location) time.Time {
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if _, off := mid.In(loc).Zone(); off == before {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi.In(loc)
}
