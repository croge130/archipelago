package facade

import (
	"context"

	"github.com/croge130/archipelago/alias/evaluation"
	"github.com/croge130/archipelago/alias/structure"
)

// Reader is everything the facade needs to read — just
// evaluation.Store today, named here in case Ensure/Release ever need
// a read evaluation.Store itself doesn't.
type Reader interface {
	evaluation.Store
}

// Writer is the one mutation Alias's data needs: a raw set, with no
// idempotency or conflict logic of its own — that logic lives in
// EnsureAlias/ReleaseAlias below, which decide the desired row state
// before calling this.
type Writer interface {
	UpsertAlias(ctx context.Context, a structure.Alias) error
}
