package structure

import "testing"

func TestLifecycleValid(t *testing.T) {
	if !LifecycleActive.Valid() || !LifecycleArchived.Valid() {
		t.Fatal("expected both Lifecycle values to be valid")
	}
	if Lifecycle("deleted").Valid() {
		t.Fatal("expected an unknown Lifecycle value to be invalid")
	}
}

func TestActivationModeValid(t *testing.T) {
	if !ActivationImmediate.Valid() || !ActivationDeferred.Valid() {
		t.Fatal("expected both ActivationMode values to be valid")
	}
	if ActivationMode("sometimes").Valid() {
		t.Fatal("expected an unknown ActivationMode to be invalid")
	}
}

func TestBindingModeValid(t *testing.T) {
	for _, m := range []BindingMode{BindingInheritedLive, BindingCopiedAtCreation, BindingExplicitOverride} {
		if !m.Valid() {
			t.Fatalf("expected %q to be valid", m)
		}
	}
	if BindingMode("whenever").Valid() {
		t.Fatal("expected an unknown BindingMode to be invalid")
	}
}

func TestTargetKindValid(t *testing.T) {
	for _, k := range []TargetKind{TargetKindGlobal, TargetKindRef, TargetKindPolicyContext} {
		if !k.Valid() {
			t.Fatalf("expected %q to be valid", k)
		}
	}
	if TargetKind("nowhere").Valid() {
		t.Fatal("expected an unknown TargetKind to be invalid")
	}
}
