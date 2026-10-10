package recurrence

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("time zone %s is not available on this machine: %v", name, err)
	}
	return loc
}

func cal(t *testing.T, spec string, loc *time.Location) Schedule {
	t.Helper()
	s, err := Calendar(spec, loc)
	if err != nil {
		t.Fatalf("Calendar(%q): %v", spec, err)
	}
	return s
}

// nexts walks n occurrences from `from` and renders each in loc.
func nexts(s Schedule, from time.Time, n int, loc *time.Location) []string {
	var out []string
	t := from
	for i := 0; i < n; i++ {
		next, ok := s.Next(t)
		if !ok {
			out = append(out, "<none>")
			break
		}
		out = append(out, next.In(loc).Format("01-02 15:04 MST"))
		t = next
	}
	return out
}

func eq(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s:\n got %v\nwant %v", label, got, want)
	}
}

func TestEveryIsAnchoredOrRelative(t *testing.T) {
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := Every(time.Hour, anchor)
	if err != nil {
		t.Fatal(err)
	}
	for after, want := range map[time.Time]time.Time{
		anchor.Add(-time.Minute):                 anchor,
		anchor:                                   anchor.Add(time.Hour),
		anchor.Add(59 * time.Minute):             anchor.Add(time.Hour),
		anchor.Add(time.Hour):                    anchor.Add(2 * time.Hour),
		anchor.Add(1000*time.Hour + time.Second): anchor.Add(1001 * time.Hour),
	} {
		if got, ok := s.Next(after); !ok || !got.Equal(want) {
			t.Errorf("Next(%v) = %v, want %v", after.Sub(anchor), got.Sub(anchor), want.Sub(anchor))
		}
	}
	rel, _ := Every(90*time.Second, time.Time{})
	now := time.Date(2026, 5, 5, 5, 5, 5, 0, time.UTC)
	if got, _ := rel.Next(now); !got.Equal(now.Add(90 * time.Second)) {
		t.Errorf("relative Every = %v", got)
	}
	if _, err := Every(0, anchor); err == nil {
		t.Error("a zero interval was accepted")
	}
	if _, err := Every(-time.Second, anchor); err == nil {
		t.Error("a negative interval was accepted")
	}
}

func TestOnceFiresOnceAndOnlyInTheFuture(t *testing.T) {
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	s := Once(at)
	if got, ok := s.Next(at.Add(-time.Second)); !ok || !got.Equal(at) {
		t.Errorf("before: %v %v", got, ok)
	}
	if _, ok := s.Next(at); ok {
		t.Error("Once fired again from its own time")
	}
	if _, ok := s.Next(at.Add(time.Hour)); ok {
		t.Error("Once fired after it had passed")
	}
}

func TestCalendarBasicsInUTC(t *testing.T) {
	utc := time.UTC
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, utc)
	eq(t, "daily 02:30", nexts(cal(t, "30 2 * * *", utc), from, 3, utc), "01-01 02:30 UTC", "01-02 02:30 UTC", "01-03 02:30 UTC")
	eq(t, "seconds field", nexts(cal(t, "*/20 * * * * *", utc), from, 3, utc), "01-01 00:00 UTC", "01-01 00:00 UTC", "01-01 00:01 UTC")
	eq(t, "@hourly", nexts(cal(t, "@hourly", utc), from, 2, utc), "01-01 01:00 UTC", "01-01 02:00 UTC")
	eq(t, "names and steps", nexts(cal(t, "0 9-17/4 * JAN,MAR MON-FRI", utc), from, 3, utc), "01-01 09:00 UTC", "01-01 13:00 UTC", "01-01 17:00 UTC")
	// Day-of-month and day-of-week combine the standard way: either may match.
	got := Between(cal(t, "0 0 13 * 5", utc), from, time.Date(2026, 1, 31, 0, 0, 0, 0, utc), 20)
	days := map[int]bool{}
	for _, g := range got {
		days[g.Day()] = true
	}
	if !days[13] || !days[2] || !days[9] || !days[16] {
		t.Errorf("dom/dow OR semantics: got days %v", days)
	}
	if _, ok := cal(t, "0 0 31 2 *", utc).Next(from); ok {
		t.Error("the 31st of February matched")
	}
	if got, ok := cal(t, "0 0 29 2 *", utc).Next(from); !ok || got.Year() != 2028 {
		t.Errorf("leap day = %v %v, want 2028", got, ok)
	}
}

