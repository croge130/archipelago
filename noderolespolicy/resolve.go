package noderolespolicy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/croge130/archipelago/governor"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/policy/evaluation"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
)

// Cap records a budget dimension where policy asked for more than the node
// adopted the node role at.
type Cap struct {
	Dimension string
	Asked     string
	Allowed   string
}

func (c Cap) String() string {
	return fmt.Sprintf("%s: policy asked %s, the node adopted %s", c.Dimension, c.Asked, c.Allowed)
}

// DutyChoice is what policy says about one duty.
type DutyChoice struct {
	// Interval is the effective interval policy sets; zero means policy says
	// nothing about it (or it was not applicable; see Note).
	Interval time.Duration
	// Requested is what policy asked for before the duty's floor applied.
	Requested time.Duration
	// Floored reports that Requested was below the duty's MinInterval.
	Floored bool
	// Enabled is nil when policy is silent.
	Enabled *bool
	// Note explains a policy value that could not be applied.
	Note string
}

// Resolved is the policy outcome for one adopted node role, before anything
// is applied.
type Resolved struct {
	Role string
	// Budget is what the node role should now ask the governor for: the
	// adopted budget with policy applied, capped at what was adopted.
	Budget governor.Limits
	// FromPolicy reports whether any budget key applied.
	FromPolicy bool
	Capped     []Cap
	Duties     map[string]DutyChoice
	// Sources names, per policy key and scope, the instances that fed the
	// value, for explaining a surprising result.
	Sources map[string][]uuid.UUID
}

// Resolver reads policy for adopted node roles.
type Resolver struct {
	store  evaluation.Store
	domain string
}

// NewResolver reads from store. domain, if set, is the domain whose policy
// applies; policy for other domains is ignored.
func NewResolver(store evaluation.Store, domain string) *Resolver {
	return &Resolver{store: store, domain: domain}
}

func (rs *Resolver) refs(role, duty string) []structure.Ref {
	var refs []structure.Ref
	if rs.domain != "" {
		refs = append(refs, structure.Ref{Kind: RefDomain, Key: rs.domain})
	}
	refs = append(refs, structure.Ref{Kind: RefRole, Key: role})
	if duty != "" {
		refs = append(refs, structure.Ref{Kind: RefDuty, Key: role + "/" + duty})
	}
	return refs
}

// Resolve computes what policy says about a node role and each of its duties.
func (rs *Resolver) Resolve(ctx context.Context, a *noderoles.Adopted) (Resolved, error) {
	key := a.Key()
	base := a.Base()
	out := Resolved{Role: key, Budget: base, Duties: map[string]DutyChoice{}, Sources: map[string][]uuid.UUID{}}

	get := func(policyKey, subject string, refs []structure.Ref) (any, bool, error) {
		res, found, err := evaluation.Resolve(ctx, rs.store, policyKey, refs)
		if err != nil {
			return nil, false, fmt.Errorf("noderolespolicy: resolving %s for %s: %w", policyKey, subject, err)
		}
		if found {
			out.Sources[policyKey+"@"+subject] = res.ContributingInstanceIDs
		}
		return res.Value, found, nil
	}
	roleRefs := rs.refs(key, "")

	if v, found, err := get(KeyConcurrency, key, roleRefs); err != nil {
		return Resolved{}, err
	} else if found {
		asked := int(asInt(v))
		out.FromPolicy = true
		if asked > base.Concurrency {
			out.Capped = append(out.Capped, Cap{"concurrency", fmt.Sprint(asked), fmt.Sprint(base.Concurrency)})
			asked = base.Concurrency
		}
		out.Budget.Concurrency = asked
	}
	if v, found, err := get(KeyQueueDepth, key, roleRefs); err != nil {
		return Resolved{}, err
	} else if found {
		asked := int(asInt(v))
		out.FromPolicy = true
		if asked > base.QueueDepth {
			out.Capped = append(out.Capped, Cap{"queue depth", fmt.Sprint(asked), fmt.Sprint(base.QueueDepth)})
			asked = base.QueueDepth
		}
		out.Budget.QueueDepth = asked
	}
	if v, found, err := get(KeyRunTimeout, key, roleRefs); err != nil {
		return Resolved{}, err
	} else if found {
		asked := asDuration(v)
		out.FromPolicy = true
		// A zero adopted timeout means none, so any timeout is tighter.
		if base.RunTimeout > 0 && asked > base.RunTimeout {
			out.Capped = append(out.Capped, Cap{"run timeout", asked.String(), base.RunTimeout.String()})
			asked = base.RunTimeout
		}
		out.Budget.RunTimeout = asked
	}

	for _, d := range a.Definition().Duties {
		refs := rs.refs(key, d.Name)
		var c DutyChoice
		if v, found, err := get(KeyScheduleInterval, key+"/"+d.Name, refs); err != nil {
			return Resolved{}, err
		} else if found {
			asked := asDuration(v)
			switch {
			case a.DefaultSchedule(d.Name) == nil:
				c.Note = fmt.Sprintf("interval %s ignored: duty %q is driven by events, not a schedule", asked, d.Name)
			default:
				c.Requested, c.Interval = asked, asked
				if d.MinInterval > 0 && asked < d.MinInterval {
					c.Interval, c.Floored = d.MinInterval, true
				}
			}
		}
		if v, found, err := get(KeyScheduleEnabled, key+"/"+d.Name, refs); err != nil {
			return Resolved{}, err
		} else if found {
			on, ok := v.(bool)
			if !ok {
				return Resolved{}, fmt.Errorf("noderolespolicy: %s for %s/%s resolved to %T, not a bool", KeyScheduleEnabled, key, d.Name, v)
			}
			c.Enabled = &on
		}
		out.Duties[d.Name] = c
	}
	return out, nil
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(math.Round(n))
	}
	return 0
}

func asDuration(v any) time.Duration {
	switch n := v.(type) {
	case float64:
		return time.Duration(math.Round(n * float64(time.Second)))
	case int64:
		return time.Duration(n) * time.Second
	case int:
		return time.Duration(n) * time.Second
	}
	return 0
}
