package structure

// State is Vitals' one closed, Archipelago-understood vocabulary — per
// 14-vitals-model.md, ported unchanged from Lighthouse's pkg/vitals
// since none of it is realm-shaped. A caller wanting additional domain
// state uses Value/DetailsJSON, not a new top-level state.
type State string

const (
	StateUnknown     State = "unknown"
	StateOK          State = "ok"
	StateInfo        State = "info"
	StatePending     State = "pending"
	StateStarting    State = "starting"
	StateRunning     State = "running"
	StatePaused      State = "paused"
	StateMaintenance State = "maintenance"
	StateStale       State = "stale"
	StateWarning     State = "warning"
	StateDegraded    State = "degraded"
	StateFailed      State = "failed"
	StateOffline     State = "offline"
	StateDisabled    State = "disabled"
	StateCompleted   State = "completed"
	StateCancelled   State = "cancelled"
)

func (s State) Valid() bool {
	switch s {
	case StateUnknown, StateOK, StateInfo, StatePending, StateStarting, StateRunning,
		StatePaused, StateMaintenance, StateStale, StateWarning, StateDegraded,
		StateFailed, StateOffline, StateDisabled, StateCompleted, StateCancelled:
		return true
	default:
		return false
	}
}

// Impact is optional and advisory — the current seriousness of a
// reading, as distinct from Importance (the thing's standing
// importance regardless of its current reading).
type Impact string

const (
	ImpactNone     Impact = "none"
	ImpactLow      Impact = "low"
	ImpactMedium   Impact = "medium"
	ImpactHigh     Impact = "high"
	ImpactCritical Impact = "critical"
)

func (i Impact) Valid() bool {
	switch i {
	case ImpactNone, ImpactLow, ImpactMedium, ImpactHigh, ImpactCritical:
		return true
	default:
		return false
	}
}

// Importance is how important a vital generally is, independent of its
// current reading.
type Importance string

const (
	ImportanceLow      Importance = "low"
	ImportanceNormal   Importance = "normal"
	ImportanceHigh     Importance = "high"
	ImportanceCritical Importance = "critical"
)

func (i Importance) Valid() bool {
	switch i {
	case ImportanceLow, ImportanceNormal, ImportanceHigh, ImportanceCritical:
		return true
	default:
		return false
	}
}

// MemberKind says what a GroupMember's own reference actually points
// to — exactly one of VitalInstanceID/ChildGroupID is set to match.
type MemberKind string

const (
	MemberKindVitalInstance MemberKind = "vital_instance"
	MemberKindVitalGroup    MemberKind = "vital_group"
)

func (k MemberKind) Valid() bool {
	return k == MemberKindVitalInstance || k == MemberKindVitalGroup
}