func TestCalendarRejectsWhatItMustAndNeverPanics(t *testing.T) {
	utc := time.UTC
	for _, spec := range []string{
		"", "   ", "not a cron", "61 * * * *", "* * * * * * * * *", "*/0 * * * *", "0-0-0 * * * *",
		"TZ=", "CRON_TZ=", "TZ=UTC", "CRON_TZ=UTC * * * * *", "tz=UTC * * * * *", "CRON_TZ=Nowhere/Land * * * * *",
		"@every 5m", "@EVERY 1h", "1,,2 * * * *", ",1 * * * *", "1, 2 * * * *", "1 ,2 * * * *", "1, * * * *",
		"99999999999999999999 * * * *",
	} {
		if s, err := Calendar(spec, utc); err == nil {
			t.Errorf("Calendar(%q) accepted (%v)", spec, s)
		}
	}
	if _, err := Calendar("* * * * *", nil); err == nil {
		t.Error("a nil location was accepted")
	}
	if _, err := Calendar("  0 0 * * *  ", utc); err != nil {
		t.Errorf("surrounding space rejected: %v", err)
	}
}

func TestSpringForwardFiresAtTheEndOfTheGapNotNever(t *testing.T) {
	ny := zone(t, "America/New_York")
	// 2026-03-08: 02:00 EST jumps to 03:00 EDT; 02:30 does not exist.
	from := time.Date(2026, 3, 7, 12, 0, 0, 0, ny)
	eq(t, "daily 02:30", nexts(cal(t, "30 2 * * *", ny), from, 3, ny), "03-08 03:00 EDT", "03-09 02:30 EDT", "03-10 02:30 EDT")
	// A schedule denser than the gap fires once for it, then carries on.
	eq(t, "every 15 min", nexts(cal(t, "*/15 * * * *", ny), time.Date(2026, 3, 8, 1, 30, 0, 0, ny), 5, ny),
		"03-08 01:45 EST", "03-08 03:00 EDT", "03-08 03:15 EDT", "03-08 03:30 EDT", "03-08 03:45 EDT")
	// On time slots after the gap nothing changes.
	eq(t, "daily 04:00", nexts(cal(t, "0 4 * * *", ny), from, 2, ny), "03-08 04:00 EDT", "03-09 04:00 EDT")
}

func TestFallBackFiresTheRepeatedHourOnce(t *testing.T) {
	ny := zone(t, "America/New_York")
	// 2026-11-01: 02:00 EDT falls back to 01:00 EST; 01:30 happens twice.
	from := time.Date(2026, 10, 31, 12, 0, 0, 0, ny)
	eq(t, "daily 01:30", nexts(cal(t, "30 1 * * *", ny), from, 3, ny), "11-01 01:30 EDT", "11-02 01:30 EST", "11-03 01:30 EST")
	// Hourly, wall-clock: the repeated 01:00 does not run twice.
	eq(t, "hourly", nexts(cal(t, "0 * * * *", ny), time.Date(2026, 11, 1, 0, 0, 0, 0, ny), 4, ny),
		"11-01 01:00 EDT", "11-01 02:00 EST", "11-01 03:00 EST", "11-01 04:00 EST")
}

// Asked from inside the second pass of the repeated hour, Next must not hand
// back the first pass's occurrences, which already happened.
func TestNextIsNeverAtOrBeforeWhatItWasAskedFrom(t *testing.T) {
	ny := zone(t, "America/New_York")
	s := cal(t, "50 1 * * *", ny)
	firstPass := time.Date(2026, 11, 1, 1, 40, 0, 0, ny) // 01:40 EDT
	secondPass := firstPass.Add(time.Hour)               // 01:40 EST
	if secondPass.In(ny).Format("MST") != "EST" || secondPass.Format("15:04") != "01:40" {
		t.Fatalf("test premise wrong: %v", secondPass)
	}
	got, _ := s.Next(secondPass)
	if !got.After(secondPass) {
		t.Errorf("Next(%v) = %v, not after it", secondPass, got)
	}
	if got.In(ny).Format("01-02 15:04 MST") != "11-02 01:50 EST" {
		t.Errorf("from the second pass: %v, want the next day's occurrence", got.In(ny))
	}
	if first, _ := s.Next(firstPass); first.In(ny).Format("01-02 15:04 MST") != "11-01 01:50 EDT" {
		t.Errorf("from the first pass: %v", first.In(ny))
	}
}

