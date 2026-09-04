package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveKeyWithIterations(t *testing.T) {
	password := "test-password-123"
	salt := GenerateSaltFixed()

	key := DeriveKeyWithIterations(password, salt, DefaultIterations)
	if len(key) != KeySize {
		t.Errorf("Expected key length %d, got %d", KeySize, len(key))
	}

	// Same parameters must produce the same key
	key2 := DeriveKeyWithIterations(password, salt, DefaultIterations)
	if !bytes.Equal(key, key2) {
		t.Error("Same password, salt and iterations should produce same key")
	}

	// Different iteration counts must produce different keys
	keyLegacy := DeriveKeyWithIterations(password, salt, LegacyIterations)
	if bytes.Equal(key, keyLegacy) {
		t.Error("Different iteration counts should produce different keys")
	}
}

func TestDeriveKeyUsesLegacyIterations(t *testing.T) {
	salt := GenerateSaltFixed()
	key := DeriveKey("pw", salt)
	expected := DeriveKeyWithIterations("pw", salt, LegacyIterations)
	if !bytes.Equal(key, expected) {
		t.Error("DeriveKey must delegate to LegacyIterations")
	}
}

func TestIterationConstants(t *testing.T) {
	if DefaultIterations != 600000 {
		t.Errorf("DefaultIterations = %d, want 600000 (OWASP 2026)", DefaultIterations)
	}
	if LegacyIterations != 100000 {
		t.Errorf("LegacyIterations = %d, want 100000", LegacyIterations)
	}
}

func TestKDFConstantsIterations(t *testing.T) {
	if LegacyIterations != 100_000 {
		t.Fatalf("LegacyIterations = %d, want 100000", LegacyIterations)
	}
	if DefaultIterations != 600_000 {
		t.Fatalf("DefaultIterations = %d, want 600000", DefaultIterations)
	}
}
