package typedvalue

import (
	"fmt"
	"strings"
)

// reservedNamespacePrefixes blocks accidental shadowing of the
// platform's own count kinds — a hygiene guardrail, not a security
// boundary, the same reasoning gatehouse-core's permission-namespace
// reservation already documents: an app registering its own
// count:<what> name already has full access to its own process, so
// there's no adversary to wall off, only a confusing-bug-by-collision
// to prevent.
var reservedNamespacePrefixes = []string{"typedvalue", "archipelago"}

func requireUnreserved(name string) error {
	for _, prefix := range reservedNamespacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("%q uses the reserved %q namespace (pass allowReserved to override)", name, prefix)
		}
	}
	return nil
}
