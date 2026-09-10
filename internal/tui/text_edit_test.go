package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newLoadedTextTab(t *testing.T) *textTab {
	t.Helper()
	tab := newTextTab(Managers{Text: newTestTextManager(t)})
	tab.SetSize(80, 20)
	return flushText(tab, tab.load())
}

func replaceFormText(t *testing.T, f *form, value string) *form {
	t.Helper()
	for range []rune(f.input.Value()) {
		next, _ := f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		f = next
	}
	for _, ch := range value {
		next, _ := f.Update(runeKey(string(ch)))
		f = next
	}
	return f
}

func TestTextRenameKeyViaForm(t *testing.T) {
	tab := newLoadedTextTab(t)
	if err := tab.mgr.Text.Set("default", "old", "content"); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	tab = flushText(tab, tab.load())

	tab.focusLeft = false
	tab.itemIndex = 0
	out, _ := tab.Update(runeKey("r"))
	tab = out.(*textTab)
	if tab.form == nil {
		t.Fatal("r should open the rename form")
	}
	tab.form = replaceFormText(t, tab.form, "new")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*textTab)
	tab = flushText(tab, cmd)

	if !hasTextItem(tab, "default", "new") {
		t.Fatalf("renamed block missing: %#v", tab.itemsByGroup["default"])
	}
	if hasTextItem(tab, "default", "old") {
		t.Error("old key still present after rename")
	}
}

func TestTextRenameGroupAndDeleteGroup(t *testing.T) {
	tab := newLoadedTextTab(t)
	flushText(tab, tab.doAddGroup("notes"))
	if err := tab.mgr.Text.Set("notes", "todo", "buy milk"); err != nil {
		t.Fatalf("seed block: %v", err)
	}
	tab = flushText(tab, tab.load())
	tab.groupIndex = indexOfTextGroup(tab, "notes")
	tab.focusLeft = true

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*textTab)
	if tab.form == nil {
		t.Fatal("r on the group pane should open the group rename form")
	}
	tab.form = replaceFormText(t, tab.form, "journal")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*textTab)
	tab = flushText(tab, cmd)
	if !hasTextGroup(tab, "journal") || hasTextGroup(tab, "notes") {
		t.Fatalf("group rename failed: %#v", tab.groups)
	}

	tab.groupIndex = indexOfTextGroup(tab, "journal")
	tab.focusLeft = true
	out, _ = tab.Update(runeKey("d"))
	tab = out.(*textTab)
	if tab.mode != textModeDeleteGroupConfirm {
		t.Fatalf("mode = %v, want delete-group confirm", tab.mode)
	}
	if !strings.Contains(tab.View(), "删除分组 journal") {
		t.Errorf("confirm modal missing: %q", tab.View())
	}
	out, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*textTab)
	tab = flushText(tab, cmd)
	if hasTextGroup(tab, "journal") {
		t.Error("group still present after confirmed delete")
	}
	// Confirm was required: the default group can never be deleted.
	tab.groupIndex = indexOfTextGroup(tab, "default")
	tab.focusLeft = true
	out, cmd = tab.Update(runeKey("d"))
	tab = out.(*textTab)
	if tab.mode == textModeDeleteGroupConfirm {
		t.Fatal("default group must not be deletable")
	}
	if msgs := runCmd(cmd); len(msgs) == 0 {
		t.Error("expected a warning toast for the refused delete")
	}
}

func TestTextImportFromFile(t *testing.T) {
	tab := newLoadedTextTab(t)
	src := filepath.Join(t.TempDir(), "import.txt")
	if err := os.WriteFile(src, []byte("imported content"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	out, _ := tab.Update(runeKey("i"))
	tab = out.(*textTab)
	if tab.form == nil {
		t.Fatal("i should open the import form")
	}
	// 分组: keep the prefilled current group, move to the key field.
	tab.form, _ = tab.form.Update(tea.KeyMsg{Type: tea.KeyTab})
	tab.form = replaceFormText(t, tab.form, "imported")
	tab.form, _ = tab.form.Update(tea.KeyMsg{Type: tea.KeyTab})
	tab.form = replaceFormText(t, tab.form, src)
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*textTab)
	tab = flushText(tab, cmd)
	if !hasTextItem(tab, "default", "imported") {
		t.Fatalf("imported block missing: %#v", tab.itemsByGroup["default"])
	}
	got, err := tab.mgr.Text.Get("default", "imported")
	if err != nil {
		t.Fatalf("get imported: %v", err)
	}
	if got != "imported content" {
		t.Errorf("imported content = %q", got)
	}
}

func indexOfTextGroup(t *textTab, name string) int {
	for i, g := range t.groups {
		if g.name == name {
			return i
		}
	}
	return -1
}
