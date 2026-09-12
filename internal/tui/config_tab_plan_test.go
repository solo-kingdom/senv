package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPlanTab creates a config whose target already exists with the given
// content, loads the tab, and opens the install or uninstall plan for it.
func setupPlanTab(t *testing.T, stored, onDisk, kind string, groupScope bool) (*configTab, string) {
	t.Helper()
	mgr := newTestConfigManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "app.conf")
	if onDisk != "" {
		if err := os.WriteFile(target, []byte(onDisk), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
	}
	src := writeSourceFile(t, stored)
	if err := mgr.Create("app", src, target, "work", "test config"); err != nil {
		t.Fatalf("create: %v", err)
	}

	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(100, 30)
	tab = flushConfig(tab, tab.load())
	items := allConfigItems(tab)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].group != "work" || items[0].description != "test config" {
		t.Errorf("row meta = %q/%q, want work/test config", items[0].group, items[0].description)
	}

	next, cmd := tab.enterPlan(kind, groupScope)
	tab = flushConfig(next.(*configTab), cmd)
	if tab.mode != configModePlan || tab.plan == nil {
		t.Fatalf("expected plan mode, got mode=%v", tab.mode)
	}
	return tab, target
}

func TestConfigTabInstallPlanConfirm(t *testing.T) {
	tab, target := setupPlanTab(t, "stored\n", "", "install", false)

	if tab.plan.installPlan == nil || len(tab.plan.installPlan.Items) != 1 {
		t.Fatalf("install plan = %+v", tab.plan)
	}
	if tab.plan.installPlan.Items[0].Action != "create" {
		t.Errorf("action = %q, want create", tab.plan.installPlan.Items[0].Action)
	}

	// Cancel path: esc cancels without writing.
	next, _ := tab.handlePlanKey(runeKey("esc"))
	tab = next.(*configTab)
	if tab.mode != configModeNormal || tab.plan != nil {
		t.Fatalf("expected cancel, got mode=%v", tab.mode)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("cancelled install must not write")
	}

	// Confirm path: y executes.
	next, cmd := tab.enterPlan("install", false)
	tab = flushConfig(next.(*configTab), cmd)
	next, cmd = tab.handlePlanKey(runeKey("y"))
	tab = flushConfig(next.(*configTab), cmd)
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "stored\n" {
		t.Errorf("installed content = %q, err=%v", string(data), err)
	}
	if tab.mode != configModeNormal {
		t.Errorf("expected normal mode after execute, got %v", tab.mode)
	}
}

func TestConfigTabUninstallChangedConfirm(t *testing.T) {
	tab, target := setupPlanTab(t, "stored\n", "local edits\n", "uninstall", true)

	if tab.plan.uninstallPlan == nil || !tab.plan.uninstallPlan.HasChanged() {
		t.Fatalf("expected changed uninstall plan, got %+v", tab.plan)
	}

	// Confirm plan -> moves into per-item changed confirmation.
	next, _ := tab.handlePlanKey(runeKey("y"))
	tab = next.(*configTab)
	if tab.mode != configModeChangedConfirm {
		t.Fatalf("expected changed-confirm mode, got %v", tab.mode)
	}

	// Deny deletion of the modified file.
	next, cmd := tab.handlePlanKey(runeKey("n"))
	tab = flushConfig(next.(*configTab), cmd)
	if _, err := os.Stat(target); err != nil {
		t.Error("denied changed file must be kept")
	}

	// Again, this time confirm deletion.
	next, cmd = tab.enterPlan("uninstall", true)
	tab = flushConfig(next.(*configTab), cmd)
	next, _ = tab.handlePlanKey(runeKey("y"))
	tab = next.(*configTab)
	next, cmd = tab.handlePlanKey(runeKey("y"))
	tab = flushConfig(next.(*configTab), cmd)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("confirmed changed file should be deleted")
	}
}

