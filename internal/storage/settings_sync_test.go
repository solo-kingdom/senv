package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/provider"
)

func TestProviderAutoSyncSettings(t *testing.T) {
	settings := NewSettings()
	if settings.Provider.AutoSync != nil {
		t.Fatal("new settings should leave AutoSync nil for backward-compatible default-on")
	}
	if settings.Provider.SyncThrottle != "30s" {
		t.Fatalf("default sync_throttle = %q, want 30s", settings.Provider.SyncThrottle)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, SettingsFile)
	data := []byte(`{"provider":{"type":"server","auto_sync":false,"sync_throttle":"nope"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir, filepath.Join(dir, "data"))
	loaded, err := m.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider.AutoSync == nil || *loaded.Provider.AutoSync {
		t.Fatal("explicit auto_sync=false must round-trip as a non-nil false pointer")
	}
	// The provider package owns effective parsing; storage verifies the raw value survives.
	if loaded.Provider.SyncThrottle != "nope" {
		t.Fatalf("sync_throttle = %q, want raw invalid value retained for fallback", loaded.Provider.SyncThrottle)
	}

	if fallback := provider.ParseSyncThrottle("nope"); fallback != 30*time.Second {
		t.Fatalf("invalid throttle fallback = %s", fallback)
	}
}
