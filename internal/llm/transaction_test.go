package llm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConfigTransactionSerializesSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	first, err := newConfigTransaction(path)
	if err != nil {
		t.Fatalf("first transaction: %v", err)
	}
	acquired := make(chan struct{}, 1)
	go func() {
		tx, err := newConfigTransaction(path)
		if err != nil {
			t.Errorf("second transaction: %v", err)
			close(acquired)
			return
		}
		acquired <- struct{}{}
		tx.unlock()
	}()
	select {
	case <-acquired:
		t.Fatal("second transaction acquired locked path")
	case <-time.After(25 * time.Millisecond):
	}
	if err := first.commit(); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second transaction did not acquire after commit")
	}
}

func TestConfigTransactionWritesPrivatelyAndCleansBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"value":"old"}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	tx, err := newConfigTransaction(path)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := tx.write(path, []byte(`{"value":"new"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := mustRead(t, path); !strings.Contains(string(got), "new") {
		t.Fatalf("config = %s", got)
	}
	if err := tx.commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".senv-bak-*"))
	if len(matches) != 0 {
		t.Fatalf("backups survived commit: %v", matches)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config perm = %o, want 600", info.Mode().Perm())
	}
}

func TestConfigTransactionRollbackRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := os.WriteFile(first, []byte(`{"id":1}`), 0o600); err != nil {
		t.Fatalf("write first: %v", err)
	}
	tx, err := newConfigTransaction(first, second)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := tx.write(first, []byte(`{"id":2}`)); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := tx.write(second, []byte(`{"id":3}`)); err != nil {
		t.Fatalf("write second: %v", err)
	}
	if err := tx.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	tx.unlock()
	if got := mustRead(t, first); string(got) != `{"id":1}` {
		t.Fatalf("first = %s", got)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("new second config survived rollback: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".senv-bak-*"))
	if len(matches) != 0 {
		t.Fatalf("successful rollback retained backups: %v", matches)
	}
}

func TestConfigTransactionConcurrentWritersHaveConsistentFinalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.json")
	if err := os.WriteFile(path, []byte(`{"value":0}`), 0o600); err != nil {
		t.Fatalf("write initial: %v", err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := newConfigTransaction(path)
			if err != nil {
				t.Errorf("transaction %d: %v", i, err)
				return
			}
			if err := tx.write(path, []byte(`{"value":`+string(rune('0'+i))+`}`)); err != nil {
				t.Errorf("write %d: %v", i, err)
				tx.unlock()
				return
			}
			if err := tx.commit(); err != nil {
				t.Errorf("commit %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	var final map[string]any
	if err := json.Unmarshal(mustRead(t, path), &final); err != nil {
		t.Fatalf("parse final config: %v", err)
	}
	value, ok := final["value"].(float64)
	if !ok || value < 1 || value > 4 {
		t.Fatalf("final config = %#v", final)
	}
}

func TestPiAdapterTransactionRollsBackFirstFile(t *testing.T) {
	home := t.TempDir()
	adapter := piAdapter()
	paths := adapter.ConfigPaths(home)
	if err := os.MkdirAll(filepath.Dir(paths[0]), 0o700); err != nil {
		t.Fatalf("create Pi config dir: %v", err)
	}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte(`{"before":"`+string(rune('a'+i))+`"}`), 0o600); err != nil {
			t.Fatalf("write original %s: %v", path, err)
		}
	}
	tx, err := newConfigTransaction(paths...)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	settings := paths[1]
	tx.renameHook = func(_, newName string) error {
		if newName == settings {
			return errors.New("injected second-file failure")
		}
		return nil
	}
	req := SwitchRequest{
		AgentID: adapter.ID, ProviderAlias: "main", BaseURL: "https://api.example.com",
		Model: "m1", Credential: "key", ConfigPath: paths[0], tx: tx,
	}
	if err := adapter.Apply(req); err == nil {
		t.Fatal("Pi Apply() unexpectedly survived second-file failure")
	}
	if err := tx.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	tx.unlock()
	for i, path := range paths {
		want := `{"before":"` + string(rune('a'+i)) + `"}`
		if got := mustRead(t, path); string(got) != want {
			t.Fatalf("path %s = %s, want %s", path, got, want)
		}
	}
}
