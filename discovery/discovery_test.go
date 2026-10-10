package discovery

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

var loopback = []net.IP{net.ParseIP("127.0.0.1")}

// requireMulticast skips a test on a machine where multicast on the local
// link does not work (a locked-down container, some CI): a failure there
// says nothing about this package.
var (
	probeOnce   sync.Once
	probeReason string
)

func requireMulticast(t *testing.T) {
	t.Helper()
	probeOnce.Do(func() { probeReason = probeMulticast() })
	if probeReason != "" {
		t.Skip(probeReason)
	}
}

func probeMulticast() string {
	a, err := Announce(Announcement{Port: 7000, MinProtocol: 1, MaxProtocol: 1, Addrs: loopback})
	if err != nil {
		return fmt.Sprintf("cannot announce here: %v", err)
	}
	defer a.Close()
	time.Sleep(100 * time.Millisecond)
	got, err := Browse(context.Background(), BrowseOptions{Window: time.Second, Queries: 2})
	if err != nil || len(got) == 0 {
		return fmt.Sprintf("multicast does not work on this machine (found %d, err %v)", len(got), err)
	}
	return ""
}

// forge announces something a hostile or buggy peer might, bypassing
// Announce's own validation.
func forge(t *testing.T, instance string, port int, txt ...string) {
	t.Helper()
	svc, err := mdns.NewMDNSService(instance, Service, "", instance+".local.", port, loopback, txt)
	if err != nil {
		t.Fatalf("forge %s: %v", instance, err)
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("forge server: %v", err)
	}
	t.Cleanup(func() { srv.Shutdown() })
}

func browse(t *testing.T, opts BrowseOptions) []Candidate {
	t.Helper()
	if opts.Window == 0 {
		opts.Window = 1200 * time.Millisecond
	}
	if opts.Queries == 0 {
		opts.Queries = 3
	}
	got, err := Browse(context.Background(), opts)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	return got
}

func byInstance(cs []Candidate, instance string) (Candidate, bool) {
	for _, c := range cs {
		if c.Instance == instance {
			return c, true
		}
	}
	return Candidate{}, false
}

func TestAnAnnouncementIsFoundWithItsPortAddressAndProtocolRange(t *testing.T) {
	requireMulticast(t)
	a, err := Announce(Announcement{Port: 7443, MinProtocol: 1, MaxProtocol: 3, Addrs: loopback})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	time.Sleep(100 * time.Millisecond)

	c, ok := byInstance(browse(t, BrowseOptions{}), a.Instance())
	if !ok {
		t.Fatalf("the announcement %q was not found", a.Instance())
	}
	if c.Port != 7443 || c.MinProtocol != 1 || c.MaxProtocol != 3 {
		t.Errorf("candidate = %+v", c)
	}
	if aps := c.AddrPorts(); len(aps) != 1 || aps[0].String() != "127.0.0.1:7443" {
		t.Errorf("AddrPorts = %v", aps)
	}
	if !c.Speaks(3, 5) || !c.Speaks(1, 1) || c.Speaks(4, 9) {
		t.Errorf("Speaks gave the wrong overlap answers for 1-3: %v %v %v", c.Speaks(3, 5), c.Speaks(1, 1), c.Speaks(4, 9))
	}
}

func TestSeveralListenersOnOneNodeAreSeparateCandidates(t *testing.T) {
	requireMulticast(t)
	var names []string
	for _, port := range []int{7001, 7002, 7003} {
		a, err := Announce(Announcement{Port: port, MinProtocol: 1, MaxProtocol: 1, Addrs: loopback})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		names = append(names, a.Instance())
	}
	time.Sleep(100 * time.Millisecond)
	got := browse(t, BrowseOptions{})
	var ports []int
	for _, n := range names {
		c, ok := byInstance(got, n)
		if !ok {
			t.Fatalf("listener %s was not found among %d candidates", n, len(got))
		}
		ports = append(ports, c.Port)
	}
	sort.Ints(ports)
	if fmt.Sprint(ports) != "[7001 7002 7003]" {
		t.Errorf("ports = %v", ports)
	}
}

