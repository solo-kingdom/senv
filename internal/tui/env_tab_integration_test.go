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
func flush(t *envTab, cmd tea.Cmd) *envTab { return flushTab(t, cmd).(*envTab) }

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

	if len(tab.groups) < 2 || tab.groups[0].name != envAllLabel || !tab.groups[0].isAll {
		t.Fatalf("expected All pseudo-group first, got %#v", tab.groups)
	}
	if tab.groups[1].name != "default" {
		t.Fatalf("expected default group second, got %#v", tab.groups[1])
	}
	if !tab.groups[1].isActive || !tab.groups[1].isDefault {
		t.Fatalf("default group must be active+default: %#v", tab.groups[1])
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
	// intercepts the default group locally and reports a warning toast instead of
	// calling the manager.
	tab.groupIndex = 1 // default（All 伪组占 0）
	cmd := tab.doDeactivate()
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one warning toast, got %#v", msgs)
	}
	tm, ok := msgs[0].(toastMsg)
	if !ok || tm.level != toastWarn {
		t.Fatalf("expected warning toast, got %#v", msgs[0])
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

	// Select the default group (All pseudo-group occupies index 0).
	tab = driveKey(tab, "j")
	// Press "n" to open the structured new-variable form (grill D6-⑥).
	tab = driveKey(tab, "n")
	if tab.form == nil {
		t.Fatal("n should open the structured form")
	}

	// Fill key then value fields.
	tab.form = replaceFormText(t, tab.form, "API_KEY")
	tab.form, _ = tab.form.Update(tea.KeyMsg{Type: tea.KeyTab})
	tab.form = replaceFormText(t, tab.form, "sk-live-1234")
	// Submit: triggers the manager mutation + reload, so flush.
	next, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = flush(next.(*envTab), cmd)

	if tab.mode != envModeNormal {
		t.Fatalf("expected normal mode after submit, got %v", tab.mode)
	}
	if !hasEnvItem(tab, "default", "API_KEY", "sk-live-1234") {
		t.Errorf("API_KEY not persisted: %#v", tab.itemsByGroup["default"])
	}
}

// TestEnvNewVarGroupKey 在 All 视图下新建必须显式给出分组名（表单校验）。
func TestEnvNewVarGroupKey(t *testing.T) {
	envMgr := newTestEnvManager(t)
	tab := newEnvTab(Managers{Env: envMgr})
	tab.SetSize(80, 20)
	tab = flush(tab, tab.load())
	// 游标停在 All 伪组（index 0）：无默认落组，n 应提示并拒绝打开表单
	tab = driveKey(tab, "n")
	if tab.form != nil {
		t.Fatal("All view must refuse new-variable without a concrete group")
	}
}

// driveKey applies a key, ignoring any returned (blink) command. Used for
// keystrokes whose command is just cursor-animation and has no side effect to
// resolve in the test.
func driveKey(t *envTab, key string) *envTab {
	next, _ := t.Update(runeKey(key))
	return next.(*envTab)
}
