package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newLoadedEnvTab returns an env tab with data loaded from a real manager.
func newLoadedEnvTab(t *testing.T, mgrs Managers) *envTab {
	t.Helper()
	tab := newEnvTab(mgrs)
	tab.SetSize(80, 20)
	return flush(tab, tab.load())
}

func TestEnvRenameKeyViaForm(t *testing.T) {
	mgrs, audit := newAuditTestManagers(t)
	tab := newLoadedEnvTab(t, mgrs)
	flush(tab, tab.doSet("default", "OLD", "v"))

	tab.focusLeft = false
	tab.itemIndex = 0
	tabItem := tab.filteredItems()[0]
	if tabItem.key != "OLD" {
		t.Fatalf("cursor not on OLD: %#v", tabItem)
	}

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*envTab)
	if tab.form == nil {
		t.Fatal("r should open the rename form")
	}
	if !tab.InputMode() {
		t.Error("an open form must report InputMode")
	}
	// Replace the prefilled value.
	for range "OLD" {
		out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		tab = out.(*envTab)
	}
	for _, ch := range []string{"N", "E", "W"} {
		out, _ := tab.Update(runeKey(ch))
		tab = out.(*envTab)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*envTab)
	tab = flush(tab, cmd)
	if tab.form != nil {
		t.Fatal("submit should close the form")
	}
	if !hasEnvItem(tab, "default", "NEW", "v") {
		t.Fatalf("renamed key missing: %#v", tab.itemsByGroup["default"])
	}
	if hasEnvItem(tab, "default", "OLD", "") {
		t.Error("old key still present after rename")
	}
	last := audit.calls[len(audit.calls)-1]
	if !last.success || last.target != "env:default:NEW" || !strings.Contains(last.detail, "rename") {
		t.Fatalf("rename audit = %#v", audit.calls)
	}
}

func TestEnvRenameConflictKeepsFormOpen(t *testing.T) {
	mgrs, _ := newAuditTestManagers(t)
	tab := newLoadedEnvTab(t, mgrs)
	flush(tab, tab.doSet("default", "A", "1"))
	flush(tab, tab.doSet("default", "B", "2"))

	tab.focusLeft = false
	tab.itemIndex = 0
	out, _ := tab.Update(runeKey("r"))
	tab = out.(*envTab)
	for range tab.filteredItems()[0].key {
		out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		tab = out.(*envTab)
	}
	out, _ = tab.Update(runeKey("B"))
	tab = out.(*envTab)
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*envTab)
	if tab.form == nil {
		t.Fatal("conflicting rename must keep the form open")
	}
	if !strings.Contains(tab.View(), "已存在") {
		t.Errorf("inline conflict error missing: %q", tab.View())
	}
	if cmd != nil {
		if msgs := runCmd(cmd); len(msgs) > 0 {
			t.Fatalf("invalid submit produced commands: %#v", msgs)
		}
	}
}

func TestEnvRenameGroupAndDeleteGroup(t *testing.T) {
	mgrs, _ := newAuditTestManagers(t)
	tab := newLoadedEnvTab(t, mgrs)
	flush(tab, tab.doAddGroup("staging"))
	flush(tab, tab.doSet("staging", "K", "v"))
	tab.groupIndex = groupIndexByName(tab, "staging")
	tab.focusLeft = true

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*envTab)
	if tab.form == nil {
		t.Fatal("r on the group pane should open the group rename form")
	}
	for range "staging" {
		out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		tab = out.(*envTab)
	}
	for _, ch := range []string{"p", "r", "o", "d"} {
		out, _ := tab.Update(runeKey(ch))
		tab = out.(*envTab)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*envTab)
	tab = flush(tab, cmd)
	if groupIndexByName(tab, "prod") < 0 {
		t.Fatalf("renamed group missing: %#v", tab.groups)
	}
	if groupIndexByName(tab, "staging") >= 0 {
		t.Error("old group name still visible")
	}
	if !hasEnvItem(tab, "prod", "K", "v") {
		t.Error("variables were not carried over by the group rename")
	}

	// Delete the group: d on the group pane asks for confirmation first.
	tab.groupIndex = groupIndexByName(tab, "prod")
	tab.focusLeft = true
	out, _ = tab.Update(runeKey("d"))
	tab = out.(*envTab)
	if tab.mode != envModeDeleteGroupConfirm {
		t.Fatalf("mode = %v, want delete-group confirm", tab.mode)
	}
	if !strings.Contains(tab.View(), "删除分组 prod") {
		t.Errorf("confirm modal missing group name: %q", tab.View())
	}
	out, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*envTab)
	tab = flush(tab, cmd)
	if groupIndexByName(tab, "prod") >= 0 {
		t.Error("group still present after confirmed delete")
	}
}

func TestEnvDefaultGroupRenameAndDeleteRefused(t *testing.T) {
	mgrs, _ := newAuditTestManagers(t)
	tab := newLoadedEnvTab(t, mgrs)
	tab.groupIndex = groupIndexByName(tab, "default")
	tab.focusLeft = true

	out, cmd := tab.Update(runeKey("r"))
	tab = out.(*envTab)
	if tab.form != nil {
		t.Fatal("default group must not be renameable")
	}
	if msgs := runCmd(cmd); len(msgs) == 0 {
		t.Error("expected a warning toast for the refused rename")
	}
	out, cmd = tab.Update(runeKey("d"))
	tab = out.(*envTab)
	if tab.mode == envModeDeleteGroupConfirm {
		t.Fatal("default group must not be deletable")
	}
	if msgs := runCmd(cmd); len(msgs) == 0 {
		t.Error("expected a warning toast for the refused delete")
	}
}

func TestEnvDeleteActiveGroupWarnsAboutActivation(t *testing.T) {
	mgrs, _ := newAuditTestManagers(t)
	tab := newLoadedEnvTab(t, mgrs)
	flush(tab, tab.doAddGroup("prod"))
	flush(tab, tab.doSet("prod", "K", "v"))
	tab.groupIndex = groupIndexByName(tab, "prod")
	flush(tab, tab.doActivate())

	tab.groupIndex = groupIndexByName(tab, "prod")
	tab.focusLeft = true
	out, _ := tab.Update(runeKey("d"))
	tab = out.(*envTab)
	if !strings.Contains(tab.View(), "激活") {
		t.Errorf("active-group confirm must mention the activation loss: %q", tab.View())
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*envTab)
	tab = flush(tab, cmd)
	if groupIndexByName(tab, "prod") >= 0 {
		t.Error("active group not deleted after confirmation")
	}
	if out, err := mgrs.Env.Export(); err != nil {
		t.Fatalf("export after deleting active group: %v", err)
	} else if strings.Contains(out, "K=") {
		t.Errorf("export still contains deleted variables: %q", out)
	}
}
