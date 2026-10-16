package noderolespolicy

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/policy/facade"
	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
)

// The policy keys. They live in the reserved archipelago namespace.
const (
	KeyConcurrency      = "archipelago.noderoles.budget.concurrency"
	KeyQueueDepth       = "archipelago.noderoles.budget.queue_depth"
	KeyRunTimeout       = "archipelago.noderoles.budget.run_timeout"
	KeyScheduleInterval = "archipelago.noderoles.schedule.interval"
	KeyScheduleEnabled  = "archipelago.noderoles.schedule.enabled"
)

// runKind is the count kind concurrency and queue depth are measured in.
const runKind = "run"

func init() {
	// Idempotent; every process that reads or writes these definitions needs
	// the kind registered to validate them.
	if err := typedvalue.RegisterCountKind(runKind, false); err != nil {
		panic(fmt.Sprintf("noderolespolicy: registering count kind %q: %v", runKind, err))
	}
}

// Spec is one policy definition this package owns.
type Spec struct {
	Key         string
	ValueType   typedvalue.Definition
	Constraints typeconstraints.Set
	Merge       typeconstraints.MergeMode
}

func ptr[T any](v T) *T { return &v }

// Specs lists the definitions, in a stable order.
func Specs() []Spec {
	return []Spec{
		{KeyConcurrency, typedvalue.Count(runKind, "runs of a node role at once"),
			typeconstraints.Set{Min: ptr(1.0)}, typeconstraints.MergeMinimum},
		{KeyQueueDepth, typedvalue.Count(runKind, "runs of a node role allowed to wait"),
			typeconstraints.Set{Min: ptr(0.0)}, typeconstraints.MergeMinimum},
		{KeyRunTimeout, typedvalue.DurationSeconds("longest a single run of a node role may take"),
			typeconstraints.Set{Min: ptr(1.0)}, typeconstraints.MergeMinimum},
		{KeyScheduleInterval, typedvalue.DurationSeconds("time between runs of a scheduled duty"),
			typeconstraints.Set{Min: ptr(1.0)}, typeconstraints.MergeMaximum},
		{KeyScheduleEnabled, typedvalue.Bool("whether a duty runs at all"),
			typeconstraints.Set{}, typeconstraints.MergeBooleanAnd},
	}
}

// EnsureDefinitions registers every definition, idempotently. A definition
// already present with a different shape is an error, never a redefinition.
func EnsureDefinitions(ctx context.Context, r facade.Reader, w facade.Writer, actor string) error {
	for _, s := range Specs() {
		if _, err := facade.EnsurePolicyDefinition(ctx, r, w, s.Key, s.ValueType, s.Constraints, s.Merge,
			structure.ActivationImmediate, structure.BindingInheritedLive, actor); err != nil {
			return fmt.Errorf("noderolespolicy: ensure %s: %w", s.Key, err)
		}
	}
	return nil
}
