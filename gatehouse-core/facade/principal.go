package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// EnsurePrincipal is idempotent by key: a second call with the same
// key returns the existing principal rather than erroring or creating
// a duplicate. This is still the explicit, separately-authorized
// operation the model doc's "no implicit principal creation" invariant
// calls for — "ensure" means safe for an app to call every time it
// starts up, not "create without being asked."
//
// Known limitation, named rather than hidden: this does a read then a
// write, not a single atomic upsert, so two concurrent EnsurePrincipal
// calls for a brand-new key can race (both see "not found," one's
// INSERT then fails on the key's UNIQUE constraint). Fine for the
// common case of one app provisioning its own principals at startup;
// worth revisiting with a real upsert if concurrent first-use ever
// becomes a real pattern.
func EnsurePrincipal(ctx context.Context, reader Reader, writer Writer, key string, principalType structure.PrincipalType) (structure.Principal, error) {
	existing, found, err := reader.GetPrincipalByKey(ctx, key)
	if err != nil {
		return structure.Principal{}, fmt.Errorf("facade: ensure principal: %w", err)
	}
	if found {
		return existing, nil
	}

	// Truncated to microsecond precision to match what Postgres's
	// timestamptz actually stores — otherwise a value returned fresh
	// on create (full nanosecond + monotonic precision) stops
	// comparing equal to the same instant read back later, which
	// strips both.
	now := time.Now().Truncate(time.Microsecond)
	p := structure.Principal{
		PrincipalID: uuid.New(),
		Key:         key,
		Type:        principalType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := p.Validate(); err != nil {
		return structure.Principal{}, fmt.Errorf("facade: ensure principal: %w", err)
	}
	if err := writer.CreatePrincipal(ctx, p); err != nil {
		return structure.Principal{}, fmt.Errorf("facade: ensure principal: %w", err)
	}
	return p, nil
}
