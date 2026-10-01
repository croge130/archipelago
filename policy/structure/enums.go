package structure

// Lifecycle is shared by PolicyDefinition and PolicyInstance. Unlike
// alias's Lifecycle (active/released — a slot that can be reused),
// archived here is terminal bookkeeping: a definition or instance
// that's done being live authority, not a name waiting to be
// reactivated.
type Lifecycle string

const (
	LifecycleActive   Lifecycle = "active"
	LifecycleArchived Lifecycle = "archived"
)

func (l Lifecycle) Valid() bool {
	return l == LifecycleActive || l == LifecycleArchived
}

// ActivationMode says when a resolved value change takes effect.
// Renamed from Lighthouse's new_sessions_only to Deferred specifically
// because Policy can't reference Gatehouse-core's Session to define
// "new" against — the consumer integration decides what counts as a
// fresh acquisition worth picking up the new value.
type ActivationMode string

const (
	ActivationImmediate ActivationMode = "immediate"
	ActivationDeferred  ActivationMode = "deferred"
)

func (m ActivationMode) Valid() bool {
	return m == ActivationImmediate || m == ActivationDeferred
}

// BindingMode says whether a consumer that read a resolved value
// keeps tracking live changes to it, snapshotted it once at creation,
// or was given an instance-level override that ignores both. Carried
// forward from Lighthouse unchanged — nothing about live-vs-copied
// resolution tracking is realm-shaped.
type BindingMode string

const (
	BindingInheritedLive    BindingMode = "inherited_live"
	BindingCopiedAtCreation BindingMode = "copied_at_creation"
	BindingExplicitOverride BindingMode = "explicit_override"
)

func (m BindingMode) Valid() bool {
	switch m {
	case BindingInheritedLive, BindingCopiedAtCreation, BindingExplicitOverride:
		return true
	default:
		return false
	}
}

// TargetKind is what a PolicyInstance attaches to — the replacement
// for Lighthouse's whole SecurityContext containment tree, per
// 10-typedvalue-and-policy-model.md.
type TargetKind string

const (
	TargetKindGlobal        TargetKind = "global"
	TargetKindRef           TargetKind = "ref"
	TargetKindPolicyContext TargetKind = "policy_context"
)

func (k TargetKind) Valid() bool {
	switch k {
	case TargetKindGlobal, TargetKindRef, TargetKindPolicyContext:
		return true
	default:
		return false
	}
}
