package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wii/senv/internal/ssh"
)

// 本文件锁定 tui-missing-mgmt-entries 的 SSH Tab `u` unexport：
// 预检确认框、执行撤回、无可撤回项 toast、esc 取消零副作用。

func applyThenLoad(t *testing.T, tab *sshTab) *sshTab {
	t.Helper()
	if _, err := tab.mgr.SSH.Apply(ssh.RenderFilter{}); err != nil {
		t.Fatal(err)
	}
	return flushTab(tab, tab.load()).(*sshTab)
}

func pressU(t *testing.T, tab *sshTab) *sshTab {
	t.Helper()
	out, cmd := tab.Update(runeKey("u"))
	return flushTab(out, cmd).(*sshTab)
}

func TestSSHUnexportConfirmListsFragmentsAndKeepsKeys(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	tab = applyThenLoad(t, tab)
	tab.focus = paneHost
	tab = pressU(t, tab)
	if tab.mode != sshModeUnexport {
		t.Fatalf("u must stage the unexport confirm, mode=%v", tab.mode)
	}
	view := tab.View()
	for _, want := range []string{
		"unexport senv ssh export?",
		"remove senv Include line",
		"delete 2 group fragment(s)",
		"keys/",
		"are kept",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("unexport confirm missing %q:\n%s", want, view)
		}
	}

	out, cmd := tab.Update(runeKey("y"))
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal || tab.pendingUnexport != nil {
		t.Fatal("confirm must close the modal")
	}
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if !strings.Contains(toast, "include removed") || !strings.Contains(toast, "group fragments deleted") {
		t.Fatalf("unexport toast wrong: %q", toast)
	}

	cfg, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil || strings.Contains(string(cfg), ssh.IncludeLine) {
		t.Fatalf("include line must be removed: %v\n%s", err, cfg)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups")); !os.IsNotExist(err) {
		t.Fatalf("groups/ must be removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "keys", "prod", "prod-key")); err != nil {
		t.Fatalf("materialized key must be kept: %v", err)
	}
	if _, err := tab.mgr.SSH.GetHost("web"); err != nil {
		t.Fatalf("vault host must be kept: %v", err)
	}
}

func TestSSHUnexportAvailableFromSidebar(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	tab = applyThenLoad(t, tab)
	tab.focus = paneGroup
	tab = pressU(t, tab)
	if tab.mode != sshModeUnexport {
		t.Fatalf("sidebar u must stage unexport, mode=%v", tab.mode)
	}
}

func TestSSHUnexportNothingToDoToasts(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	out, cmd := tab.Update(runeKey("u"))
	tab = out.(*sshTab)
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("precheck cmd msgs = %v", msgs)
	}
	out, cmd = tab.Update(msgs[0])
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal {
		t.Fatalf("nothing to unexport must not open confirm, mode=%v", tab.mode)
	}
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if toast != "nothing to unexport" {
		t.Fatalf("toast = %q", toast)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); !os.IsNotExist(err) {
		t.Fatalf("nothing-to-do must not create ssh config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv")); !os.IsNotExist(err) {
		t.Fatalf("nothing-to-do must not create senv tree: %v", err)
	}
}

func TestSSHUnexportCancelLeavesExportIntact(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	tab = applyThenLoad(t, tab)
	tab = pressU(t, tab)
	if tab.mode != sshModeUnexport {
		t.Fatalf("u must open confirm, mode=%v", tab.mode)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal || tab.pendingUnexport != nil {
		t.Fatalf("esc must cancel: mode=%v staged=%v", tab.mode, tab.pendingUnexport != nil)
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
	cfg, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil || !strings.Contains(string(cfg), ssh.IncludeLine) {
		t.Fatalf("cancel must keep include: %v\n%s", err, cfg)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups", "prod.conf")); err != nil {
		t.Fatalf("cancel must keep fragments: %v", err)
	}
}
