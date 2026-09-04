package text

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

func TestTextRekeyConcurrentMutationBoundary(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("password"); err != nil {
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
	go func() { done <- NewManager(store, "password").Set("notes", "CONCURRENT", "value") }()
	select {
	case err := <-done:
		t.Fatalf("text mutation crossed rekey boundary: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(base, "data", storage.TextDirName, "notes", "CONCURRENT.enc")); !os.IsNotExist(err) {
		t.Fatalf("text entry appeared while lock held: %v", err)
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
