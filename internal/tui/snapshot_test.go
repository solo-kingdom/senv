package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSnapshotRegistrySharedAndInvalidated(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if m.mgr.snap == nil {
		t.Fatal("New should install a snapshot registry")
	}
	snap1, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap1.Vars["default"]["A"] != "1" {
		t.Fatalf("A = %q, want 1", snap1.Vars["default"]["A"])
	}

	if err := mgrs.Env.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	snap2, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap2.Vars["default"]["B"]; ok {
		t.Fatal("write must not appear in the cached snapshot before invalidate")
	}

	m.mgr.snap.Invalidate()
	snap3, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap3.Vars["default"]["B"] != "2" {
		t.Fatalf("B = %q after invalidate, want 2", snap3.Vars["default"]["B"])
	}
}

func TestWriteDoneInvalidatesSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.Get(); err != nil {
		t.Fatal(err)
	}
	if err := mgrs.Env.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(envReloadMsg{})
	m = out.(Model)
	snap, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Vars["default"]["B"] != "2" {
		t.Fatalf("B = %q after write-done invalidate, want 2", snap.Vars["default"]["B"])
	}
}

func TestSearchReadsSharedEnvSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "API_KEY", "secret"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.Get(); err != nil {
		t.Fatal(err)
	}
	s := newSearchTab(m.mgr)
	all := func() []searchResult {
		msg := s.gather()()
		gm, ok := msg.(searchGatheredMsg)
		if !ok {
			t.Fatalf("gather returned %T", msg)
		}
		return gm.all
	}()
	found := false
	for _, r := range all {
		if r.resultType == typeEnv && r.key == "API_KEY" {
			found = true
			if r.preview == "secret" {
				t.Fatal("search preview leaked env value")
			}
		}
	}
	if !found {
		t.Fatal("expected API_KEY from shared snapshot")
	}
}

// TestTextSnapshotRegistrySharedAndInvalidated 验证 text 快照与 env 同机制：
// 缓存命中（写后不自动更新）、Invalidate 后重建。
func TestTextSnapshotRegistrySharedAndInvalidated(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Text.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	snap1, err := m.mgr.snap.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap1.Items["default"]) != 1 || snap1.Groups[0].KeyCount != 1 {
		t.Fatalf("snap1 = %+v, want one default entry", snap1)
	}

	if err := mgrs.Text.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	snap2, err := m.mgr.snap.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Items["default"]) != 1 {
		t.Fatal("write must not appear in the cached text snapshot before invalidate")
	}

	m.mgr.snap.Invalidate()
	snap3, err := m.mgr.snap.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap3.Items["default"]) != 2 {
		t.Fatalf("snap3 items = %d, want 2 after invalidate", len(snap3.Items["default"]))
	}
}

// TestRenameDoneInvalidatesSnapshot 守住 writeDoneMsg 的回归：rename/import
// 完成消息（renameDoneMsg）也是写操作，必须作废 env/text 快照 memo，
// 否则 tab 随后的 reload 会命中改名前的缓存。
func TestRenameDoneInvalidatesSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Text.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.GetText(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.mgr.snap.Get(); err != nil {
		t.Fatal(err)
	}
	if err := mgrs.Text.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(renameDoneMsg{group: "default", key: "B", text: "renamed"})
	m = out.(Model)
	snap, err := m.mgr.snap.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Items["default"]) != 2 {
		t.Fatalf("text snapshot items = %d after renameDoneMsg, want 2 (memo must be invalidated)", len(snap.Items["default"]))
	}
}

// TestCtrlRInvalidatesSnapshot 验证显式刷新（ctrl+r）先作废进程内 memo：
// 用户刷新必须重新读 vault，而不是命中写后/pull 前的缓存。
func TestCtrlRInvalidatesSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Text.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.GetText(); err != nil {
		t.Fatal(err)
	}
	if err := mgrs.Text.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = out.(Model)
	snap, err := m.mgr.snap.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Items["default"]) != 2 {
		t.Fatalf("text snapshot items = %d after ctrl+r, want 2 (memo must be invalidated)", len(snap.Items["default"]))
	}
}
