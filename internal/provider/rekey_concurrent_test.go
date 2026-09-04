package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

func TestProviderRekeyConcurrentSyncApplyBoundary(t *testing.T) {
	base := t.TempDir()
	configPath, dataPath := filepath.Join(base, "config"), filepath.Join(base, "data")
	store := storage.NewManager(configPath, dataPath)
	if err := store.Initialize("password"); err != nil {
		t.Fatal(err)
	}
	provider := &ServerProvider{cache: &localCache{configPath: configPath, dataPath: dataPath}}
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
	go func() {
		done <- provider.withVaultMutation(func() error {
			return provider.cache.apply(Entry{Kind: KindText, Grp: "notes", Key: "remote", Ciphertext: []byte("ciphertext")})
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("sync apply crossed rekey boundary: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dataPath, "texts", "notes", "remote.enc")); !os.IsNotExist(err) {
		t.Fatalf("remote entry appeared while lock held: %v", err)
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
