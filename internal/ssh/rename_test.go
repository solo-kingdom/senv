package ssh

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func importTestKeyPair(t *testing.T, mgr *Manager, name string) *KeyPairSummary {
	t.Helper()
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), name+"@test", "")
	summary, err := mgr.ImportKeyPair(name, path, false)
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	return summary
}

func TestRenameKeyPairUpdatesHostReferences(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	summary := importTestKeyPair(t, mgr, "web-key")

	for _, alias := range []string{"web", "web-2"} {
		if err := mgr.AddHost(&storage.HostEntry{
			Alias: alias, Hostname: alias + ".example.com", IdentityKey: "web-key",
		}); err != nil {
			t.Fatalf("add host %s: %v", alias, err)
		}
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "other", Hostname: "other.example.com"}); err != nil {
		t.Fatalf("add other: %v", err)
	}

	updated, err := mgr.RenameKeyPair("web-key", "prod-key")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	want := []string{"web", "web-2"}
	if len(updated) != len(want) || updated[0] != want[0] || updated[1] != want[1] {
		t.Fatalf("updated hosts = %#v, want %#v", updated, want)
	}

	renamed, err := mgr.GetKeyPairSummary("prod-key")
	if err != nil {
		t.Fatalf("get renamed: %v", err)
	}
	if renamed.Name != "prod-key" || renamed.Fingerprint != summary.Fingerprint {
		t.Errorf("renamed keypair = %+v, want name prod-key and unchanged fingerprint", renamed)
	}
	if _, err := mgr.GetKeyPairSummary("web-key"); err == nil {
		t.Error("old keypair name still resolvable")
	}
	for _, alias := range []string{"web", "web-2"} {
		host, err := mgr.GetHost(alias)
		if err != nil {
			t.Fatalf("get host %s: %v", alias, err)
		}
		if host.IdentityKey != "prod-key" {
			t.Errorf("host %s identityKey = %q, want prod-key", alias, host.IdentityKey)
		}
	}
	other, err := mgr.GetHost("other")
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	if other.IdentityKey != "" {
		t.Errorf("unrelated host gained identityKey %q", other.IdentityKey)
	}
}

func TestRenameKeyPairConflictsAndMissing(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	importTestKeyPair(t, mgr, "a-key")
	importTestKeyPair(t, mgr, "b-key")
	if err := mgr.AddHost(&storage.HostEntry{Alias: "h", Hostname: "h.example.com", IdentityKey: "a-key"}); err != nil {
		t.Fatalf("add host: %v", err)
	}

	if _, err := mgr.RenameKeyPair("a-key", "b-key"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflict error = %v, want already-exists", err)
	}
	if _, err := mgr.RenameKeyPair("missing", "c-key"); err == nil {
		t.Fatal("missing source should fail")
	}
	if _, err := mgr.RenameKeyPair("a-key", "bad/name"); err == nil {
		t.Fatal("invalid target name should fail")
	}

	// Every failed attempt must leave the vault untouched.
	if _, err := mgr.GetKeyPairSummary("a-key"); err != nil {
		t.Fatalf("source keypair broken: %v", err)
	}
	host, err := mgr.GetHost("h")
	if err != nil {
		t.Fatalf("host broken: %v", err)
	}
	if host.IdentityKey != "a-key" {
		t.Errorf("host identityKey changed by failed rename: %q", host.IdentityKey)
	}
	// A rename without hosts still works and reports zero updates.
	updated, err := mgr.RenameKeyPair("b-key", "c-key")
	if err != nil {
		t.Fatalf("rename without references: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("updated = %#v, want none", updated)
	}
	if _, err := mgr.GetKeyPairSummary("c-key"); err != nil {
		t.Fatalf("renamed keypair missing: %v", err)
	}
}
