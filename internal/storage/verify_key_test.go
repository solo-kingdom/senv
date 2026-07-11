package storage

import (
	"testing"
)

func TestVerifyKey(t *testing.T) {
	mgr, _ := setupTestManager(t)
	const password = "test-password"

	valid, err := mgr.VerifyKey(make([]byte, 32))
	if err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
	if valid {
		t.Fatal("random key should not verify")
	}

	key := derivedKey(t, mgr, password)
	valid, err = mgr.VerifyKey(key)
	if err != nil {
		t.Fatalf("VerifyKey with correct key: %v", err)
	}
	if !valid {
		t.Fatal("derived key should verify")
	}
}
