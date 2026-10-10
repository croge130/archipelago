package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// Limits on what a hostile link can make Browse hold or parse.
const (
	defaultWindow        = 2 * time.Second
	defaultQueries       = 3
	defaultMaxCandidates = 64
	maxAddrsPerCandidate = 8
	maxTXTFields         = 8
	maxTXTFieldLen       = 255
	maxInstanceLen       = 63
)

// Candidate is a peer an announcement claims exists. It is a hint: see the
// package comment.
type Candidate struct {
	Instance                 string
	Addrs                    []netip.Addr
	Port                     int
	MinProtocol, MaxProtocol int
}

// AddrPorts are the addresses to try, with the announced port.
func (c Candidate) AddrPorts() []netip.AddrPort {
	out := make([]netip.AddrPort, len(c.Addrs))
	for i, a := range c.Addrs {
		out[i] = netip.AddrPortFrom(a, uint16(c.Port))
	}
	return out
}

// Speaks reports whether this candidate's protocol range overlaps
// [min, max], i.e. whether a handshake between the two could succeed.
func (c Candidate) Speaks(min, max int) bool {
	return c.MinProtocol <= max && min <= c.MaxProtocol
}

// BrowseOptions configures Browse. The zero value is usable.
type BrowseOptions struct {
	// Window is how long to listen (default 2s). A context deadline that
	// comes sooner shortens it.
	Window time.Duration
	// Queries is how many times the question is asked within the window
	// (default 3); one lost packet then does not hide a peer.
	Queries int
	// MaxCandidates bounds what is kept (default 64), so a flood of forged
	// announcements cannot grow memory without limit.
	MaxCandidates int
	// Interface restricts multicast to one interface.
	Interface *net.Interface
	// Found, if set, is called once per new candidate as it is heard,
	// from Browse's own goroutine.
	Found func(Candidate)

	Logger *slog.Logger
}

