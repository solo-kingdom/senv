package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
)

// newTestEnvManager builds a real env.Manager over a freshly initialized temp
// project, so manager-side effects (Set/Delete/Activate/...) are exercised.
func newTestEnvManager(t *testing.T) *env.Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return env.NewManager(sm, "pw")
}

// flush executes a command and feeds the resulting message chain back into the
// tab until no command remains (capped to avoid runaway loops). It skips the
// textinput blink loop naturally because an unrecognized msg yields a nil cmd.
func flush(t *envTab, cmd tea.Cmd) *envTab {
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
		t = next.(*envTab)
		cmd = nextCmd
	}
	return t
}

func hasEnvItem(t *envTab, group, key, value string) bool {
	for _, it := range t.itemsByGroup[group] {
		if it.key == key && (value == "" || it.value == value) {
			return true
		}
	}
	return false
}

func findGroup(t *envTab, name string) envGroupRow {
	for _, g := range t.groups {
		if g.name == name {
			return g
		}
	}
	return envGroupRow{name: name}
}

func groupIndexByName(t *envTab, name string) int {
	for i, g := range t.groups {
		if g.name == name {
			return i
		}
	}
	return -1
}

func TestEnvManagerOpsDirect(t *testing.T) {
	envMgr := newTestEnvManager(t)
	tab := newEnvTab(Managers{Env: envMgr})
	tab.SetSize(80, 20)
	tab = flush(tab, tab.load())

	if len(tab.groups) == 0 || tab.groups[0].name != "default" {
		t.Fatalf("expected default group first, got %#v", tab.groups)
	}
	if !tab.groups[0].isActive || !tab.groups[0].isDefault {
		t.Fatalf("default group must be active+default: %#v", tab.groups[0])
	}

	// Set two variables in default.
	tab = flush(tab, tab.doSet("default", "FOO", "bar"))
	tab = flush(tab, tab.doSet("default", "BAZ", "qux"))
	if !hasEnvItem(tab, "default", "FOO", "bar") {
		t.Error("FOO=bar missing after set")
	}
	if !hasEnvItem(tab, "default", "BAZ", "qux") {
		t.Error("BAZ=qux missing after set")
	}

	// Add a new group, then seed it with a key so it shows up in the listing
	// (empty non-default groups are hidden by the view layer).
	tab = flush(tab, tab.doAddGroup("prod"))
	tab = flush(tab, tab.doSet("prod", "KEY", "v"))
	if groupIndexByName(tab, "prod") < 0 {
		t.Fatal("prod group not visible after seeding a key")
	}
	tab.groupIndex = groupIndexByName(tab, "prod")
	tab = flush(tab, tab.doActivate())
	if g := findGroup(tab, "prod"); !g.isActive {
		t.Error("prod should be active after doActivate")
	}

	// Deactivate prod.
	tab = flush(tab, tab.doDeactivate())
	if g := findGroup(tab, "prod"); g.isActive {
		t.Error("prod should be inactive after doDeactivate")
	}

	// Deleting the default group via deactivate must be refused. The tab layer
	// intercepts the default group locally (returns a nil command + flash).
	tab.groupIndex = 0 // default
	cmd := tab.doDeactivate()
	if cmd != nil {
		t.Errorf("expected tab to refuse deactivating default locally, got a command")
	}

	// Delete a variable and confirm it disappears.
	tab.groupIndex = 0
	tab = flush(tab, tab.doDelete("default", "FOO"))
	if hasEnvItem(tab, "default", "FOO", "") {
		t.Error("FOO should be deleted")
	}
	if !hasEnvItem(tab, "default", "BAZ", "qux") {
		t.Error("BAZ should still exist")
	}
}

// TestEnvNewVarModalFlow drives the two-step "new variable" modal with the
// keyboard and asserts the value is persisted, covering tasks 5.1/5.2 end-to-end.
func TestEnvNewVarModalFlow(t *testing.T) {
	envMgr := newTestEnvManager(t)
	tab := newEnvTab(Managers{Env: envMgr})
	tab.SetSize(80, 20)
	tab = flush(tab, tab.load())

	// Press "n" to start the new-variable flow.
	tab = driveKey(tab, "n")
	if tab.mode != envModeNewKey {
		t.Fatalf("expected envModeNewKey, got %v", tab.mode)
	}

	// Type the key "API_KEY" (typing returns a blink cmd we can ignore).
	for _, r := range "API_KEY" {
		tab = driveKey(tab, string(r))
	}
	// Submit key -> advances to value entry.
	tab = driveKey(tab, "enter")
	if tab.mode != envModeNewValue {
		t.Fatalf("expected envModeNewValue, got %v", tab.mode)
	}
	if tab.pendingNewKey != "API_KEY" {
		t.Fatalf("pendingNewKey=%q want API_KEY", tab.pendingNewKey)
	}

	// Type the value.
	for _, r := range "sk-live-1234" {
		tab = driveKey(tab, string(r))
	}
	// Submit value: this triggers the manager mutation + reload, so flush.
	next, cmd := tab.Update(runeKey("enter"))
	tab = flush(next.(*envTab), cmd)

	if tab.mode != envModeNormal {
		t.Fatalf("expected normal mode after submit, got %v", tab.mode)
	}
	if !hasEnvItem(tab, "default", "API_KEY", "sk-live-1234") {
		t.Errorf("API_KEY not persisted: %#v", tab.itemsByGroup["default"])
	}
}

// TestEnvNewVarGroupKey adds a variable to a non-default group via group:key
// without selecting that group in the left pane (empty groups are hidden).
func TestEnvNewVarGroupKey(t *testing.T) {
	envMgr := newTestEnvManager(t)
	tab := newEnvTab(Managers{Env: envMgr})
	tab.SetSize(80, 20)
	tab = flush(tab, tab.load())
	tab.groupIndex = 0 // cursor on default

	tab = driveKey(tab, "n")
	for _, r := range "prod:SECRET" {
		tab = driveKey(tab, string(r))
	}
	tab = driveKey(tab, "enter")
	if tab.mode != envModeNewValue {
		t.Fatalf("expected envModeNewValue, got %v", tab.mode)
	}
	if tab.pendingNewGroup != "prod" || tab.pendingNewKey != "SECRET" {
		t.Fatalf("pending = %q/%q, want prod/SECRET", tab.pendingNewGroup, tab.pendingNewKey)
	}

	for _, r := range "top-secret" {
		tab = driveKey(tab, string(r))
	}
	next, cmd := tab.Update(runeKey("enter"))
	tab = flush(next.(*envTab), cmd)

	if groupIndexByName(tab, "prod") < 0 {
		t.Fatal("prod group should appear after adding a key via group:key")
	}
	if !hasEnvItem(tab, "prod", "SECRET", "top-secret") {
		t.Errorf("SECRET not persisted in prod: %#v", tab.itemsByGroup["prod"])
	}
}

// driveKey applies a key, ignoring any returned (blink) command. Used for
// keystrokes whose command is just cursor-animation and has no side effect to
// resolve in the test.
func driveKey(t *envTab, key string) *envTab {
	next, _ := t.Update(runeKey(key))
	return next.(*envTab)
}
