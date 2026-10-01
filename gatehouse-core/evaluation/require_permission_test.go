package evaluation

import (
	"context"
	"testing"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

func TestRequirePermissionAllowed(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(globalPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", structure.GrantEffectAllow))

	if err := RequirePermission(context.Background(), store, principal, "myapp.readinglist.read"); err != nil {
		t.Fatalf("expected RequirePermission to allow, got: %v", err)
	}
}

func TestRequirePermissionDenied(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))

	if err := RequirePermission(context.Background(), store, principal, "myapp.readinglist.read"); err == nil {
		t.Fatal("expected RequirePermission to deny with no grant at all")
	}
}

func TestRequireContextPermissionRejectsEmptyContext(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	if err := RequireContextPermission(context.Background(), store, principal, "myapp.readinglist.read", "", "42"); err == nil {
		t.Fatal("expected an error for an empty contextType")
	}
}

func TestRequireContextPermissionAllowed(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))
	store.addGrant(contextPermissionGrant(structure.GrantSubjectTypePrincipal, principal, "myapp.readinglist.read", "myapp.readinglist", "42", structure.GrantEffectAllow))

	if err := RequireContextPermission(context.Background(), store, principal, "myapp.readinglist.read", "myapp.readinglist", "42"); err != nil {
		t.Fatalf("expected RequireContextPermission to allow, got: %v", err)
	}
}
