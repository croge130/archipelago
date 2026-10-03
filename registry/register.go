package registry

import (
	"context"
	"encoding/json"

	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/transit"
)

// RegisterFromSession resolves session's verified peer identity to a
// principal via peerauth.ResolvePrincipal — the same chokepoint
// peerauth.Require itself uses — then registers a fresh Instance for
// that principal in group. The instance's PrincipalID is never
// caller-asserted; it's only ever what the connection's own mTLS
// identity resolves to.
func RegisterFromSession(ctx context.Context, reader gatehouseFacade.Reader, writer gatehouseFacade.Writer, session transit.Session, group string, metadata json.RawMessage) (structure.Instance, error) {
	principalID, err := peerauth.ResolvePrincipal(ctx, reader, session)
	if err != nil {
		return structure.Instance{}, err
	}
	return gatehouseFacade.RegisterInstance(ctx, writer, principalID, group, metadata)
}
