package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestInvalidKDFRejectedBeforeDeriveSession(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	manager := storage.NewManager(configPath, dataPath)
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	metadata.KDFIterations = 1_000_001
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configPath, storage.MetadataFile), data, 0o600); err != nil {
		t.Fatalf("write invalid metadata: %v", err)
	}

	originalDerive := deriveKeyWithIterations
	deriveCalls := 0
	deriveKeyWithIterations = func(password string, salt []byte, iterations int) []byte {
		deriveCalls++
		return originalDerive(password, salt, iterations)
	}
	t.Cleanup(func() { deriveKeyWithIterations = originalDerive })

	timeout, err := ParseTimeout("never")
	if err != nil {
		t.Fatalf("ParseTimeout: %v", err)
	}
	sessionManager := NewManager(configPath, dataPath)
	err = sessionManager.StartSession("correct-secret", timeout)
	if !errors.Is(err, storage.ErrInvalidKDFParameters) {
		t.Fatalf("StartSession error = %v, want ErrInvalidKDFParameters", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid password") {
		t.Fatalf("invalid KDF metadata was reported as invalid password: %v", err)
	}
	if deriveCalls != 0 {
		t.Fatalf("PBKDF2 invoked %d times at session entry", deriveCalls)
	}
	cache, loadErr := sessionManager.LoadCache()
	if loadErr != nil {
		t.Fatalf("LoadCache: %v", loadErr)
	}
	if cache != nil {
		t.Fatal("session cache created for invalid KDF metadata")
	}
}
