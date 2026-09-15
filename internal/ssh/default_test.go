package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestSetDefaultWritesFragmentAndInclude(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "def@test", "")
	if _, err := mgr.ImportKeyPairWithGroup("home-key", keyPath, "personal", false); err != nil {
		t.Fatal(err)
	}

	target, err := mgr.SetDefaultKeyPair("home-key")
	if err != nil {
		t.Fatal(err)
	}
	wantKey := senvPath(t, "keys", "personal", "home-key")
	if target != wantKey {
		t.Fatalf("target = %q, want %q", target, wantKey)
	}
	if _, err := os.Stat(wantKey); err != nil {
		t.Fatalf("materialized key: %v", err)
	}
	if _, err := os.Stat(wantKey + ".pub"); err != nil {
		t.Fatalf("materialized public key: %v", err)
	}
	frag := senvPath(t, "groups", "_default.conf")
	data, err := os.ReadFile(frag)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "Host *") || !strings.Contains(body, "IdentityFile "+wantKey) {
		t.Fatalf("default fragment:\n%s", body)
	}
	info, err := os.Stat(frag)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("fragment mode = %o, want 600", info.Mode().Perm())
	}
	cfg, err := os.ReadFile(filepath.Join(mustHome(t), ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), IncludeLine) {
		t.Fatalf("include not registered:\n%s", cfg)
	}
	name, err := mgr.DefaultKeyPairName()
	if err != nil || name != "home-key" {
		t.Fatalf("default name = %q, %v", name, err)
	}

	if _, err := mgr.SetDefaultKeyPair("home-key"); err != nil {
		t.Fatalf("idempotent set-default: %v", err)
	}
}

func TestClearDefaultRemovesFragmentOnly(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "def@test", "")
	if _, err := mgr.ImportKeyPair("home-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetDefaultKeyPair("home-key"); err != nil {
		t.Fatal(err)
	}
	keyFile := senvPath(t, "keys", "_ungrouped", "home-key")
	if err := ClearDefault(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(senvPath(t, "groups", "_default.conf")); !os.IsNotExist(err) {
		t.Fatalf("fragment must be gone: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("materialized key must remain: %v", err)
	}
	if name, err := mgr.DefaultKeyPairName(); err != nil || name != "" {
		t.Fatalf("cleared name = %q, %v", name, err)
	}
}

func TestDefaultFollowsRenameEditDelete(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "def@test", "")
	if _, err := mgr.ImportKeyPair("old-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetDefaultKeyPair("old-key"); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.RenameKeyPair("old-key", "new-key"); err != nil {
		t.Fatal(err)
	}
	want := senvPath(t, "keys", "_ungrouped", "new-key")
	got, err := DefaultIdentityFile()
	if err != nil || got != want {
		t.Fatalf("after rename identity = %q, %v want %q", got, err, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("renamed default must be materialized: %v", err)
	}
	if _, err := os.Stat(want + ".pub"); err != nil {
		t.Fatalf("renamed default public key missing: %v", err)
	}
	if name, err := mgr.DefaultKeyPairName(); err != nil || name != "new-key" {
		t.Fatalf("name after rename = %q, %v", name, err)
	}

	if err := mgr.UpdateKeyPair("new-key", func(e *storage.KeyPairEntry) error {
		e.Group = "work"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want = senvPath(t, "keys", "work", "new-key")
	got, err = DefaultIdentityFile()
	if err != nil || got != want {
		t.Fatalf("after edit-group identity = %q, %v want %q", got, err, want)
	}

	if _, err := mgr.DeleteKeyPair("new-key", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(senvPath(t, "groups", "_default.conf")); !os.IsNotExist(err) {
		t.Fatalf("delete must clear default fragment: %v", err)
	}
}

func TestApplyPreservesDefaultFragment(t *testing.T) {
	mgr := newApplyFixture(t)
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "def@test", "")
	if _, err := mgr.ImportKeyPair("solo-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetDefaultKeyPair("solo-key"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(senvPath(t, "groups", "_default.conf"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(senvPath(t, "groups", "_default.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("host apply rewrote _default.conf:\n%s", after)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "solo-key") {
			t.Fatalf("default key must not be warned as unreferenced: %v", res.Warnings)
		}
	}
	unregistered, groupsRemoved, err := mgr.Unexport()
	if err != nil || !unregistered || !groupsRemoved {
		t.Fatalf("unexport = %v %v %v", unregistered, groupsRemoved, err)
	}
	if _, err := os.Stat(senvPath(t, "groups", "_default.conf")); !os.IsNotExist(err) {
		t.Fatalf("unexport must remove default fragment: %v", err)
	}
	if _, err := os.Stat(senvPath(t, "keys", "_ungrouped", "solo-key")); err != nil {
		t.Fatalf("default key file must survive unexport: %v", err)
	}
}

func TestRenderDefaultGroupCollision(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	if err := mgr.AddHost(&storage.HostEntry{Alias: "x", Hostname: "x", Group: "_default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Render(RenderFilter{}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestPruneSkipsDefaultKey(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "def@test", "")
	if _, err := mgr.ImportKeyPair("solo-key", keyPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetDefaultKeyPair("solo-key"); err != nil {
		t.Fatal(err)
	}
	candidates, err := mgr.PruneCandidates()
	if err != nil {
		t.Fatal(err)
	}
	keep := senvPath(t, "keys", "_ungrouped", "solo-key")
	for _, c := range candidates {
		if c.Path == keep {
			t.Fatalf("default key must not be pruned: %+v", candidates)
		}
	}
}