func TestOtherZonesThatShiftDifferently(t *testing.T) {
	// Lord Howe shifts by 30 minutes, not an hour.
	lh := zone(t, "Australia/Lord_Howe")
	// 2026-10-04 02:00 +1030 jumps to 02:30 +11: 02:15 does not exist.
	eq(t, "lord howe gap", nexts(cal(t, "15 2 * * *", lh), time.Date(2026, 10, 3, 12, 0, 0, 0, lh), 2, lh), "10-04 02:30 +11", "10-05 02:15 +11")
	// Southern hemisphere: spring-forward is in October, fall-back in April.
	syd := zone(t, "Australia/Sydney")
	eq(t, "sydney gap", nexts(cal(t, "30 2 * * *", syd), time.Date(2026, 10, 3, 12, 0, 0, 0, syd), 2, syd), "10-04 03:00 AEDT", "10-05 02:30 AEDT")
	// No DST at all, and an offset with minutes.
	kol := zone(t, "Asia/Kolkata")
	eq(t, "kolkata", nexts(cal(t, "0 9 * * *", kol), time.Date(2026, 3, 8, 0, 0, 0, 0, kol), 2, kol), "03-08 09:00 IST", "03-09 09:00 IST")
}

// The property that matters to a caller: walking forward from any starting
// point, occurrences strictly increase, and a daily calendar fires exactly
// once on every day of the year including the transition days, in several
// zones.
func TestAYearOfDailyOccurrencesIsOnePerDayAndStrictlyIncreasing(t *testing.T) {
	for _, name := range []string{"America/New_York", "Europe/London", "Australia/Sydney", "Australia/Lord_Howe", "Asia/Kolkata", "UTC", "America/St_Johns"} {
		loc := zone(t, name)
		for _, spec := range []string{"30 2 * * *", "30 1 * * *", "0 0 * * *", "59 23 * * *"} {
			s := cal(t, spec, loc)
			t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
			t1 := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
			got := Between(s, t0.Add(-time.Second), t1.Add(-time.Second), 400)
			if len(got) != 365 {
				t.Errorf("%s %q: %d occurrences in 2026, want 365", name, spec, len(got))
				continue
			}
			seen := map[string]bool{}
			for i, g := range got {
				day := g.In(loc).Format("2006-01-02")
				if seen[day] {
					t.Errorf("%s %q: fired twice on %s", name, spec, day)
				}
				seen[day] = true
				if i > 0 && !g.After(got[i-1]) {
					t.Errorf("%s %q: occurrence %d (%v) not after %v", name, spec, i, g, got[i-1])
				}
			}
		}
	}
}

func TestFromAnyPointNextIsStrictlyAfterIt(t *testing.T) {
	ny := zone(t, "America/New_York")
	for _, spec := range []string{"*/7 * * * *", "0 * * * *", "30 2 * * *", "*/30 * * * * *"} {
		s := cal(t, spec, ny)
		// Every 7 minutes, across both transition days, from every quarter hour.
		for _, day := range []time.Time{time.Date(2026, 3, 8, 0, 0, 0, 0, ny), time.Date(2026, 11, 1, 0, 0, 0, 0, ny)} {
			for i := 0; i < 24*4; i++ {
				from := day.Add(time.Duration(i) * 15 * time.Minute)
				if next, ok := s.Next(from); !ok || !next.After(from) {
					t.Fatalf("%q from %v: next %v ok=%v", spec, from, next, ok)
				}
			}
		}
	}
}

func TestBetweenListsMissedOccurrencesOldestFirstAndBounded(t *testing.T) {
	utc := time.UTC
	s := cal(t, "0 * * * *", utc)
	since := time.Date(2026, 1, 1, 0, 30, 0, 0, utc)
	until := time.Date(2026, 1, 1, 5, 0, 0, 0, utc)
	got := Between(s, since, until, 100)
	if len(got) != 5 || got[0].Hour() != 1 || got[4].Hour() != 5 {
		t.Errorf("Between = %v", got)
	}
	if got := Between(s, since, until, 2); len(got) != 2 || got[0].Hour() != 1 {
		t.Errorf("bounded Between = %v", got)
	}
	if got := Between(s, until, since, 10); len(got) != 0 {
		t.Errorf("an empty window gave %v", got)
	}
	if got := Between(Once(until.Add(time.Hour)), since, until, 10); len(got) != 0 {
		t.Errorf("a one-shot in the future inside the window: %v", got)
	}
}

func TestEveryOccurrenceHasNoNanosecondSurprises(t *testing.T) {
	loc := time.UTC
	s := cal(t, "* * * * * *", loc)
	from := time.Date(2026, 1, 1, 0, 0, 0, 999_999_999, loc)
	next, _ := s.Next(from)
	if !next.After(from) || next.Nanosecond() != 0 || !strings.HasPrefix(next.Format(time.RFC3339), "2026-01-01T00:00:01") {
		t.Errorf("Next(%v) = %v", from, next)
	}
}