// 18's rule: the announcement says the protocol versions and nothing else, so
// a passive observer on the link learns nothing about the node's domains.
func TestTheAnnouncementCarriesNothingButTheProtocolRange(t *testing.T) {
	requireMulticast(t)
	a, err := Announce(Announcement{Port: 7443, MinProtocol: 1, MaxProtocol: 2, Addrs: loopback})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	time.Sleep(100 * time.Millisecond)

	// Look at the raw answer with the library directly, not through Browse.
	entries := make(chan *mdns.ServiceEntry, 16)
	_ = mdns.Query(&mdns.QueryParam{Service: Service, Timeout: time.Second, Entries: entries, DisableIPv6: true, Logger: log.New(io.Discard, "", 0)})
	close(entries)
	found := false
	for e := range entries {
		if !strings.HasPrefix(e.Name, a.Instance()+".") {
			continue
		}
		found = true
		if len(e.InfoFields) != 1 || e.InfoFields[0] != "proto=1-2" {
			t.Errorf("TXT = %q, want exactly [proto=1-2]", e.InfoFields)
		}
		if !regexp.MustCompile(`^archipelago-[0-9a-f]{8}$`).MatchString(a.Instance()) {
			t.Errorf("instance name %q is not a random opaque name", a.Instance())
		}
	}
	if !found {
		t.Fatal("did not see our own announcement")
	}
}

func TestMalformedAndForgedAnnouncementsAreDiscarded(t *testing.T) {
	requireMulticast(t)
	good, err := Announce(Announcement{Port: 7100, MinProtocol: 1, MaxProtocol: 1, Addrs: loopback})
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	forge(t, "no-proto", 7101, "something=else")
	forge(t, "no-txt", 7102)
	forge(t, "garbage-proto", 7103, "proto=banana")
	forge(t, "inverted-proto", 7104, "proto=5-2")
	forge(t, "zero-proto", 7105, "proto=0-1")
	forge(t, "huge-proto", 7106, "proto=1-999999999")
	forge(t, "fine-with-extras", 7107, "x=1", "proto=2-4", "y=2") // unknown keys are ignored
	time.Sleep(150 * time.Millisecond)

	got := browse(t, BrowseOptions{})
	names := map[string]bool{}
	for _, c := range got {
		names[c.Instance] = true
	}
	for _, bad := range []string{"no-proto", "no-txt", "garbage-proto", "inverted-proto", "zero-proto", "huge-proto"} {
		if names[bad] {
			t.Errorf("a malformed announcement %q was returned as a candidate", bad)
		}
	}
	if !names[good.Instance()] {
		t.Error("the well-formed announcement was lost among the forged ones")
	}
	if c, ok := byInstance(got, "fine-with-extras"); !ok || c.MinProtocol != 2 || c.MaxProtocol != 4 {
		t.Errorf("an announcement with unknown extra keys: %+v ok=%v; want it accepted with proto 2-4", c, ok)
	}
}

func TestAFloodOfAnnouncementsIsBounded(t *testing.T) {
	requireMulticast(t)
	for i := 0; i < 8; i++ {
		forge(t, fmt.Sprintf("flood-%d", i), 7200+i, "proto=1-1")
	}
	time.Sleep(150 * time.Millisecond)
	got := browse(t, BrowseOptions{MaxCandidates: 3})
	if len(got) != 3 {
		t.Errorf("kept %d candidates with a limit of 3", len(got))
	}
}

