package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/ledger"
	"github.com/wii/senv/internal/storage"
)

func newLedgerTestTab(t *testing.T) *ledgerTab {
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
	w := &fakeAuditWriter{}
	tab := newLedgerTab(Managers{
		Ledger:      ledger.NewManager(sm, key),
		AuditWriter: w,
	})
	tab.SetSize(120, 24)
	return tab
}

func seedLedgerHead(t *testing.T, tab *ledgerTab, group, typ, id, desc string) {
	t.Helper()
	h, err := ledger.NewHead(typ, id, desc, nil)
	if err != nil {
		t.Fatalf("new head: %v", err)
	}
	if err := tab.mgr.Ledger.SaveHead(group, h); err != nil {
		t.Fatalf("save head: %v", err)
	}
}

func loadLedgerTab(t *testing.T, tab *ledgerTab) *ledgerTab {
	t.Helper()
	out, _ := tab.Update(flushTab(tab, tab.Init()))
	return out.(*ledgerTab)
}

func TestLedgerTabGroupsAndHeads(t *testing.T) {
	tab := newLedgerTestTab(t)
	seedLedgerHead(t, tab, "personal", "device", "lnv", "self-hosted node")
	seedLedgerHead(t, tab, "feg", "service", "api", "ad api")
	tab = loadLedgerTab(t, tab)

	if !tab.loaded || len(tab.heads) != 2 {
		t.Fatalf("loaded=%v heads=%d", tab.loaded, len(tab.heads))
	}
	if got := tab.groupNames(tab.heads); len(got) != 2 || got[0] != "feg" || got[1] != "personal" {
		t.Fatalf("groups = %v", got)
	}
	// "All" pseudo-group shows every head.
	if n := len(tab.filteredHeads()); n != 2 {
		t.Fatalf("All heads = %d, want 2", n)
	}
	// Selecting a group narrows the list.
	tab.groupIndex = 1 // feg
	if n := len(tab.filteredHeads()); n != 1 {
		t.Fatalf("feg heads = %d, want 1", n)
	}
}

func TestLedgerTabJumpAndAppend(t *testing.T) {
	tab := newLedgerTestTab(t)
	seedLedgerHead(t, tab, "personal", "device", "lnv", "self-hosted node")
	tab = loadLedgerTab(t, tab)

	tab.focusJump("personal", "device.lnv")
	if h, ok := tab.currentHead(); !ok || h.ID != "lnv" {
		t.Fatalf("focusJump did not land on device.lnv: %+v ok=%v", h, ok)
	}

	next, _ := tab.enterAppend()
	tab = next.(*ledgerTab)
	if tab.form == nil {
		t.Fatal("append form did not open")
	}
	cmd := tab.formSubmit(map[string]string{"summary": "upgraded", "kind": "op", "body": "body"})
	if cmd == nil {
		t.Fatal("append submit returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(ledgerReloadMsg); !ok {
		t.Fatalf("append msg = %T, want ledgerReloadMsg", msg)
	}
	recs, err := tab.mgr.Ledger.Log("personal", "device.lnv", 10)
	if err != nil || len(recs) != 1 || recs[0].Summary != "upgraded" {
		t.Fatalf("records = %+v err=%v", recs, err)
	}
}

func TestLedgerTabStatusAndField(t *testing.T) {
	tab := newLedgerTestTab(t)
	seedLedgerHead(t, tab, "personal", "device", "lnv", "node")
	tab = loadLedgerTab(t, tab)
	tab.focusJump("personal", "device.lnv")

	// set status
	next, _ := tab.enterSetStatus()
	tab = next.(*ledgerTab)
	cmd := tab.formSubmit(map[string]string{"text": "online", "level": "ok"})
	if _, ok := cmd().(ledgerReloadMsg); !ok {
		t.Fatal("set-status did not return reload")
	}
	h, err := tab.mgr.Ledger.GetHead("personal", "device.lnv")
	if err != nil || len(h.Status) != 1 || h.Status[0].Text != "online" {
		t.Fatalf("status = %+v err=%v", h.Status, err)
	}

	// edit field
	next, _ = tab.enterEditField()
	tab = next.(*ledgerTab)
	cmd = tab.formSubmit(map[string]string{"name": "location", "value": "home"})
	if _, ok := cmd().(ledgerReloadMsg); !ok {
		t.Fatal("edit-field did not return reload")
	}
	h, err = tab.mgr.Ledger.GetHead("personal", "device.lnv")
	if err != nil || len(h.Fields) != 1 || h.Fields[0].Name != "location" {
		t.Fatalf("fields = %+v err=%v", h.Fields, err)
	}
}

func TestLedgerTabFilter(t *testing.T) {
	tab := newLedgerTestTab(t)
	seedLedgerHead(t, tab, "personal", "device", "lnv", "self-hosted node")
	seedLedgerHead(t, tab, "personal", "device", "mini", "mac mini")
	tab = loadLedgerTab(t, tab)

	out, _ := tab.Update(runeKey("/"))
	tab = out.(*ledgerTab)
	for _, k := range runeKeys("mini") {
		out, _ = tab.Update(k)
		tab = out.(*ledgerTab)
	}
	if n := len(tab.filteredHeads()); n != 1 {
		t.Fatalf("filtered heads = %d, want 1", n)
	}
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*ledgerTab)
	if n := len(tab.filteredHeads()); n != 2 {
		t.Fatalf("after esc heads = %d, want 2", n)
	}
}

func TestLedgerTabViewRespectsWidth(t *testing.T) {
	tab := newLedgerTestTab(t)
	seedLedgerHead(t, tab, "personal", "device", "lnv", "self-hosted node")
	tab = loadLedgerTab(t, tab)
	tab.SetSize(80, 20)
	view := tab.View()
	if view == "" {
		t.Fatal("empty view")
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("line exceeds width: %d > 80: %q", w, line)
		}
	}
	// Detail overlay renders head + records without panicking.
	tab.focusLeft = false
	out, cmd := tab.Update(runeKey("enter"))
	tab = out.(*ledgerTab)
	tab = flushTab(tab, cmd).(*ledgerTab)
	if tab.detail == nil {
		t.Fatal("enter did not open detail")
	}
	if got := tab.View(); got == "" {
		t.Fatal("empty detail view")
	}
}
