package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestPruneCandidatesAndDelete(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "prune@test", "")
	if _, err := mgr.ImportKeyPairWithGroup("used-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ImportKeyPairWithGroup("moved-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod", IdentityKey: "used-key"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "db", Hostname: "10.0.0.2", Group: "prod", IdentityKey: "moved-key"}); err != nil {
		t.Fatal(err)
	}

	// 布点：被引用（不动）、改组后的旧路径（InVault=true）、无主遗留（InVault=false）。
	keep := senvPath(t, "keys", "prod", "used-key")
	oldPath := senvPath(t, "keys", "prod", "moved-key")
	orphan := senvPath(t, "keys", "_ungrouped", "ghost-key")
	legacy := senvPath(t, "ghost-legacy")
	for _, p := range []string{keep, oldPath, orphan, legacy} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// moved-key 改组 → 新路径才被引用，旧路径入清单。
	if err := mgr.UpdateKeyPair("moved-key", func(e *storage.KeyPairEntry) error {
		e.Group = "staging"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := mgr.PruneCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates = %+v, want 3", candidates)
	}
	byPath := map[string]PruneCandidate{}
	for _, c := range candidates {
		byPath[c.Path] = c
		if c.Path == keep {
			t.Fatalf("referenced key must not be listed: %+v", c)
		}
	}
	if !byPath[oldPath].InVault {
		t.Fatalf("old-path candidate must be flagged InVault: %+v", byPath[oldPath])
	}
	if byPath[orphan].InVault || byPath[legacy].InVault {
		t.Fatalf("orphan/legacy candidates must not be InVault: %+v", byPath)
	}

	var paths []string
	for _, c := range candidates {
		paths = append(paths, c.Path)
	}
	deleted, err := DeletePrunedFiles(paths)
	if err != nil || len(deleted) != 3 {
		t.Fatalf("deleted = %v, %v", deleted, err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("referenced key must survive: %v", err)
	}
}

func TestPruneCandidatesIncludesPublicCompanion(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "prune@test", "")
	if _, err := mgr.ImportKeyPairWithGroup("used-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ImportKeyPairWithGroup("orphan-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod", IdentityKey: "used-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Materialize("used-key", false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Materialize("orphan-key", false); err != nil {
		t.Fatal(err)
	}

	candidates, err := mgr.PruneCandidates()
	if err != nil {
		t.Fatal(err)
	}
	keep := senvPath(t, "keys", "prod", "used-key")
	orphan := senvPath(t, "keys", "prod", "orphan-key")
	byPath := map[string]PruneCandidate{}
	for _, c := range candidates {
		byPath[c.Path] = c
		if strings.HasPrefix(c.Path, keep) {
			t.Fatalf("referenced key or companion must not be listed: %+v", c)
		}
	}
	if _, ok := byPath[orphan]; !ok {
		t.Fatalf("orphan private missing: %+v", byPath)
	}
	pub, ok := byPath[orphan+".pub"]
	if !ok {
		t.Fatalf("orphan public companion missing: %+v", byPath)
	}
	if !pub.InVault {
		t.Fatalf("orphan .pub should resolve to vault keypair: %+v", pub)
	}
}

func TestPruneCandidatesEmpty(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	candidates, err := mgr.PruneCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
}
