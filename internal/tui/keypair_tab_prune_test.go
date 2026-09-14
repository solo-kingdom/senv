package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// 本文件锁定 tui-missing-mgmt-entries 的 KeyPair Tab `p` prune：
// 候选列表标注、确认删除、esc 零删除、部分失败不回滚。

func newPruneTab(t *testing.T) (*keyPairTab, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := t.TempDir()
	sm := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(sm, "pw")
	keyPath := writeSSHTUIKey(t, "id_ed25519")
	if _, err := mgr.ImportKeyPairWithGroup("prod-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ImportKeyPairWithGroup("orphan-vault", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod", IdentityKey: "prod-key"}); err != nil {
		t.Fatal(err)
	}

	keep, err := ssh.MaterializePath("prod", "prod-key")
	if err != nil {
		t.Fatal(err)
	}
	orphaned, err := ssh.MaterializePath("prod", "orphan-vault")
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := ssh.MaterializePath("other", "ghost")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{keep, orphaned, ghost} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tab := newKeyPairTab(Managers{SSH: mgr})
	tab.SetSize(120, 24)
	return flushTab(tab, tab.load()).(*keyPairTab), home
}

func pressP(t *testing.T, tab *keyPairTab) *keyPairTab {
	t.Helper()
	out, cmd := tab.Update(runeKey("p"))
	return flushTab(out, cmd).(*keyPairTab)
}

func TestKeyPairPruneListsUnreferencedAndDeletesOnConfirm(t *testing.T) {
	tab, _ := newPruneTab(t)
	tab.focus = kpPaneList
	tab = pressP(t, tab)
	if tab.mode != kpModePrune {
		t.Fatalf("p must stage prune, mode=%v", tab.mode)
	}
	if len(tab.pendingPrune) != 2 {
		t.Fatalf("candidates = %d, want 2", len(tab.pendingPrune))
	}
	view := tab.View()
	orphaned, _ := ssh.MaterializePath("prod", "orphan-vault")
	ghost, _ := ssh.MaterializePath("other", "ghost")
	keep, _ := ssh.MaterializePath("prod", "prod-key")
	var sawVault, sawGhost bool
	for _, c := range tab.pendingPrune {
		if c.Path == orphaned && c.InVault {
			sawVault = true
		}
		if c.Path == ghost && !c.InVault {
			sawGhost = true
		}
		if c.Path == keep {
			t.Fatalf("referenced key must not be listed: %+v", c)
		}
	}
	if !sawVault || !sawGhost {
		t.Fatalf("candidates missing InVault/ghost: %+v", tab.pendingPrune)
	}
	if !strings.Contains(view, orphaned) || !strings.Contains(view, "(keypair") || !strings.Contains(view, "still in vault)") {
		t.Fatalf("InVault candidate missing annotation:\n%s", view)
	}
	if !strings.Contains(view, ghost) {
		t.Fatalf("ghost path missing:\n%s", view)
	}
	if strings.Contains(view, keep) {
		t.Fatalf("referenced key must not be listed:\n%s", view)
	}
	if !strings.Contains(view, "delete 2 file(s)") {
		t.Fatalf("footer count missing:\n%s", view)
	}

	out, cmd := tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*keyPairTab)
	if tab.mode != kpModeNormal {
		t.Fatalf("confirm must return to normal, mode=%v", tab.mode)
	}
	if _, err := os.Stat(orphaned); !os.IsNotExist(err) {
		t.Fatalf("orphan-vault must be deleted: %v", err)
	}
	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Fatalf("ghost must be deleted: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("referenced key must survive: %v", err)
	}
	if _, err := tab.mgr.SSH.GetKeyPairSummary("orphan-vault"); err != nil {
		t.Fatalf("vault keypair must be kept: %v", err)
	}
}

func TestKeyPairPruneEmptyToasts(t *testing.T) {
	tab, _ := newPruneTab(t)
	// 只留被引用的落盘文件。
	orphaned, _ := ssh.MaterializePath("prod", "orphan-vault")
	ghost, _ := ssh.MaterializePath("other", "ghost")
	for _, p := range []string{orphaned, ghost} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	out, cmd := tab.Update(runeKey("p"))
	tab = out.(*keyPairTab)
	msgs := runCmd(cmd)
	out, cmd = tab.Update(msgs[0])
	tab = out.(*keyPairTab)
	if tab.mode != kpModeNormal {
		t.Fatalf("empty prune must not open modal, mode=%v", tab.mode)
	}
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if toast != "no unreferenced materialized keys" {
		t.Fatalf("toast = %q", toast)
	}
	keep, _ := ssh.MaterializePath("prod", "prod-key")
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("referenced key must survive empty prune: %v", err)
	}
}

func TestKeyPairPruneCancelDeletesNothing(t *testing.T) {
	tab, _ := newPruneTab(t)
	tab = pressP(t, tab)
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*keyPairTab)
	if tab.mode != kpModeNormal || tab.pendingPrune != nil {
		t.Fatalf("esc must cancel: mode=%v staged=%v", tab.mode, tab.pendingPrune != nil)
	}
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if toast != "cancelled" {
		t.Fatalf("cancel toast = %q", toast)
	}
	orphaned, _ := ssh.MaterializePath("prod", "orphan-vault")
	ghost, _ := ssh.MaterializePath("other", "ghost")
	for _, p := range []string{orphaned, ghost} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("cancel must keep %s: %v", p, err)
		}
	}
}

func TestKeyPairPrunePartialFailureWarnsAndKeepsDeleted(t *testing.T) {
	tab, _ := newPruneTab(t)
	ghost, _ := ssh.MaterializePath("other", "ghost")
	blockedDir := filepath.Dir(ghost)
	if err := os.Chmod(blockedDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockedDir, 0o700) })

	tab = pressP(t, tab)
	out, cmd := tab.Update(runeKey("y"))
	tab = out.(*keyPairTab)
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("prune cmd msgs = %v", msgs)
	}
	out, follow := tab.Update(msgs[0])
	tab = out.(*keyPairTab)
	var warn string
	for _, m := range runCmd(follow) {
		if wm, ok := m.(warnMsg); ok {
			warn = wm.text
		}
	}
	if !strings.Contains(warn, "deleted 1 of 2") {
		t.Fatalf("partial failure warn missing, warn=%q", warn)
	}
	orphaned, _ := ssh.MaterializePath("prod", "orphan-vault")
	if _, err := os.Stat(orphaned); !os.IsNotExist(err) {
		t.Fatalf("successful delete must not roll back: %v", err)
	}
	if _, err := os.Stat(ghost); err != nil {
		t.Fatalf("blocked file must remain: %v", err)
	}
}

func TestKeyPairPruneAvailableFromSidebar(t *testing.T) {
	tab, _ := newPruneTab(t)
	tab.focus = kpPaneGroup
	tab = pressP(t, tab)
	if tab.mode != kpModePrune {
		t.Fatalf("sidebar p must stage prune, mode=%v", tab.mode)
	}
}
