package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSyncSource 是可控的同步源：Status 返回固定快照，Push 记录调用次数。
type fakeSyncSource struct {
	state    SyncState
	pushes   int
	pushFail error
}

func (f *fakeSyncSource) Status() SyncState { return f.state }

func (f *fakeSyncSource) Push() SyncState {
	f.pushes++
	st := f.state
	if f.pushFail != nil {
		st.Err = f.pushFail
		return st
	}
	st.Dirty = 0
	st.Last = time.Date(2026, 9, 10, 14, 3, 0, 0, time.Local)
	f.state = st
	return st
}

func syncModel(t *testing.T, src SyncSource) Model {
	t.Helper()
	mgrs := newFullManagers(t)
	mgrs.Sync = src
	m := New(mgrs)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)
	// Feed the initial status refresh like bubbletea's event loop would.
	for _, msg := range runCmd(m.Init()) {
		out, _ := m.Update(msg)
		m = out.(Model)
	}
	return m
}

func TestSyncBadgeShowsPendingAndLastSync(t *testing.T) {
	src := &fakeSyncSource{state: SyncState{Dirty: 3, Last: time.Date(2026, 9, 10, 14, 3, 0, 0, time.Local)}}
	m := syncModel(t, src)
	if src.pushes != 0 {
		t.Fatalf("Init must not push, got %d pushes", src.pushes)
	}
	if !strings.Contains(m.View(), "3 条待推送") {
		t.Errorf("badge should show pending count; view=%q", m.View())
	}

	m.syncState = SyncState{Dirty: 0, Last: src.state.Last}
	if v := m.View(); !strings.Contains(v, "已同步 14:03") {
		t.Errorf("badge should show last sync time, got %q", v)
	}
}

func TestSyncBadgeHiddenWithoutSyncSource(t *testing.T) {
	m := syncModel(t, nil)
	if badge := m.syncBadge(); badge != "" {
		t.Fatalf("badge = %q, want empty in git mode", badge)
	}
	if strings.Contains(m.View(), "⟳") {
		t.Errorf("git mode view must not render sync status: %q", m.View())
	}
}

func TestWriteTriggersAsyncPushAndRefreshesBadge(t *testing.T) {
	src := &fakeSyncSource{state: SyncState{Dirty: 2}}
	m := syncModel(t, src)

	out, cmd := m.Update(envReloadMsg{})
	m = out.(Model)
	msgs := runCmd(cmd)
	if src.pushes != 1 {
		t.Fatalf("pushes = %d, want 1 after a write", src.pushes)
	}
	// Feed the resulting status messages back like bubbletea would.
	for _, msg := range msgs {
		out, _ := m.Update(msg)
		m = out.(Model)
	}
	if m.syncState.Dirty != 0 {
		t.Errorf("dirty = %d after successful push, want 0", m.syncState.Dirty)
	}
	if v := m.View(); !strings.Contains(v, "已同步") {
		t.Errorf("view = %q, want 已同步", v)
	}
}

func TestSyncPushFailureKeepsDirtyWithReason(t *testing.T) {
	src := &fakeSyncSource{state: SyncState{Dirty: 1}, pushFail: errors.New("server 不可达")}
	m := syncModel(t, src)
	out, cmd := m.Update(textReloadMsg{})
	m = out.(Model)
	for _, msg := range runCmd(cmd) {
		out, _ := m.Update(msg)
		m = out.(Model)
	}
	view := m.View()
	if !strings.Contains(view, "1 条待推送") || !strings.Contains(view, "同步失败") {
		t.Errorf("view = %q, want pending count + failure reason", view)
	}
	if !strings.Contains(view, "server 不可达") {
		t.Errorf("view = %q, want the short failure reason", view)
	}
}

func TestQuitWarnsOnceWhenDirty(t *testing.T) {
	src := &fakeSyncSource{state: SyncState{Dirty: 2}}
	m := syncModel(t, src)

	out, cmd := m.Update(runeKey("q"))
	m = out.(Model)
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("first q with pending changes must not quit")
		}
	}
	if !strings.Contains(m.View(), "2 条待推送") || !strings.Contains(m.View(), "再按一次 q") {
		t.Fatalf("view = %q, want quit warning", m.View())
	}

	out, cmd = m.Update(runeKey("q"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("second q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

func TestQuitImmediateWhenClean(t *testing.T) {
	m := syncModel(t, &fakeSyncSource{state: SyncState{Dirty: 0}})
	_, cmd := m.Update(runeKey("q"))
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}
