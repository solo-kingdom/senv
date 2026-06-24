package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/storage"
)

func newTestConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return config.NewManager(sm, "pw")
}

func writeSourceFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return p
}

func hasConfigItem(t *configTab, name string) bool {
	for _, it := range t.items {
		if it.name == name {
			return true
		}
	}
	return false
}

func flushConfig(t *configTab, cmd tea.Cmd) *configTab {
	const max = 16
	for i := 0; i < max; i++ {
		if cmd == nil {
			break
		}
		msg := cmd()
		if msg == nil {
			break
		}
		next, nextCmd := t.Update(msg)
		t = next.(*configTab)
		cmd = nextCmd
	}
	return t
}

func TestConfigLoadAndOps(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "a=1\n")
	if err := mgr.Create("app", src, "/etc/app.conf"); err != nil {
		t.Fatalf("create app: %v", err)
	}

	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(80, 20)
	tab = flushConfig(tab, tab.load())

	if len(tab.items) != 1 || tab.items[0].name != "app" {
		t.Fatalf("expected app, got %#v", tab.items)
	}
	if tab.items[0].targetPath != "/etc/app.conf" {
		t.Errorf("target = %q, want /etc/app.conf", tab.items[0].targetPath)
	}

	// Create another config via doCreate.
	src2 := writeSourceFile(t, "b=2\n")
	tab = flushConfig(tab, tab.doCreate("web", src2, "/etc/web.conf"))
	if !hasConfigItem(tab, "web") {
		t.Fatal("web config not created")
	}

	// Export to an explicit path.
	out := filepath.Join(t.TempDir(), "out.conf")
	tab = flushConfig(tab, tab.doExport("app", out))
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read exported: %v", err)
	}
	if string(data) != "a=1\n" {
		t.Errorf("exported = %q, want a=1", string(data))
	}

	// Detail lookup via showDetail (exercises Get).
	// Select "app" then open detail.
	tab.itemIndex = indexByName(tab, "app")
	next, cmd := tab.showDetail()
	tab = flushConfig(next.(*configTab), cmd)
	if tab.mode != configModeDetail || tab.detail == nil {
		t.Fatalf("expected detail mode, got mode=%v detail=%v", tab.mode, tab.detail)
	}
	if tab.detail.name != "app" || tab.detail.targetPath != "/etc/app.conf" {
		t.Errorf("detail = %#v", tab.detail)
	}

	// Return from detail on any key.
	tab, _ = applyKey(tab, "esc")
	if tab.mode != configModeNormal {
		t.Errorf("expected normal mode after detail dismiss, got %v", tab.mode)
	}

	// Delete.
	tab.itemIndex = indexByName(tab, "app")
	tab = flushConfig(tab, tab.doDelete(tab.items[tab.itemIndex].name))
	if hasConfigItem(tab, "app") {
		t.Error("app should be deleted")
	}

	// editCurrent builds a non-nil exec command (vim run is manual-verified).
	tab.itemIndex = indexByName(tab, "web")
	if _, cmd := tab.editCurrent(); cmd == nil {
		t.Error("editCurrent should return a non-nil exec command")
	}
}

func indexByName(t *configTab, name string) int {
	for i, it := range t.items {
		if it.name == name {
			return i
		}
	}
	return 0
}

func applyKey(t *configTab, key string) (*configTab, tea.Cmd) {
	next, cmd := t.Update(runeKey(key))
	return next.(*configTab), cmd
}
