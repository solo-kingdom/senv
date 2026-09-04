package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// allConfigItems returns every entry across groups (the "All" sidebar view).
func allConfigItems(t *configTab) []configRow {
	saved := t.groupIndex
	t.groupIndex = 0
	defer func() { t.groupIndex = saved }()
	return t.filteredItems()
}

func hasConfigItem(t *configTab, name string) bool {
	for _, it := range allConfigItems(t) {
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
	if err := mgr.Create("app", src, "/etc/app.conf", "", ""); err != nil {
		t.Fatalf("create app: %v", err)
	}

	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(80, 20)
	tab = flushConfig(tab, tab.load())

	items := allConfigItems(tab)
	if len(items) != 1 || items[0].name != "app" {
		t.Fatalf("expected app, got %#v", items)
	}
	if items[0].targetPath != "/etc/app.conf" {
		t.Errorf("target = %q, want /etc/app.conf", items[0].targetPath)
	}

	// Create another config via doCreate.
	src2 := writeSourceFile(t, "b=2\n")
	tab = flushConfig(tab, tab.doCreate("web", src2, "/etc/web.conf", "", ""))
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
	tab = flushConfig(tab, tab.doDelete(allConfigItems(tab)[tab.itemIndex].name))
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
	for i, it := range allConfigItems(t) {
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

func TestConfigTabShowsQuarantineWarning(t *testing.T) {
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	index := &storage.ConfigIndex{Configs: map[string]storage.ConfigFile{
		"database-prod": {Name: "database-prod", EncryptedFile: "database-prod.enc", Group: "default"},
		"feg:ai-ops-portal.pub": {
			Name: "feg:ai-ops-portal.pub", EncryptedFile: "feg:ai-ops-portal.pub.enc", Group: "default",
		},
	}}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sm.GetConfigPath(), storage.ConfigIndexFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := config.NewManager(sm, "pw")

	model := New(Managers{Config: mgr})
	next, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = next.(Model)

	// Load the config tab and replay its messages into the model so both
	// configLoadedMsg and the resulting warnMsg are processed.
	tab := newConfigTab(model.mgr)
	tab.SetSize(80, 20)
	cmd := tab.load()
	var warnCmd tea.Cmd
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		nextTab, nextCmd := tab.Update(msg)
		tab = nextTab.(*configTab)
		cmd = nextCmd
		if nextCmd != nil && warnCmd == nil {
			warnCmd = nextCmd
		}
	}
	if warnCmd != nil {
		if warn, ok := warnCmd().(warnMsg); ok {
			nextModel, _ := model.Update(warn)
			model = nextModel.(Model)
		}
	}

	if !hasConfigItem(tab, "database-prod") {
		t.Fatal("valid entry missing from config tab")
	}
	if hasConfigItem(tab, "feg:ai-ops-portal.pub") {
		t.Fatal("quarantined entry leaked into config tab")
	}
	view := model.View()
	if !strings.Contains(view, "feg:ai-ops-portal.pub") || !strings.Contains(view, "senv config repair") {
		t.Fatalf("warning missing from view:\n%s", view)
	}
}
