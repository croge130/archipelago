package structure

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PrincipalType is the set of things that can act, per
// 09-gatehouse-core-model.md's Principal section. app_client is
// deliberately absent — it existed in Lighthouse for a cross-app
// relationship this model doesn't have.
type PrincipalType string

const (
	PrincipalTypeUser           PrincipalType = "user"
	PrincipalTypeAgent          PrincipalType = "agent"
	PrincipalTypeServiceAccount PrincipalType = "service_account"
	PrincipalTypeSystem         PrincipalType = "system"
	PrincipalTypeExternal       PrincipalType = "external"
	PrincipalTypeAnonymous      PrincipalType = "anonymous"
)

func (t PrincipalType) Valid() bool {
	switch t {
	case PrincipalTypeUser, PrincipalTypeAgent, PrincipalTypeServiceAccount,
		PrincipalTypeSystem, PrincipalTypeExternal, PrincipalTypeAnonymous:
		return true
	default:
		return false
	}
}

// Principal is anything that can act. Devices and hosts are
// deliberately not a PrincipalType — per the model doc, they're
// correlation context referenced from a Credential or Session, never a
// principal in their own right.
type Principal struct {
	PrincipalID      uuid.UUID
	Key              string // stable, human-oriented
	DisplayName      string
	Type             PrincipalType
	OwnerPrincipalID *uuid.UUID      // attestation-rights input; see model doc
	Metadata         json.RawMessage // opaque, never authorized on
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate checks the shape Gatehouse-core's evaluator can rely on
// without re-deriving it at every call site. It does not check
// uniqueness, existence of OwnerPrincipalID, or anything requiring a
// DB — that's Evaluation and Storage's job, not Structure's.
func (p Principal) Validate() error {
	if p.PrincipalID == uuid.Nil {
		return fmt.Errorf("structure: principal: PrincipalID is required")
	}
	if p.Key == "" {
		return fmt.Errorf("structure: principal: Key is required")
	}
	if !p.Type.Valid() {
		return fmt.Errorf("structure: principal: invalid Type %q", p.Type)
	}
	if p.OwnerPrincipalID != nil && *p.OwnerPrincipalID == p.PrincipalID {
		// A principal owning itself is exactly the self-owned-root
		// shape this model deliberately doesn't use (see the model
		// doc's resolution of the multi-root question) — ownership
		// here is about attestation rights, not bypass, so self-
		// ownership has no meaning to allow.
		return fmt.Errorf("structure: principal: OwnerPrincipalID cannot reference itself")
	}
	return nil
}
