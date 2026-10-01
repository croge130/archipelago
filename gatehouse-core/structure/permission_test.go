package structure

import "testing"

func TestPermissionDefinitionValidateOK(t *testing.T) {
	p := PermissionDefinition{
		PermissionKey:          "myapp.readinglist.delete",
		RequiredAuthorityLevel: AuthorityLevelElevated,
		WildcardIncludable:     true,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid permission definition to validate, got: %v", err)
	}
}

func TestPermissionDefinitionRejectsWildcardRecoveryAccess(t *testing.T) {
	p := PermissionDefinition{
		PermissionKey:          "myapp.admin.wipe_everything",
		RequiredAuthorityLevel: AuthorityLevelRecoveryAccess,
		WildcardIncludable:     true,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected wildcard-includable + recovery_access to be rejected — wildcards never include recovery-access permissions")
	}
}

func TestPermissionDefinitionAllowsNonWildcardRecoveryAccess(t *testing.T) {
	p := PermissionDefinition{
		PermissionKey:          "myapp.admin.wipe_everything",
		RequiredAuthorityLevel: AuthorityLevelRecoveryAccess,
		WildcardIncludable:     false,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected a non-wildcard recovery_access permission to validate, got: %v", err)
	}
}
