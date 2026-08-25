package turbopg

import (
	"strings"
	"testing"
)

func TestValidateDocumentID(t *testing.T) {
	if err := ValidateDocumentID(""); err != ErrEmptyDocumentID {
		t.Fatalf("empty: %v", err)
	}
	tooLong := DocumentID(strings.Repeat("a", MaxDocumentIDBytes+1))
	if err := ValidateDocumentID(tooLong); !IsInvalidInput(err) {
		t.Fatalf("long: %v", err)
	}
	if err := ValidateDocumentID("ok"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAttributeName(t *testing.T) {
	if err := ValidateAttributeName("$dist"); !IsInvalidInput(err) {
		t.Fatalf("$: %v", err)
	}
	if err := ValidateAttributeName("name"); err != nil {
		t.Fatal(err)
	}
}
