package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

func TestSSHTabMasksPrivateKey(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(store, "test-password")
	privateKey := "-----BEGIN OPENSSH PRIVATE KEY-----TUI-SECRET-----END OPENSSH PRIVATE KEY-----\n"
	if err := store.SaveKeyPair("secret", &storage.KeyPairEntry{
		Name:        "secret",
		PrivateKey:  privateKey,
		Fingerprint: "SHA256:tui-test",
		ImportedAt:  time.Now(),
	}, "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example", IdentityKey: "secret"}); err != nil {
		t.Fatal(err)
	}

	model := New(Managers{SSH: mgr})
	if model.tabs[len(model.tabs)-1].Title() != "SSH" {
		t.Fatalf("SSH tab not registered: %d tabs", len(model.tabs))
	}
	tab := model.tabs[len(model.tabs)-1].(*sshTab)
	tab.SetSize(80, 20)
	next, cmd := tab.Update(sshLoadedMsg{hosts: []storage.HostEntry{{Alias: "web", Hostname: "web.example"}}, keyPairs: []ssh.KeyPairSummary{{Name: "secret", Fingerprint: "SHA256:tui-test"}}})
	tab = next.(*sshTab)
	if cmd != nil {
		t.Fatal("loaded update unexpectedly returned a command")
	}
	view := tab.View()
	if !strings.Contains(view, "SHA256:tui-test") || !strings.Contains(view, "web") {
		t.Fatalf("SSH view missing metadata:\n%s", view)
	}
	if strings.Contains(view, "TUI-SECRET") || strings.Contains(view, "PRIVATE KEY") {
		t.Fatalf("SSH view leaked private-key material:\n%s", view)
	}
}

func TestSSHTabShowsSelectedRows(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(store, "test-password")
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "db", Hostname: "db.example"}); err != nil {
		t.Fatal(err)
	}

	tab := newSSHTab(Managers{SSH: mgr})
	tab.SetSize(80, 20)
	next, _ := tab.Update(sshLoadedMsg{
		hosts:    []storage.HostEntry{{Alias: "web"}, {Alias: "db"}},
		keyPairs: []ssh.KeyPairSummary{{Name: "old", Fingerprint: "SHA256:old"}, {Name: "new", Fingerprint: "SHA256:new"}},
	})
	tab = next.(*sshTab)
	view := tab.View()
	if !strings.Contains(view, "▸ web") || !strings.Contains(view, "▸ old") {
		t.Fatalf("selected rows missing markers:\n%s", view)
	}
}
