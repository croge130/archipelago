package scheduler

import (
	"context"
	"sync"
	"time"
)

// Record is the catch-up record: the last scheduled slot each duty finished,
// by node role and duty. It is a small interface because the durable
// implementation belongs to whatever storage a deployment has; the default
// is in memory, which gives a node with no storage "skip" semantics because
// it cannot know what it missed.
type Record interface {
	LastRun(ctx context.Context, role, duty string) (slot time.Time, found bool, err error)
	SetLastRun(ctx context.Context, role, duty string, slot time.Time) error
}

// MemoryRecord is the in-memory Record.
type MemoryRecord struct {
	mu sync.Mutex
	m  map[[2]string]time.Time
}

// NewMemoryRecord creates an empty in-memory record.
func NewMemoryRecord() *MemoryRecord { return &MemoryRecord{m: map[[2]string]time.Time{}} }

func (r *MemoryRecord) LastRun(_ context.Context, role, duty string) (time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.m[[2]string{role, duty}]
	return t, ok, nil
}

func (r *MemoryRecord) SetLastRun(_ context.Context, role, duty string, slot time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := [2]string{role, duty}
	if slot.After(r.m[k]) {
		r.m[k] = slot
	}
	return nil
}
