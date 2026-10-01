package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validSession() Session {
	return Session{
		SessionID:      uuid.New(),
		PrincipalID:    uuid.New(),
		Kind:           SessionKindUI,
		AuthorityLevel: AuthorityLevelStandard,
		CreatedAt:      time.Now(),
		LastSeen:       time.Now(),
	}
}

func TestSessionValidateOK(t *testing.T) {
	if err := validSession().Validate(); err != nil {
		t.Fatalf("expected a valid session to validate, got: %v", err)
	}
}

func TestSessionValidateRejectsAssertedByWithoutAssertedKind(t *testing.T) {
	s := validSession()
	id := uuid.New()
	s.AssertedByPrincipalID = &id
	if err := s.Validate(); err == nil {
		t.Fatal("expected AssertedByPrincipalID on a non-asserted session to be rejected")
	}
	s.Kind = SessionKindAsserted
	if err := s.Validate(); err != nil {
		t.Fatalf("expected AssertedByPrincipalID on an asserted session to validate, got: %v", err)
	}
}

func TestSessionIsRevoked(t *testing.T) {
	s := validSession()
	if s.IsRevoked() {
		t.Error("fresh session should not be revoked")
	}
	now := time.Now()
	s.RevokedAt = &now
	if !s.IsRevoked() {
		t.Error("session with RevokedAt set should be revoked")
	}
}

func TestSessionIsExpired(t *testing.T) {
	s := validSession()
	now := time.Now()
	if s.IsExpired(now) {
		t.Error("session with no ExpiresAt should never be expired")
	}
	past := now.Add(-time.Hour)
	s.ExpiresAt = &past
	if !s.IsExpired(now) {
		t.Error("session with a past ExpiresAt should be expired")
	}
}

func TestAuthorityLevelMeets(t *testing.T) {
	cases := []struct {
		have, require AuthorityLevel
		want          bool
	}{
		{AuthorityLevelStandard, AuthorityLevelStandard, true},
		{AuthorityLevelElevated, AuthorityLevelStandard, true},
		{AuthorityLevelStandard, AuthorityLevelElevated, false},
		{AuthorityLevelElevated, AuthorityLevelRecoveryAccess, false},
		{AuthorityLevelRecoveryAccess, AuthorityLevelElevated, true},
	}
	for _, c := range cases {
		if got := c.have.Meets(c.require); got != c.want {
			t.Errorf("%s.Meets(%s) = %v, want %v", c.have, c.require, got, c.want)
		}
	}
}
