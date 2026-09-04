package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func corruptKDFMetadataForCommandTest(t *testing.T, configPath, dataPath string, iterations int) {
	t.Helper()
	manager := storage.NewManager(configPath, dataPath)
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	metadata.KDFIterations = iterations
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configPath, storage.MetadataFile), data, 0o600); err != nil {
		t.Fatalf("write invalid metadata: %v", err)
	}
}

func countCommandDerivations(t *testing.T) *int {
	t.Helper()
	original := deriveKeyWithIterations
	calls := 0
	deriveKeyWithIterations = func(password string, salt []byte, iterations int) []byte {
		calls++
		return original(password, salt, iterations)
	}
	t.Cleanup(func() { deriveKeyWithIterations = original })
	return &calls
}

func TestInvalidKDFRejectedBeforeDeriveCLI(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	corruptKDFMetadataForCommandTest(t, cfg, data, 1_000_001)
	calls := countCommandDerivations(t)

	prompts := 0
	_, err := resolveAuth(cfg, data, countingPrompter("correct-secret", &prompts))
	if !errors.Is(err, storage.ErrInvalidKDFParameters) {
		t.Fatalf("resolveAuth error = %v, want ErrInvalidKDFParameters", err)
	}
	if errors.Is(err, errInvalidPassword) {
		t.Fatalf("invalid KDF metadata was reported as invalid password: %v", err)
	}
	if prompts != 0 {
		t.Fatalf("password prompt called %d times before metadata validation", prompts)
	}
	if *calls != 0 {
		t.Fatalf("PBKDF2 invoked %d times at CLI entry", *calls)
	}
}

func TestInvalidKDFRejectedBeforeDeriveMCP(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)
	corruptKDFMetadataForCommandTest(t, cfg, data, 1_000_001)
	calls := countCommandDerivations(t)

	authorization, err := getMCPAuthorization(cfg, data)
	if authorization != nil {
		t.Fatal("MCP startup returned authorization for invalid KDF metadata")
	}
	if !errors.Is(err, storage.ErrInvalidKDFParameters) {
		t.Fatalf("MCP startup error = %v, want ErrInvalidKDFParameters", err)
	}
	if errors.Is(err, errInvalidPassword) {
		t.Fatalf("invalid KDF metadata was reported as invalid password: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("PBKDF2 invoked %d times at MCP entry", *calls)
	}
}

func TestInvalidKDFRejectedBeforeDeriveRekey(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)
	corruptKDFMetadataForCommandTest(t, cfg, data, 1_000_001)
	calls := countCommandDerivations(t)

	prompts := 0
	authPrompt = countingPrompter("unused", &prompts)
	err := runPasswd(nil, nil)
	if !errors.Is(err, storage.ErrInvalidKDFParameters) {
		t.Fatalf("runPasswd error = %v, want ErrInvalidKDFParameters", err)
	}
	if errors.Is(err, errInvalidPassword) {
		t.Fatalf("invalid KDF metadata was reported as invalid password: %v", err)
	}
	if prompts != 0 {
		t.Fatalf("rekey prompted %d times before metadata validation", prompts)
	}
	if *calls != 0 {
		t.Fatalf("PBKDF2 invoked %d times at rekey entry", *calls)
	}
}
