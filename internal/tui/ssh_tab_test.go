package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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
	var tab *sshTab
	for _, t := range model.tabs {
		if st, ok := t.(*sshTab); ok {
			tab = st
		}
	}
	if tab == nil {
		t.Fatalf("SSH tab not registered: %d tabs", len(model.tabs))
	}
	tab.SetSize(100, 20)
	next, cmd := tab.Update(sshLoadedMsg{hosts: []storage.HostEntry{{Alias: "web", Hostname: "web.example", IdentityKey: "secret"}}, keyPairs: []ssh.KeyPairSummary{{Name: "secret", Fingerprint: "SHA256:tui-test"}}})
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
	// Host 栏按别名字典序，游标（index 0）落在 db 上；KeyPair 栏已迁出本 Tab。
	if !strings.Contains(view, "▸ db") {
		t.Fatalf("selected row missing marker:\n%s", view)
	}
}

// TestSSHTabLoadingStateBeforeLoad 校验加载态范式：装载完成前渲染常驻双栏几何
// 并内嵌加载提示，不得误显带操作指引的空态文案（tui-tab-consistency-render）。
func TestSSHTabLoadingStateBeforeLoad(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	tab := newSSHTab(Managers{SSH: ssh.NewManager(store, "test-password")})
	tab.SetSize(100, 24)
	if tab.loaded {
		t.Fatal("fresh tab should not be loaded")
	}
	out := tab.View()
	if !strings.Contains(out, "loading SSH assets…") {
		t.Fatalf("loading hint missing before load: %q", clipRunesT(out, 120))
	}
	if strings.Contains(out, "no SSH assets yet") {
		t.Fatal("empty-state guidance shown while loading")
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 100 || h != 26 {
		t.Fatalf("loading view size = %dx%d, want 100x26 (persistent three-pane geometry)", w, h)
	}

	next, _ := tab.Update(sshLoadedMsg{})
	tab = next.(*sshTab)
	out = tab.View()
	if strings.Contains(out, "loading SSH assets…") {
		t.Fatal("loading hint still shown after load")
	}
	if !strings.Contains(out, "no SSH assets yet") {
		t.Fatalf("empty state missing after empty load: %q", clipRunesT(out, 120))
	}
}
