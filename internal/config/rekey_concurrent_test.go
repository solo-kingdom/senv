package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

func TestConfigRekeyConcurrentMutationBoundary(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("password"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "source")
	if err := os.WriteFile(source, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- store.WithVaultMutation(func(*storage.Manager) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	done := make(chan error, 1)
	go func() { done <- NewManager(store, "password").Create("concurrent", source, "", "default", "") }()
	select {
	case err := <-done:
		t.Fatalf("config mutation crossed rekey boundary: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(base, "data", "concurrent.enc")); !os.IsNotExist(err) {
		t.Fatalf("config entry appeared while lock held: %v", err)
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
