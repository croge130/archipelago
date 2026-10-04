package facade

import (
	"context"
	"fmt"
	"strings"

	"github.com/croge130/archipelago/gatehouse-core/structure"
)

// defaultReservedNamespaces are the top-level permission-key segments
// blocked by default — Archipelago's own built-in bases, not an
// attempt to wall off a malicious app. An app registering its own
// permission keys already has full access to its own database;
// there's no real adversary to defend against here. This exists to
// catch an app unknowingly shadowing a built-in permission, a hygiene
// guardrail, not a security boundary — see
// docs/architecture/09-gatehouse-core-model.md's Permission namespace
// reservation section.
var defaultReservedNamespaces = map[string]bool{
	"gatehouse":   true,
	"archipelago": true,
	"policy":      true,
	"certstore":   true,
	"transit":     true,
	"alias":       true,
	"vitals":      true,
}

// RegisterPermissionOptions controls RegisterPermission's defaults.
type RegisterPermissionOptions struct {
	// AllowReservedNamespace opts out of the default-blocked check —
	// the explicit override the model doc calls for, via policy or SDK
	// setup config. There's no Policy base wired up yet to source this
	// from automatically, so for now it's a parameter the caller sets
	// deliberately.
	AllowReservedNamespace bool
}

// RegisterPermission registers a PermissionDefinition, enforcing the
// reserved-namespace default and structure's own shape validation
// (which already rejects, among other things, a wildcard-includable
// recovery_access definition). Idempotent by PermissionKey, the same
// "ensure" rule EnsureMTLSCredential's own doc comment names: a second
// call registering the exact same definition is a no-op, never a raw
// insert that errors on the key's own uniqueness constraint — the
// shape every caller actually wants for something meant to run at
// every app startup. A second call with a different definition under
// the same key is ErrConflict, not a silent redefinition.
//
// Known limitation, same as EnsurePrincipal's own: this is a read
// then a write, not an atomic upsert, so two concurrent first-time
// registrations of the same brand-new key can race (both see "not
// found," one's INSERT then fails on the key's own uniqueness
// constraint instead of returning the idempotent success a caller
// expects).
func RegisterPermission(ctx context.Context, reader Reader, writer Writer, def structure.PermissionDefinition, opts RegisterPermissionOptions) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if !opts.AllowReservedNamespace && isReservedNamespace(def.PermissionKey) {
		return fmt.Errorf("facade: register permission: %q is under a reserved namespace; set AllowReservedNamespace to override", def.PermissionKey)
	}

	existing, found, err := reader.GetPermissionDefinition(ctx, def.PermissionKey)
	if err != nil {
		return fmt.Errorf("facade: register permission: %w", err)
	}
	if found {
		if existing == def {
			return nil
		}
		return ErrConflict
	}

	if err := writer.RegisterPermissionDefinition(ctx, def); err != nil {
		return fmt.Errorf("facade: register permission: %w", err)
	}
	return nil
}

func isReservedNamespace(permissionKey string) bool {
	prefix, _, found := strings.Cut(permissionKey, ".")
	if !found {
		return false
	}
	return defaultReservedNamespaces[prefix]
}
