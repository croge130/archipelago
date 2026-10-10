package scheduler

import (
	"container/heap"
	"sync"
	"time"
)

// Timer is a pending callback.
type Timer interface{ Stop() bool }

// Clock is the scheduler's source of time, so tests can drive it by hand.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// ManualClock is a Clock that moves only when told to. Callbacks that come due
// run synchronously, in time order, on the goroutine calling Advance, with the
// clock set to each callback's own time while it runs.
type ManualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers timerHeap
	seq    int
}

// NewManualClock starts a manual clock at the given time.
func NewManualClock(start time.Time) *ManualClock { return &ManualClock{now: start} }

func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *ManualClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 {
		d = 0
	}
	c.seq++
	t := &manualTimer{c: c, at: c.now.Add(d), f: f, seq: c.seq}
	heap.Push(&c.timers, t)
	return t
}

// Advance moves time forward by d, running every callback that comes due.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		if len(c.timers) == 0 || c.timers[0].at.After(target) {
			c.now = target
			c.mu.Unlock()
			return
		}
		t := heap.Pop(&c.timers).(*manualTimer)
		if t.at.After(c.now) {
			c.now = t.at
		}
		t.done = true
		c.mu.Unlock()
		t.f()
	}
}

// Jump moves time forward without running anything, so callbacks that were
// due are now late; Advance(0) then runs them. It simulates a process that was
// suspended.
func (c *ManualClock) Jump(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Pending is how many callbacks are waiting.
func (c *ManualClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type manualTimer struct {
	c     *ManualClock
	at    time.Time
	f     func()
	seq   int
	index int
	done  bool
}

func (t *manualTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.done {
		return false
	}
	t.done = true
	for i, x := range t.c.timers {
		if x == t {
			heap.Remove(&t.c.timers, i)
			break
		}
	}
	return true
}

type timerHeap []*manualTimer

func (h timerHeap) Len() int { return len(h) }
func (h timerHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}
func (h timerHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *timerHeap) Push(x any)   { t := x.(*manualTimer); t.index = len(*h); *h = append(*h, t) }
func (h *timerHeap) Pop() any     { old := *h; n := len(old); t := old[n-1]; *h = old[:n-1]; return t }
