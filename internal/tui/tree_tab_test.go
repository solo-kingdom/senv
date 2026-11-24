package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/tree"
)

func newTreeTestTab(t *testing.T) *treeTab {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	key, err := sm.DeriveKeyFromPassword("pw")
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	tm := tree.NewManager(sm, key)
	if err := tm.AddGroup("deploy", "deployment configs"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if _, err := tm.Set("deploy", "tcbj/caddy/Caddyfile", []byte("senv.lan {}\n"), 0o644, "caddy", false); err != nil {
		t.Fatalf("set 1: %v", err)
	}
	if _, err := tm.Set("deploy", "lnv/dns/db.lan", []byte("* IN A 192.168.6.12\n"), 0o644, "dns", false); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	tab := newTreeTab(Managers{Tree: tm, AuditWriter: &fakeAuditWriter{}})
	tab.SetSize(120, 24)
	out, _ := tab.Update(flushTab(tab, tab.Init()))
	return out.(*treeTab)
}

func TestTreeTabExpandFilterDetailAndJump(t *testing.T) {
	tab := newTreeTestTab(t)
	if !tab.loaded || len(tab.metas) != 2 {
		t.Fatalf("loaded=%v metas=%d", tab.loaded, len(tab.metas))
	}
	// Initially collapsed: 1 root group node.
	if n := len(tab.navNodes()); n != 1 {
		t.Fatalf("collapsed navNodes = %d, want 1", n)
	}
	// Space expands deploy -> shows lnv/ and tcbj/.
	next, _ := tab.Update(tea.KeyMsg{Type: tea.KeySpace})
	tab = next.(*treeTab)
	if n := len(tab.navNodes()); n != 3 {
		t.Fatalf("expanded navNodes = %d, want 3", n)
	}

	// Jump from global search lands on right pane and selects target.
	tab.focusJump("deploy", "tcbj/caddy/Caddyfile")
	mt, ok := tab.currentMeta()
	if !ok || mt.Path != "tcbj/caddy/Caddyfile" {
		t.Fatalf("focusJump got %+v ok=%v", mt, ok)
	}

	// Open detail overlay renders file content.
	next, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = next.(*treeTab)
	next, _ = tab.Update(cmd())
	tab = next.(*treeTab)
	if tab.detail == nil {
		t.Fatal("expected detail overlay")
	}
	if view := tab.View(); !strings.Contains(view, "senv.lan") {
		t.Fatalf("detail view missing content: %q", view)
	}
}