// The library keeps listening until its own timeout even after its context
// is cancelled; Browse must not.
func TestBrowseReturnsWhenItsContextEndsNotWhenTheWindowDoes(t *testing.T) {
	requireMulticast(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Browse(ctx, BrowseOptions{Window: 10 * time.Second, Queries: 1})
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("Browse took %v with a 300ms context deadline", took)
	}
	if err != nil {
		t.Errorf("a deadline is the end of the window, not an error: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel2() }()
	start = time.Now()
	_, err = Browse(ctx2, BrowseOptions{Window: 10 * time.Second, Queries: 1})
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("Browse took %v after the context was cancelled", took)
	}
	if err != context.Canceled {
		t.Errorf("a cancellation should be reported: %v", err)
	}
}

func TestAnAnnouncerThatClosedIsNotFoundAgain(t *testing.T) {
	requireMulticast(t)
	a, err := Announce(Announcement{Port: 7300, MinProtocol: 1, MaxProtocol: 1, Addrs: loopback})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok := byInstance(browse(t, BrowseOptions{}), a.Instance()); !ok {
		t.Fatal("not found while announcing")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := byInstance(browse(t, BrowseOptions{}), a.Instance()); ok {
		t.Error("still found after Close")
	}
}

func TestFoundIsCalledOncePerNewCandidate(t *testing.T) {
	requireMulticast(t)
	a, _ := Announce(Announcement{Port: 7400, MinProtocol: 1, MaxProtocol: 1, Addrs: loopback})
	defer a.Close()
	time.Sleep(100 * time.Millisecond)
	count := 0
	got := browse(t, BrowseOptions{Queries: 3, Found: func(c Candidate) {
		if c.Instance == a.Instance() {
			count++
		}
	}})
	if _, ok := byInstance(got, a.Instance()); !ok || count != 1 {
		t.Errorf("found=%v, Found called %d times for it, want exactly 1 across 3 queries", ok, count)
	}
}

func TestAnnounceRejectsWhatCannotBeAnnounced(t *testing.T) {
	for name, a := range map[string]Announcement{
		"no port":        {Port: 0, MinProtocol: 1, MaxProtocol: 1},
		"port too big":   {Port: 70000, MinProtocol: 1, MaxProtocol: 1},
		"zero min":       {Port: 7000, MinProtocol: 0, MaxProtocol: 1},
		"inverted range": {Port: 7000, MinProtocol: 3, MaxProtocol: 2},
	} {
		if x, err := Announce(a); err == nil {
			x.Close()
			t.Errorf("%s: Announce accepted %+v", name, a)
		}
	}
}

func TestParseProtoIsStrict(t *testing.T) {
	for in, want := range map[string]bool{
		"proto=1-1": true, "proto=2-9": true, "proto=1": false, "proto=a-b": false, "proto=2-1": false,
		"proto=0-1": false, "proto=-1-2": false, "proto=1-65536": false, "proto=": false, "protox=1-1": false,
	} {
		_, _, ok := parseProto([]string{in})
		if ok != want {
			t.Errorf("parseProto(%q) ok=%v, want %v", in, ok, want)
		}
	}
	// Only the first few fields are looked at, and oversized ones are skipped.
	many := make([]string, 0, 12)
	for i := 0; i < 10; i++ {
		many = append(many, "k=v")
	}
	many = append(many, "proto=1-1")
	if _, _, ok := parseProto(many); ok {
		t.Error("a proto key beyond the field limit was read")
	}
	if _, _, ok := parseProto([]string{"proto=1-1" + strings.Repeat("0", 300)}); ok {
		t.Error("an oversized field was read")
	}
}

// A hostile raw packet can carry what the library's own announcer refuses
// to build, so those cases are checked against the parser directly.
func TestParseEntryRejectsWhatNoHonestAnnouncerSends(t *testing.T) {
	good := func() *mdns.ServiceEntry {
		return &mdns.ServiceEntry{
			Name: "archipelago-aaaa._archipelago._tcp.local.", Port: 7000,
			AddrV4: net.ParseIP("10.0.0.5"), InfoFields: []string{"proto=1-1"},
		}
	}
	if _, ok := parseEntry(good()); !ok {
		t.Fatal("the baseline entry was rejected")
	}
	for name, mutate := range map[string]func(*mdns.ServiceEntry){
		"port zero":        func(e *mdns.ServiceEntry) { e.Port = 0 },
		"port too big":     func(e *mdns.ServiceEntry) { e.Port = 70000 },
		"unspecified addr": func(e *mdns.ServiceEntry) { e.AddrV4 = net.IPv4zero },
		"multicast addr":   func(e *mdns.ServiceEntry) { e.AddrV4 = net.ParseIP("224.0.0.251") },
		"broadcast addr":   func(e *mdns.ServiceEntry) { e.AddrV4 = net.IPv4bcast },
		"no address":       func(e *mdns.ServiceEntry) { e.AddrV4 = nil },
		"wrong service":    func(e *mdns.ServiceEntry) { e.Name = "x._http._tcp.local." },
		"empty instance":   func(e *mdns.ServiceEntry) { e.Name = "._archipelago._tcp.local." },
		"long instance":    func(e *mdns.ServiceEntry) { e.Name = strings.Repeat("a", 64) + "._archipelago._tcp.local." },
	} {
		e := good()
		mutate(e)
		if c, ok := parseEntry(e); ok {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
}
