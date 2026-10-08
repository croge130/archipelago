package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/typedvalue"
)

// NormalizeParams validates raw against def's ParamSpecs and returns
// the canonical JSON plus its SHA-256 hash. It is the single place a
// job's parameters are checked, used by the submitter against the
// TaskDefinition and by an executor against its own handler's
// expectation — neither trusts the other.
//
// Parameters must be a JSON object (empty or absent means {}). Unknown
// names are rejected, required ones must be present, and each value is
// normalized with typedvalue.NormalizeValue, so the canonical form —
// and therefore the hash — is stable however the caller spelled a value
// (a count sent as 7 or 7.0, or a string with stray spaces, hashes the same). The hash is what
// binds an authority grant to exactly these parameters, so that the
// parameters cannot be swapped under an authorization that was given
// for different ones.
func NormalizeParams(def structure.TaskDefinition, raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) > structure.MaxParamsBytes {
		return nil, "", fmt.Errorf("evaluation: params: %d bytes is over the %d limit", len(raw), structure.MaxParamsBytes)
	}
	given := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &given); err != nil {
			return nil, "", fmt.Errorf("evaluation: params: must be a JSON object: %w", err)
		}
		if given == nil { // the literal JSON null
			given = map[string]any{}
		}
	}

	specs := make(map[string]structure.ParamSpec, len(def.Params))
	for _, p := range def.Params {
		specs[p.Name] = p
	}
	for name := range given {
		if _, ok := specs[name]; !ok {
			return nil, "", fmt.Errorf("evaluation: params: unknown parameter %q", name)
		}
	}

	normalized := make(map[string]any, len(given))
	for _, spec := range def.Params {
		value, present := given[spec.Name]
		if !present {
			if spec.Required {
				return nil, "", fmt.Errorf("evaluation: params: missing required parameter %q", spec.Name)
			}
			continue
		}
		n, err := typedvalue.NormalizeValue(spec.Definition, value)
		if err != nil {
			return nil, "", fmt.Errorf("evaluation: params: %q: %w", spec.Name, err)
		}
		if spec.Definition.Storage == typedvalue.StorageEnum {
			if err := typedvalue.ValidateValue(spec.Definition, n); err != nil {
				return nil, "", fmt.Errorf("evaluation: params: %q: %w", spec.Name, err)
			}
		}
		normalized[spec.Name] = n
	}

	// encoding/json writes map keys in sorted order, so this is canonical.
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("evaluation: params: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

// ResolvedScope is one ScopeEntry with its context ID filled in from
// the job's parameters.
type ResolvedScope struct {
	PermissionKey string
	ContextType   string // "" means the permission is not context-scoped
	ContextID     string
}

// ResolveScope turns def's declared scope into concrete entries for one
// job by reading each ContextIDParam from params (already normalized).
// A scope entry whose parameter is absent or empty is an error, not a
// silent widening to a global permission: a handler must never end up
// with more reach than its declaration promised.
func ResolveScope(def structure.TaskDefinition, params json.RawMessage) ([]ResolvedScope, error) {
	var values map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &values); err != nil {
			return nil, fmt.Errorf("evaluation: scope: %w", err)
		}
	}
	out := make([]ResolvedScope, 0, len(def.Scope))
	for _, s := range def.Scope {
		r := ResolvedScope{PermissionKey: s.PermissionKey, ContextType: s.ContextType}
		if s.ContextIDParam != "" {
			v, ok := values[s.ContextIDParam]
			if !ok {
				return nil, fmt.Errorf("evaluation: scope: %q needs parameter %q, which is absent", s.PermissionKey, s.ContextIDParam)
			}
			id, ok := v.(string)
			if !ok || id == "" {
				return nil, fmt.Errorf("evaluation: scope: %q needs parameter %q to be a non-empty string", s.PermissionKey, s.ContextIDParam)
			}
			r.ContextID = id
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PermissionKey < out[j].PermissionKey })
	return out, nil
}

// ScopePermits reports whether a handler operation — permissionKey,
// optionally within (contextType, contextID) — falls inside the
// resolved scope. A context-scoped entry permits only that exact
// context; an unscoped entry permits only the unscoped check, never a
// context-scoped one, so declaring a global permission does not quietly
// grant it everywhere.
func ScopePermits(scope []ResolvedScope, permissionKey, contextType, contextID string) bool {
	for _, s := range scope {
		if s.PermissionKey == permissionKey && s.ContextType == contextType && s.ContextID == contextID {
			return true
		}
	}
	return false
}

// SameTaskDefinition reports whether a and b are the same definition,
// for the idempotent-registration rule (an identical re-registration is
// a no-op; a different one under the same key is a conflict).
//
// It compares canonical JSON with nil slices and nil metadata normalized
// to empty, because a definition read back from storage has its empty
// lists as [] and its empty metadata as {} where a freshly built one may
// have nil — the same nil-versus-"{}" trap that bit EndpointDefinition's
// idempotency check, which compared field by field for that reason.
func SameTaskDefinition(a, b structure.TaskDefinition) bool {
	return bytes.Equal(canonicalTaskDefinition(a), canonicalTaskDefinition(b))
}

func canonicalTaskDefinition(d structure.TaskDefinition) []byte {
	if d.Params == nil {
		d.Params = []structure.ParamSpec{}
	}
	if d.Scope == nil {
		d.Scope = []structure.ScopeEntry{}
	}
	if len(d.Metadata) == 0 {
		d.Metadata = json.RawMessage("{}")
	}
	d.DefaultBackoff = d.EffectiveBackoff() // an unstated backoff and the default are the same thing
	out, err := json.Marshal(d)
	if err != nil {
		return nil // cannot happen for these types; nil never equals a real encoding
	}
	return out
}
