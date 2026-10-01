package structure

import "testing"

func TestAuthorityGenerationCompareToStale(t *testing.T) {
	cached := AuthorityGeneration{PermissionSchemaGeneration: 1, PrincipalGrantGeneration: 5}
	current := AuthorityGeneration{PermissionSchemaGeneration: 1, PrincipalGrantGeneration: 7}

	f := cached.CompareTo(current)
	if f.SchemaStale {
		t.Error("schema generation didn't change, should not be stale")
	}
	if !f.GrantStale {
		t.Error("grant generation advanced, should be stale")
	}
	if f.GrantGap != 2 {
		t.Errorf("GrantGap = %d, want 2", f.GrantGap)
	}
	if !f.Stale() {
		t.Error("Stale() should be true when any counter is stale")
	}
}

func TestAuthorityGenerationCompareToFresh(t *testing.T) {
	gen := AuthorityGeneration{PermissionSchemaGeneration: 3, PrincipalGrantGeneration: 3}
	f := gen.CompareTo(gen)
	if f.Stale() {
		t.Error("comparing a generation to itself should never be stale")
	}
}
