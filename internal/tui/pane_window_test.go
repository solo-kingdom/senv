package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// paneBudget is the rendered height of a two-pane tab: inner height plus the
// 2-row rounded border lipgloss draws outside Height().
func paneBudget(innerH int) int { return innerH + 2 }

func TestEnvTabLongItemListDoesNotPushGroupsOffScreen(t *testing.T) {
	items := make([]envItemRow, 40)
	for i := range items {
		items[i] = envItemRow{key: fmt.Sprintf("KEY_%02d", i), value: "secret-value"}
	}
	tab := envTabWith(items...)
	const innerH = 8
	tab.SetSize(80, innerH)
	tab.focusLeft = false
	tab.itemIndex = 0

	view := tab.View()
	if h := lipgloss.Height(view); h > paneBudget(innerH) {
		t.Fatalf("view height %d exceeds pane budget %d; the group list would scroll off screen", h, paneBudget(innerH))
	}
	if !contains(view, "Groups") {
		t.Fatalf("group sidebar missing from overflowing item list:\n%s", view)
	}
	if !contains(view, "KEY_00") {
		t.Fatalf("first item should be visible at the top of the window:\n%s", view)
	}
	if contains(view, "KEY_39") {
		t.Fatalf("last item should be scrolled out of a short pane:\n%s", view)
	}
}

func TestEnvTabItemWindowFollowsCursor(t *testing.T) {
	items := make([]envItemRow, 40)
	for i := range items {
		items[i] = envItemRow{key: fmt.Sprintf("KEY_%02d", i), value: "v"}
	}
	tab := envTabWith(items...)
	tab.SetSize(80, 8)
	tab.focusLeft = false
	tab.itemIndex = 39

	view := tab.View()
	if h := lipgloss.Height(view); h > paneBudget(8) {
		t.Fatalf("view height %d exceeds pane budget %d", h, paneBudget(8))
	}
	if !contains(view, "KEY_39") {
		t.Fatalf("selected item missing from windowed list:\n%s", view)
	}
	if contains(view, "KEY_00") {
		t.Fatalf("first item should have scrolled away:\n%s", view)
	}
	if !contains(view, "Groups") {
		t.Fatal("group sidebar missing when the item cursor is at the bottom")
	}
}

func TestEnvTabManyGroupsWindowSidebar(t *testing.T) {
	tab := envTabWith()
	tab.groups = make([]envGroupRow, 30)
	tab.itemsByGroup = map[string][]envItemRow{}
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("grp_%02d", i)
		tab.groups[i] = envGroupRow{name: name, varCount: 0}
		tab.itemsByGroup[name] = nil
	}
	tab.focusLeft = true
	tab.groupIndex = 29
	tab.SetSize(80, 8)

	view := tab.View()
	if h := lipgloss.Height(view); h > paneBudget(8) {
		t.Fatalf("view height %d exceeds pane budget %d", h, paneBudget(8))
	}
	if !contains(view, "grp_29") {
		t.Fatalf("selected group missing:\n%s", view)
	}
	if contains(view, "grp_00") {
		t.Fatalf("first group should have scrolled away:\n%s", view)
	}
}

func TestConfigTabLongItemListKeepsGroupsVisible(t *testing.T) {
	tab := newConfigTab(Managers{})
	tab.loaded = true
	items := make([]configRow, 40)
	for i := range items {
		items[i] = configRow{
			name: fmt.Sprintf("cfg_%02d", i), group: "work",
			targetPath: "/tmp/x", updatedAt: "now",
		}
	}
	tab.groups = []configGroupRow{{name: allConfigsLabel}, {name: "work"}}
	tab.itemsByGroup = map[string][]configRow{"work": items}
	tab.SetSize(80, 8)
	tab.clampCursors()
	tab.itemIndex = 39

	view := tab.View()
	if h := lipgloss.Height(view); h > paneBudget(8) {
		t.Fatalf("view height %d exceeds pane budget %d; group list would scroll off screen", h, paneBudget(8))
	}
	if !contains(view, "Groups") {
		t.Fatalf("group sidebar missing:\n%s", view)
	}
	if !contains(view, "cfg_39") {
		t.Fatalf("selected config missing from window:\n%s", view)
	}
	if contains(view, "cfg_00") {
		t.Fatalf("first config should have scrolled away:\n%s", view)
	}
}

func TestLongItemListDoesNotOverflowOuterFrame(t *testing.T) {
	m := sizedModel(80, 24)
	et := m.tabs[0].(*envTab)
	items := make([]envItemRow, 50)
	for i := range items {
		items[i] = envItemRow{key: fmt.Sprintf("KEY_%02d", i), value: "v"}
	}
	et.loaded = true
	et.groups = []envGroupRow{{name: "default", isDefault: true, isActive: true, varCount: len(items)}}
	et.itemsByGroup = map[string][]envItemRow{"default": items}
	et.focusLeft = false
	et.itemIndex = 49
	et.clampCursors()

	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Errorf("row count = %d, want exactly 24 (overflow scrolls the group list off screen)", len(lines))
	}
	if !strings.Contains(out, "Groups") {
		t.Error("group sidebar missing from framed view")
	}
	if !strings.Contains(out, "KEY_49") {
		t.Error("selected item missing from framed view")
	}
}
