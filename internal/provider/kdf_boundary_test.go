package provider

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestInvalidSyncedKDFMetadataRejectedBeforeApply(t *testing.T) {
	configPath := t.TempDir()
	dataPath := t.TempDir()
	manager := storage.NewManager(configPath, dataPath)
	if err := manager.Initialize("correct-secret"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	metadataPath := filepath.Join(configPath, storage.MetadataFile)
	beforeMetadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	cache := &localCache{configPath: configPath, dataPath: dataPath}
	invalidMetadata := []byte(`{"version":"1.0","salt":"c2FsdA==","password_key":"key","kdf_iterations":1000001}`)

	err = cache.applyRemote(nil, invalidMetadata, true, &syncState{LastSyncedRevision: 42, Entries: map[string]syncEntryState{}})
	if !errors.Is(err, storage.ErrInvalidKDFParameters) {
		t.Fatalf("applyRemote error = %v, want ErrInvalidKDFParameters", err)
	}
	afterMetadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata after rejection: %v", err)
	}
	if !bytes.Equal(afterMetadata, beforeMetadata) {
		t.Fatal("invalid synced metadata changed local metadata")
	}
	if _, err := os.Stat(filepath.Join(dataPath, syncStateFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid synced metadata wrote sync state: %v", err)
	}
}
