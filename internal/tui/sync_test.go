package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSyncSource 是可控的同步源：Status 返回固定快照，Push/Pull 记录调用，
// Pull 的返回由 pullOut 注入。
type fakeSyncSource struct {
	state       SyncState
	pushes      int
	pushFail    error
	pulls       int
	pullRefresh bool
	pullOut     PullOutcome
}

func (f *fakeSyncSource) Status() SyncState { return f.state }

func (f *fakeSyncSource) Pull(refresh bool) PullOutcome {
	f.pulls++
	f.pullRefresh = refresh
	return f.pullOut
}

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

func TestPullSyncNilSourceIsNoop(t *testing.T) {
	if cmd := pullSync(nil, true); cmd != nil {
		t.Fatal("pullSync must return nil without a sync source (git mode)")
	}
}

func TestInitRunsBackgroundPullWithRefreshFlag(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		src := &fakeSyncSource{}
		mgrs := newFullManagers(t)
		mgrs.Sync = src
		mgrs.Refresh = refresh
		m := New(mgrs)
		runCmd(m.Init()) // the zero pull outcome needs no feedback
		if src.pulls != 1 {
			t.Fatalf("refresh=%v: pulls = %d, want exactly one background pull at startup", refresh, src.pulls)
		}
		if src.pullRefresh != refresh {
			t.Fatalf("refresh=%v: Pull saw refresh=%v", refresh, src.pullRefresh)
		}
	}
}

// tabLoadedFlag reports the loaded flag of tabs that keep one; the second
// return value is false for tabs without a loaded short-circuit (audit).
func tabLoadedFlag(tab Tab) (loaded, hasFlag bool) {
	switch tb := tab.(type) {
	case *envTab:
		return tb.loaded, true
	case *textTab:
		return tb.loaded, true
	case *configTab:
		return tb.loaded, true
	case *sshTab:
		return tb.loaded, true
	case *aiTab:
		return tb.loaded, true
	case *historyTab:
		return tb.loaded, true
	default:
		return false, false
	}
}

// focusAllTabs walks through every tab and back to the first, feeding each
// lazy load like a user pressing 1..N — so every tab is loaded before a test
// asserts on reload behavior.
func focusAllTabs(m Model) Model {
	for i := range m.tabs {
		out, cmd := m.Update(runeKey(string(rune('1' + i))))
		m = out.(Model)
		for _, msg := range runCmd(cmd) {
			out, _ = m.Update(msg)
			m = out.(Model)
		}
	}
	// Land back on the first tab so the active tab has a loaded flag.
	out, cmd := m.Update(runeKey("1"))
	m = out.(Model)
	for _, msg := range runCmd(cmd) {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	return m
}

func TestSyncPullWithoutChangesKeepsTabsLoaded(t *testing.T) {
	src := &fakeSyncSource{}
	m := focusAllTabs(syncModel(t, src))
	for i, tab := range m.tabs {
		if loaded, hasFlag := tabLoadedFlag(tab); hasFlag && !loaded {
			t.Fatalf("precondition: tabs[%d] not loaded after focusing", i)
		}
	}
	out, cmd := m.Update(syncPullMsg{out: PullOutcome{}})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("expected a sync status refresh command even without changes")
	}
	for _, msg := range runCmd(cmd) {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	if m.toast != "" {
		t.Errorf("toast = %q, want none when nothing was applied", m.toast)
	}
	for i, tab := range m.tabs {
		if loaded, hasFlag := tabLoadedFlag(tab); hasFlag && !loaded {
			t.Errorf("tabs[%d] reloaded although the pull applied nothing", i)
		}
	}
}

func TestSyncPullAppliedReloadsTabsWithToast(t *testing.T) {
	src := &fakeSyncSource{}
	m := focusAllTabs(syncModel(t, src))
	out, cmd := m.Update(syncPullMsg{out: PullOutcome{Applied: 2, MetadataUpdated: true}})
	m = out.(Model)
	// stale-while-revalidate：应用变更后每个已加载 Tab 保持旧数据可见
	// （loaded 不清空），由 Reload 返回的装载命令后台静默替换。
	for i, tab := range m.tabs {
		// History 是网络型 Tab：Reload 仍失效缓存待重查（无 SWR 语义）
		if tab.Title() == "History" {
			continue
		}
		if loaded, hasFlag := tabLoadedFlag(tab); hasFlag && !loaded {
			t.Errorf("tabs[%d] dropped its loaded flag after an applied pull", i)
		}
	}
	if cmd == nil {
		t.Fatal("expected toast + reload + status refresh commands")
	}
	sawToast := false
	for _, msg := range runCmd(cmd) {
		if toast, ok := msg.(toastMsg); ok && toast.text == "已从 server 更新 2 条" {
			sawToast = true
		}
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	if !sawToast {
		t.Fatal("expected the applied-changes toast message")
	}
	if !strings.Contains(m.View(), "已从 server 更新 2 条") {
		t.Errorf("view = %q, want the applied-changes toast", m.View())
	}
	if loaded, hasFlag := tabLoadedFlag(m.tabs[m.active]); hasFlag && !loaded {
		t.Error("active tab did not finish reloading")
	}
	// 切走再切回：SWR 下 Tab 仍是已加载状态，激活时不应重复触发全量装载
	if _, visit := m.Update(runeKey("2")); visit != nil {
		t.Error("visiting a loaded tab must not re-issue a load command after an applied pull")
	}
}

func TestSyncPullErrorShowsBannerWithoutToast(t *testing.T) {
	src := &fakeSyncSource{}
	m := focusAllTabs(syncModel(t, src))
	out, _ := m.Update(syncPullMsg{out: PullOutcome{Err: errors.New("server 不可达")}})
	m = out.(Model)
	if !strings.Contains(m.View(), "server 不可达") {
		t.Errorf("view = %q, want the pull error in the banner", m.View())
	}
	if m.toast != "" {
		t.Errorf("toast = %q, want none on pull error", m.toast)
	}
	for i, tab := range m.tabs {
		if loaded, hasFlag := tabLoadedFlag(tab); hasFlag && !loaded {
			t.Errorf("tabs[%d] reloaded although the pull failed", i)
		}
	}
}
