package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validTicket() Ticket {
	now := time.Now()
	return Ticket{
		TicketID:           uuid.New(),
		SubjectPrincipalID: uuid.New(),
		Audience:           "gamebridge",
		IssuedAt:           now,
		ExpiresAt:          now.Add(time.Minute),
		Signature:          []byte("sig"),
	}
}

func TestTicketValidateOK(t *testing.T) {
	if err := validTicket().Validate(); err != nil {
		t.Fatalf("expected a valid ticket to validate, got: %v", err)
	}
}

func TestTicketValidateRejectsMissingTicketID(t *testing.T) {
	tk := validTicket()
	tk.TicketID = uuid.Nil
	if err := tk.Validate(); err == nil {
		t.Fatal("expected an error for a missing TicketID")
	}
}

func TestTicketValidateRejectsMissingSubjectPrincipalID(t *testing.T) {
	tk := validTicket()
	tk.SubjectPrincipalID = uuid.Nil
	if err := tk.Validate(); err == nil {
		t.Fatal("expected an error for a missing SubjectPrincipalID")
	}
}

func TestTicketValidateRejectsMissingAudience(t *testing.T) {
	tk := validTicket()
	tk.Audience = ""
	if err := tk.Validate(); err == nil {
		t.Fatal("expected an error for a missing Audience")
	}
}

func TestTicketValidateRejectsExpiresAtNotAfterIssuedAt(t *testing.T) {
	tk := validTicket()
	tk.ExpiresAt = tk.IssuedAt
	if err := tk.Validate(); err == nil {
		t.Fatal("expected an error when ExpiresAt does not come after IssuedAt")
	}
}

func TestTicketValidateRejectsMissingSignature(t *testing.T) {
	tk := validTicket()
	tk.Signature = nil
	if err := tk.Validate(); err == nil {
		t.Fatal("expected an error for a missing Signature")
	}
}

func TestTicketSigningBytesDeterministic(t *testing.T) {
	tk := validTicket()
	if string(tk.SigningBytes()) != string(tk.SigningBytes()) {
		t.Fatal("expected SigningBytes to be deterministic for the same ticket")
	}
}

func TestTicketSigningBytesExcludesSignature(t *testing.T) {
	tk := validTicket()
	a := tk.SigningBytes()
	tk.Signature = []byte("a completely different signature")
	b := tk.SigningBytes()
	if string(a) != string(b) {
		t.Fatal("expected SigningBytes to be unaffected by Signature, since Signature covers every other field")
	}
}

func TestTicketSigningBytesChangesWithAudience(t *testing.T) {
	tk := validTicket()
	a := tk.SigningBytes()
	tk.Audience = "a-different-audience"
	b := tk.SigningBytes()
	if string(a) == string(b) {
		t.Fatal("expected SigningBytes to change when Audience changes")
	}
}
