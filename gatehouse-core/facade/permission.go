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
// recovery_access definition).
func RegisterPermission(ctx context.Context, writer Writer, def structure.PermissionDefinition, opts RegisterPermissionOptions) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if !opts.AllowReservedNamespace && isReservedNamespace(def.PermissionKey) {
		return fmt.Errorf("facade: register permission: %q is under a reserved namespace; set AllowReservedNamespace to override", def.PermissionKey)
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