// Browse asks the local link for Archipelago peers and returns the distinct
// well-formed candidates heard within the window. It returns early, with
// what it has, when ctx ends, and returns ctx's error only for a
// cancellation (a deadline is simply the end of the window).
func Browse(ctx context.Context, opts BrowseOptions) ([]Candidate, error) {
	if opts.Window <= 0 {
		opts.Window = defaultWindow
	}
	if opts.Queries <= 0 {
		opts.Queries = defaultQueries
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = defaultMaxCandidates
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	deadline := time.Now().Add(opts.Window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	per := time.Until(deadline) / time.Duration(opts.Queries)
	if per < 50*time.Millisecond {
		per = 50 * time.Millisecond
	}

	entries := make(chan *mdns.ServiceEntry, 256)
	stop := make(chan struct{})
	collected := newCollection(opts.MaxCandidates, log)
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for {
			select {
			case e := <-entries:
				if c, ok := collected.add(e); ok && opts.Found != nil {
					opts.Found(c)
				}
			case <-stop:
				return
			}
		}
	}()

	// The library's query loop only ends on its own timer, even when its
	// context is cancelled, so each query runs in a goroutine we can walk
	// away from. A query we abandon finishes within its own timeout and its
	// late sends are non-blocking, so nothing is held up or leaked beyond it.
	var queriesErr error
	var qmu sync.Mutex
loop:
	for i := 0; i < opts.Queries && time.Until(deadline) > 0; i++ {
		timeout := min(per, time.Until(deadline))
		done := make(chan error, 1)
		go func() {
			done <- mdns.Query(&mdns.QueryParam{
				Service: Service, Timeout: timeout, Entries: entries, Interface: opts.Interface,
				DisableIPv6: false, Logger: stdLogger(opts.Logger),
			})
		}()
		select {
		case err := <-done:
			if err != nil {
				qmu.Lock()
				queriesErr = errors.Join(queriesErr, err)
				qmu.Unlock()
			}
		case <-ctx.Done():
			break loop
		}
	}
	// Let the last responses in the channel be read before stopping.
	if ctx.Err() == nil {
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	<-collectorDone

	out := collected.list()
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return out, ctx.Err()
	case len(out) == 0 && queriesErr != nil:
		return nil, fmt.Errorf("discovery: browse: %w", queriesErr)
	}
	return out, nil
}

type collection struct {
	mu    sync.Mutex
	max   int
	order []string
	byKey map[string]*Candidate
	log   *slog.Logger
}

func newCollection(max int, log *slog.Logger) *collection {
	return &collection{max: max, byKey: map[string]*Candidate{}, log: log}
}

// add validates an entry and merges it in. The first sighting of an
// instance fixes its port and protocol range; later sightings may only add
// addresses (an instance is announced over IPv4 and IPv6 separately). It
// reports a candidate only the first time the instance appears.
func (c *collection) add(e *mdns.ServiceEntry) (Candidate, bool) {
	cand, ok := parseEntry(e)
	if !ok {
		c.log.Debug("discarded a malformed announcement", "name", truncate(e.Name, 80))
		return Candidate{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if have, exists := c.byKey[cand.Instance]; exists {
		for _, a := range cand.Addrs {
			if len(have.Addrs) < maxAddrsPerCandidate && !containsAddr(have.Addrs, a) {
				have.Addrs = append(have.Addrs, a)
			}
		}
		return Candidate{}, false
	}
	if len(c.byKey) >= c.max {
		c.log.Debug("candidate limit reached; ignoring further announcements", "limit", c.max)
		return Candidate{}, false
	}
	c.byKey[cand.Instance] = &cand
	c.order = append(c.order, cand.Instance)
	return cand, true
}

func (c *collection) list() []Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Candidate, 0, len(c.order))
	for _, k := range c.order {
		cp := *c.byKey[k]
		cp.Addrs = append([]netip.Addr(nil), cp.Addrs...)
		out = append(out, cp)
	}
	return out
}

func containsAddr(list []netip.Addr, a netip.Addr) bool {
	for _, x := range list {
		if x == a {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// parseEntry turns a heard entry into a Candidate, or reports that it is not
// one: no protocol range, an impossible port, no usable address.
func parseEntry(e *mdns.ServiceEntry) (Candidate, bool) {
	suffix := "." + Service + ".local."
	if !strings.HasSuffix(e.Name, suffix) {
		return Candidate{}, false
	}
	instance := strings.TrimSuffix(e.Name, suffix)
	if instance == "" || len(instance) > maxInstanceLen {
		return Candidate{}, false
	}
	if e.Port < 1 || e.Port > 65535 {
		return Candidate{}, false
	}
	minV, maxV, ok := parseProto(e.InfoFields)
	if !ok {
		return Candidate{}, false
	}
	var addrs []netip.Addr
	add := func(ip net.IP, zone string) {
		a, ok := netip.AddrFromSlice(ip)
		if !ok {
			return
		}
		a = a.Unmap()
		if a.IsUnspecified() || a.IsMulticast() || (a.Is4() && a.As4() == [4]byte{255, 255, 255, 255}) {
			return
		}
		if zone != "" && a.Is6() {
			a = a.WithZone(zone)
		}
		if len(addrs) < maxAddrsPerCandidate && !containsAddr(addrs, a) {
			addrs = append(addrs, a)
		}
	}
	add(e.AddrV4, "")
	if e.AddrV6IPAddr != nil {
		add(e.AddrV6IPAddr.IP, e.AddrV6IPAddr.Zone)
	} else {
		add(e.AddrV6, "")
	}
	if len(addrs) == 0 {
		return Candidate{}, false
	}
	return Candidate{Instance: instance, Addrs: addrs, Port: e.Port, MinProtocol: minV, MaxProtocol: maxV}, true
}

// parseProto finds the proto key among the TXT fields, looking at no more
// than maxTXTFields of at most maxTXTFieldLen bytes each.
func parseProto(fields []string) (minV, maxV int, ok bool) {
	for i, f := range fields {
		if i >= maxTXTFields {
			break
		}
		if len(f) > maxTXTFieldLen {
			continue
		}
		v, found := strings.CutPrefix(f, protoKey+"=")
		if !found {
			continue
		}
		lo, hi, cut := strings.Cut(v, "-")
		if !cut {
			return 0, 0, false
		}
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || a < 1 || b < a || b > 65535 {
			return 0, 0, false
		}
		return a, b, true
	}
	return 0, 0, false
}
