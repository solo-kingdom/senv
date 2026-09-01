package tui

import (
	"testing"
)

// setupSidebarTab creates two configs in different groups and loads the tab.
func setupSidebarTab(t *testing.T) *configTab {
	t.Helper()
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "a\n")
	if err := mgr.Create("app", src, "/tmp/app.conf", "work", "main app"); err != nil {
		t.Fatalf("create app: %v", err)
	}
	src2 := writeSourceFile(t, "b\n")
	if err := mgr.Create("db", src2, "/tmp/db.conf", "personal", ""); err != nil {
		t.Fatalf("create db: %v", err)
	}
	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(100, 30)
	return flushConfig(tab, tab.load())
}

func TestConfigTabSidebarDefaultsToAll(t *testing.T) {
	tab := setupSidebarTab(t)

	if len(tab.groups) != 3 { // All + personal + work
		t.Fatalf("groups = %#v, want All + 2 real groups", tab.groups)
	}
	if tab.groups[0].name != allConfigsLabel {
		t.Errorf("groups[0] = %q, want %q", tab.groups[0].name, allConfigsLabel)
	}
	if tab.currentGroup() != "" {
		t.Errorf("currentGroup = %q, want All (empty)", tab.currentGroup())
	}
	if got := len(tab.filteredItems()); got != 2 {
		t.Errorf("All view items = %d, want 2", got)
	}
	if got := tab.sidebarCount(0); got != 2 {
		t.Errorf("All count = %d, want 2", got)
	}
	if got := tab.View(); !contains(got, "work/app") || !contains(got, "personal/db") {
		t.Errorf("All view should show group/name prefixes, got:\n%s", got)
	}
}

func TestConfigTabSidebarNavigation(t *testing.T) {
	tab := setupSidebarTab(t)

	// Focus the sidebar and move to the "work" group (sorted: personal, work).
	tab, _ = applyKey(tab, "h")
	if !tab.focusLeft {
		t.Fatal("h should focus the sidebar")
	}
	tab, _ = applyKey(tab, "j")
	tab, _ = applyKey(tab, "j")
	if tab.currentGroup() != "work" {
		t.Fatalf("currentGroup = %q, want work", tab.currentGroup())
	}
	// Selecting a group narrows the item list and resets the item cursor.
	if got := tab.filteredItems(); len(got) != 1 || got[0].name != "app" {
		t.Fatalf("work items = %#v, want app only", got)
	}

	// g/G jump within the focused pane.
	tab, _ = applyKey(tab, "g")
	if tab.groupIndex != 0 {
		t.Errorf("g in sidebar should jump to All, groupIndex = %d", tab.groupIndex)
	}

	// Moving right focuses the item list; j/k then move the item cursor.
	tab, _ = applyKey(tab, "l")
	if tab.focusLeft {
		t.Fatal("l should focus the item list")
	}
	tab, _ = applyKey(tab, "j")
	if tab.itemIndex != 1 {
		t.Errorf("itemIndex = %d, want 1 after j in All view", tab.itemIndex)
	}
	tab, _ = applyKey(tab, "G")
	if tab.itemIndex != len(tab.filteredItems())-1 {
		t.Errorf("G should jump to last item, itemIndex = %d", tab.itemIndex)
	}
}

func TestConfigTabSidebarGroupScopePlan(t *testing.T) {
	tab := setupSidebarTab(t)

	// Sidebar focused on "work": I plans a group-scope install.
	tab, _ = applyKey(tab, "h")
	tab, _ = applyKey(tab, "j")
	tab, _ = applyKey(tab, "j") // work
	next, cmd := tab.enterSidebarPlan("install")
	tab = flushConfig(next.(*configTab), cmd)
	if tab.mode != configModePlan || tab.plan == nil {
		t.Fatalf("expected plan mode, got mode=%v", tab.mode)
	}
	if tab.plan.scope.Group != "work" {
		t.Errorf("plan scope = %+v, want group work", tab.plan.scope)
	}
	if tab.plan.installPlan == nil || len(tab.plan.installPlan.Items) != 1 {
		t.Errorf("work install plan = %+v, want 1 item", tab.plan.installPlan)
	}
}

func TestConfigTabSidebarAllRejectsGroupPlan(t *testing.T) {
	tab := setupSidebarTab(t)

	tab, _ = applyKey(tab, "h") // sidebar focused on All
	next, _ := tab.enterSidebarPlan("install")
	tab = next.(*configTab)
	if tab.mode == configModePlan || tab.plan != nil {
		t.Fatal("All pseudo-group must not open a plan")
	}
	if tab.flash == "" {
		t.Error("expected a flash hint explaining All has no group scope")
	}
}

func TestConfigTabFocusJumpSelectsGroup(t *testing.T) {
	tab := setupSidebarTab(t)

	tab.focusJump("work", "app")
	if tab.focusLeft {
		t.Error("jump should leave focus on the item list")
	}
	if tab.currentGroup() != "work" {
		t.Errorf("group = %q, want work", tab.currentGroup())
	}
	it, ok := tab.currentItem()
	if !ok || it.name != "app" {
		t.Fatalf("current item = %+v, want app", it)
	}
}

func TestConfigTabFocusJumpFallsBackToAll(t *testing.T) {
	tab := setupSidebarTab(t)

	// Unknown group: land in All and still find the entry by name.
	tab.focusJump("nosuch", "db")
	if tab.currentGroup() != "" {
		t.Errorf("group = %q, want All fallback", tab.currentGroup())
	}
	it, ok := tab.currentItem()
	if !ok || it.name != "db" {
		t.Fatalf("current item = %+v, want db", it)
	}
}

func TestConfigTabFilterUpdatesSidebarCounts(t *testing.T) {
	tab := setupSidebarTab(t)

	tab.filter = "app"
	if got := len(tab.filteredItems()); got != 1 {
		t.Fatalf("filtered items = %d, want 1", got)
	}
	if got := tab.sidebarCount(0); got != 1 {
		t.Errorf("All count under filter = %d, want 1", got)
	}
	// Groups with no matches stay visible with a zero count.
	for i := 1; i < len(tab.groups); i++ {
		want := 0
		if tab.groups[i].name == "work" {
			want = 1
		}
		if got := tab.sidebarCount(i); got != want {
			t.Errorf("count[%s] = %d, want %d", tab.groups[i].name, got, want)
		}
	}

	// Clearing the filter restores real counts.
	tab.filter = ""
	if got := tab.sidebarCount(0); got != 2 {
		t.Errorf("All count after clearing filter = %d, want 2", got)
	}
}

func TestConfigTabCreateFocusesNewEntryGroup(t *testing.T) {
	tab := setupSidebarTab(t)

	src := writeSourceFile(t, "c\n")
	tab = flushConfig(tab, tab.doCreate("cache", src, "/tmp/cache.conf", "work", ""))

	if tab.pendingFocusName != "" {
		t.Error("pending focus should be consumed after reload")
	}
	if tab.currentGroup() != "work" {
		t.Errorf("group = %q, want work (new entry's group)", tab.currentGroup())
	}
	it, ok := tab.currentItem()
	if !ok || it.name != "cache" {
		t.Fatalf("current item = %+v, want cache", it)
	}
}
