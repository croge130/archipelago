package evaluation

import (
	"context"
	"errors"
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

func TestRequireDenialWrapsErrDeniedAndStoreFailureDoesNot(t *testing.T) {
	store := newMemStore()
	principal := uuid.New()
	store.addPermission(standardPermission("myapp.readinglist.read"))

	denied := RequirePermission(context.Background(), store, principal, "myapp.readinglist.read")
	if !errors.Is(denied, ErrDenied) {
		t.Fatalf("a denial should wrap ErrDenied, got: %v", denied)
	}

	sentinel := errors.New("backend unreachable")
	failed := RequirePermission(context.Background(), failingStore{Store: store, err: sentinel}, principal, "myapp.readinglist.read")
	if errors.Is(failed, ErrDenied) {
		t.Fatal("a store failure was reported as a denial")
	}
	if !errors.Is(failed, sentinel) {
		t.Fatalf("a store failure should reach the caller, got: %v", failed)
	}
}

// failingStore fails the first read Evaluate makes.
type failingStore struct {
	Store
	err error
}

func (s failingStore) GetPermissionDefinition(context.Context, string) (structure.PermissionDefinition, bool, error) {
	return structure.PermissionDefinition{}, false, s.err
}
