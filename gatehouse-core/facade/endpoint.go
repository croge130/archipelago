package facade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// ErrPermissionNotRegistered is returned by RegisterEndpoint when
// def.RequiredPermissionKey is non-empty but names no registered
// PermissionDefinition — the drift this feature exists to prevent,
// per docs/architecture/15-endpoint-advertisement-model.md: an
// endpoint can't silently claim a permission that was never
// registered, or that was registered under a different key than the
// caller expects.
var ErrPermissionNotRegistered = errors.New("facade: required permission key is not registered")

// RegisterEndpointOptions controls RegisterEndpoint's defaults —
// mirrors RegisterPermissionOptions exactly.
type RegisterEndpointOptions struct {
	// AllowReservedNamespace opts out of the default-blocked reserved-
	// namespace check for EndpointKey. Reuses the same
	// defaultReservedNamespaces map RegisterPermission already checks
	// EndpointKey against — one namespace-hygiene mechanism, not a
	// second one for a second kind of key.
	AllowReservedNamespace bool
}

// RegisterEndpoint registers an EndpointDefinition, enforcing the
// reserved-namespace default (same map, same override, as
// RegisterPermission) and structure's own shape validation. Idempotent
// by EndpointKey: a second call registering the exact same definition
// is a no-op; a second call with a different definition under the
// same key is ErrConflict — the same "ensure" rule RegisterPermission
// itself follows.
//
// The one check with no RegisterPermission analog: if
// def.RequiredPermissionKey is non-empty, it must already be a
// registered PermissionDefinition, or this returns
// ErrPermissionNotRegistered. An app registers the permissions an
// endpoint requires before registering that endpoint — a reversed
// order fails loudly here instead of producing an endpoint that
// silently requires nothing.
//
// Known limitation, same as RegisterPermission's own: this is a read
// then a write, not an atomic upsert, so two concurrent first-time
// registrations of the same brand-new key can race.
func RegisterEndpoint(ctx context.Context, reader Reader, writer Writer, def structure.EndpointDefinition, opts RegisterEndpointOptions) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if !opts.AllowReservedNamespace && isReservedNamespace(def.EndpointKey) {
		return fmt.Errorf("facade: register endpoint: %q is under a reserved namespace; set AllowReservedNamespace to override", def.EndpointKey)
	}
	if def.RequiredPermissionKey != "" {
		if _, found, err := reader.GetPermissionDefinition(ctx, def.RequiredPermissionKey); err != nil {
			return fmt.Errorf("facade: register endpoint: %w", err)
		} else if !found {
			return ErrPermissionNotRegistered
		}
	}

	existing, found, err := reader.GetEndpointDefinition(ctx, def.EndpointKey)
	if err != nil {
		return fmt.Errorf("facade: register endpoint: %w", err)
	}
	if found {
		if sameEndpointDefinition(existing, def) {
			return nil
		}
		return ErrConflict
	}

	if err := writer.RegisterEndpointDefinition(ctx, def); err != nil {
		return fmt.Errorf("facade: register endpoint: %w", err)
	}
	return nil
}

// sameEndpointDefinition compares two definitions for RegisterEndpoint's
// idempotency check. Metadata can't be compared with == (it's a slice)
// or safely with reflect.DeepEqual either: dbstore's own nullableJSON
// defaults a nil/empty Metadata to the literal bytes "{}" before
// storing, so existing (always read back through that round trip)
// would otherwise compare unequal to a fresh def whose caller simply
// omitted Metadata — a false ErrConflict on the exact "register the
// same thing twice" case this function exists to keep idempotent.
// canonicalMetadata applies that same default on both sides first.
func sameEndpointDefinition(existing, def structure.EndpointDefinition) bool {
	return existing.EndpointKey == def.EndpointKey &&
		existing.Description == def.Description &&
		existing.RequiredPermissionKey == def.RequiredPermissionKey &&
		canonicalMetadata(existing.Metadata) == canonicalMetadata(def.Metadata)
}

func canonicalMetadata(m json.RawMessage) string {
	if len(m) == 0 {
		return "{}"
	}
	return string(m)
}

// ListEndpoints returns every registered endpoint, unfiltered — the
// same registration data RegisterEndpoint writes, never a second,
// separately-maintained list. Use AdvertiseEndpoints instead when a
// caller-specific visibility policy (rather than "show everything") is
// wanted.
func ListEndpoints(ctx context.Context, reader Reader) ([]structure.EndpointDefinition, error) {
	defs, err := reader.ListEndpointDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("facade: list endpoints: %w", err)
	}
	return defs, nil
}

// AdvertiseEndpoints is where 04-facades-and-ergonomics.md's
// "visibility is a policy choice, not a fixed philosophy" becomes
// code. filterByGrant=false returns every registered endpoint — an
// app that wants to show all and deny at call time.
// filterByGrant=true keeps only the endpoints principalID is actually
// allowed to call: an empty RequiredPermissionKey always passes
// (nothing to check), otherwise this calls
// evaluation.RequirePermission against the exact same evaluator every
// other permission check in this codebase already goes through —
// never a second, advertisement-specific authorization path.
func AdvertiseEndpoints(ctx context.Context, reader Reader, principalID uuid.UUID, filterByGrant bool) ([]structure.EndpointDefinition, error) {
	defs, err := ListEndpoints(ctx, reader)
	if err != nil {
		return nil, err
	}
	if !filterByGrant {
		return defs, nil
	}

	visible := make([]structure.EndpointDefinition, 0, len(defs))
	for _, def := range defs {
		if def.RequiredPermissionKey == "" {
			visible = append(visible, def)
			continue
		}
		if err := evaluation.RequirePermission(ctx, reader, principalID, def.RequiredPermissionKey); err == nil {
			visible = append(visible, def)
		}
	}
	return visible, nil
}
