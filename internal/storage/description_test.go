package storage

import (
	"strings"
	"testing"
)

func TestValidateDescription(t *testing.T) {
	got, err := ValidateDescription("  hello  ", true)
	if err != nil || got != "hello" {
		t.Fatalf("trim = %q, %v", got, err)
	}
	if _, err := ValidateDescription("   ", false); err == nil {
		t.Fatal("empty required description should fail")
	}
	if _, err := ValidateDescription("", true); err != nil {
		t.Fatalf("empty optional description: %v", err)
	}
	tooLong := strings.Repeat("a", MaxDescriptionBytes+1)
	if _, err := ValidateDescription(tooLong, true); err == nil {
		t.Fatal("oversize description should fail")
	}
}