func TestConfigTabUninstallUnchangedDirectDelete(t *testing.T) {
	tab, target := setupPlanTab(t, "same\n", "same\n", "uninstall", false)

	if tab.plan.uninstallPlan.HasChanged() {
		t.Fatal("unchanged file must not require changed confirmation")
	}
	next, cmd := tab.handlePlanKey(runeKey("y"))
	tab = flushConfig(next.(*configTab), cmd)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("unchanged file should be deleted directly")
	}
	if tab.mode != configModeNormal {
		t.Errorf("expected normal mode, got %v", tab.mode)
	}
}

func TestConfigTabAllViewGroupInstallUsesAllScope(t *testing.T) {
	tab := setupSidebarTab(t)
	if tab.currentGroup() != "" {
		t.Fatalf("expected All view, group=%q", tab.currentGroup())
	}

	next, cmd := tab.enterPlan("install", true)
	tab = flushConfig(next.(*configTab), cmd)
	if tab.mode != configModePlan || tab.plan == nil {
		t.Fatalf("expected plan mode, got mode=%v", tab.mode)
	}
	if !tab.plan.scope.All {
		t.Errorf("plan scope = %+v, want All when I is pressed in All view", tab.plan.scope)
	}
	if tab.plan.installPlan == nil || len(tab.plan.installPlan.Items) != 2 {
		t.Errorf("All-view I plan = %+v, want 2 items", tab.plan.installPlan)
	}
}

func TestConfigInstallPlanColumnsAlign(t *testing.T) {
	short := formatPlanLine("create", "app", "/tmp/app.conf", "target does not exist", 80)
	long := formatPlanLine("backup_overwrite", "longer-name", "/tmp/other.conf", "target content differs", 80)

	arrowShort := strings.Index(short, "->")
	arrowLong := strings.Index(long, "->")
	if arrowShort < 0 || arrowLong < 0 {
		t.Fatalf("missing arrow in plan lines:\n%s\n%s", short, long)
	}
	if arrowShort != arrowLong {
		t.Errorf("plan columns misaligned: arrow at %d vs %d\n%s\n%s", arrowShort, arrowLong, short, long)
	}
}

func TestConfigItemRowsShareColumnPrefix(t *testing.T) {
	selected := formatConfigItemLine("work/app", "main app", "/tmp/app.conf", "now", 20, 70, true)
	other := formatConfigItemLine("personal/db", "", "/tmp/db.conf", "now", 20, 70, false)

	pathSel := strings.Index(afterCursorPrefix(selected), "/tmp")
	pathOther := strings.Index(afterCursorPrefix(other), "/tmp")
	if pathSel < 0 || pathOther < 0 {
		t.Fatalf("missing path in item rows:\n%s\n%s", selected, other)
	}
	if pathSel != pathOther {
		t.Errorf("item columns misaligned: path at %d vs %d\n%s\n%s", pathSel, pathOther, selected, other)
	}
}

func afterCursorPrefix(s string) string {
	r := []rune(s)
	if len(r) >= 2 {
		return string(r[2:])
	}
	return s
}

func TestConfigTabFilterMatchesGroupAndDescription(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "a\n")
	if err := mgr.Create("app", src, "/tmp/app.conf", "work", "main app"); err != nil {
		t.Fatalf("create: %v", err)
	}
	src2 := writeSourceFile(t, "b\n")
	if err := mgr.Create("db", src2, "/tmp/db.conf", "personal", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	tab := newConfigTab(Managers{Config: mgr})
	tab = flushConfig(tab, tab.load())

	tab.filterBox = Filter{term: "work"}
	if got := len(tab.filteredItems()); got != 1 || tab.filteredItems()[0].name != "app" {
		t.Errorf("filter by group = %d items, want app only", got)
	}
	tab.filterBox = Filter{term: "main app"}
	if got := len(tab.filteredItems()); got != 1 || tab.filteredItems()[0].name != "app" {
		t.Errorf("filter by description = %d items, want app only", got)
	}
}
