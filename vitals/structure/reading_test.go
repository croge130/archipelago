package structure

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func validReading() Reading {
	return Reading{
		InstanceID: uuid.New(),
		State:      StateOK,
		UpdatedAt:  time.Now(),
	}
}

func TestReadingValidateOK(t *testing.T) {
	if err := validReading().Validate(); err != nil {
		t.Fatalf("expected a valid reading to validate, got: %v", err)
	}
}

func TestReadingValidateRejectsMissingInstanceID(t *testing.T) {
	r := validReading()
	r.InstanceID = uuid.Nil
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for a missing InstanceID")
	}
}

func TestReadingValidateRejectsUnknownState(t *testing.T) {
	r := validReading()
	r.State = State("made_up")
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for an unknown State")
	}
}

func TestReadingValidateRejectsImpactScoreOutOfRange(t *testing.T) {
	r := validReading()
	over := 101
	r.ImpactScore = &over
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for an ImpactScore above 100")
	}
	under := -1
	r.ImpactScore = &under
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for a negative ImpactScore")
	}
}

func TestReadingValidateRejectsOverlongSummary(t *testing.T) {
	r := validReading()
	long := make([]byte, MaxSummaryRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	r.Summary = string(long)
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for a summary over the max length")
	}
}

func TestReadingValidateRejectsControlCharactersInSummary(t *testing.T) {
	r := validReading()
	r.Summary = "hello\x00world"
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for a summary containing control characters")
	}
}

func TestReadingValidateRejectsBadReasonCode(t *testing.T) {
	r := validReading()
	r.ReasonCode = "Not A Reason Code!"
	if err := r.Validate(); err == nil {
		t.Fatal("expected an error for an invalid reason_code")
	}
}
