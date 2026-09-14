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

// 本文件锁定 ssh-tui-apply-export change 的 TUI 行为（openspec change
// ssh-tui-apply-export，specs/ssh-assets/spec.md ADDED Requirements）：
// `A` 应用导出的三个作用域、确认框预算与取消路径、异步执行摘要 toast、
// Render 失败零副作用，以及批量/单条导出表单对 senv 自有树的防护。

// newApplyScopeTab 造出真实 manager + 临时 HOME 的已装载 SSH Tab：
// prod 组 web（引用 prod-key）+ 未分组 api，与 internal/ssh 的 apply
// 夹具同构。HOME 必须在使用前 Setenv，避免触到开发机真实 ~/.ssh。
func newApplyScopeTab(t *testing.T) (*sshTab, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := t.TempDir()
	sm := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(sm, "pw")
	if _, err := mgr.ImportKeyPairWithGroup("prod-key", writeSSHTUIKey(t, "id_ed25519"), "prod", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod", IdentityKey: "prod-key"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "api", Hostname: "10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	tab := newSSHTab(Managers{SSH: mgr})
	tab.SetSize(100, 24)
	return flushTab(tab, tab.load()).(*sshTab), home
}

// pressA 在指定焦点/下标按 `A`，返回落定后的 tab。
func pressA(t *testing.T, tab *sshTab) *sshTab {
	t.Helper()
	out, cmd := tab.Update(runeKey("A"))
	if cmd != nil {
		t.Fatalf("A must stage the confirm synchronously (no command), got %v", runCmd(cmd))
	}
	return out.(*sshTab)
}

// confirmApply 在确认框按 `y`，返回执行命令（调用方 runCmd 断言 toast）。
func confirmApply(t *testing.T, tab *sshTab) (Tab, tea.Cmd) {
	t.Helper()
	return tab.Update(runeKey("y"))
}

// TestSSHApplyHostPaneStagesGroupConfirm：host 栏 `A` 进入确认模式，标题含
// 所在组名，预算列出组片段/待落盘数/Include 状态/warning 计数
// （Scenario: host 栏 apply 重建所在组）。
func TestSSHApplyHostPaneStagesGroupConfirm(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	// Host 栏按别名字典序：api(0) / web(1)；游标落到 web（prod 组）。
	tab.focus = paneHost
	tab.hostIndex = 1
	tab = pressA(t, tab)
	if tab.mode != sshModeApplyConfirm {
		t.Fatalf("A must stage the apply confirm, mode=%v", tab.mode)
	}
	view := tab.View()
	for _, want := range []string{
		"apply export",
		"group prod",
		"web",
		"rebuild group fragments: 1",
		"materialize missing keys: 1",
		"not registered (will be added)",
		"warnings: 0",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("apply confirm missing %q:\n%s", want, view)
		}
	}
}

// TestSSHApplySidebarScopesGroupAndAll：侧栏 `A` 选中组 = 单组标题、All =
// 全量标题与全组预算（Scenario: 侧栏按组 apply / 侧栏 All 全量 apply）。
func TestSSHApplySidebarScopesGroupAndAll(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	tab.focus = paneGroup
	// 侧栏行序：All(0) / prod(1) / 未分组(2)。
	tab.groupIndex = 1
	tab = pressA(t, tab)
	if tab.mode != sshModeApplyConfirm {
		t.Fatalf("sidebar A must stage the apply confirm, mode=%v", tab.mode)
	}
	if view := tab.View(); !strings.Contains(view, "apply export — group prod") {
		t.Fatalf("group scope title missing:\n%s", view)
	}

	tab.cancelMode()
	tab.groupIndex = 0 // All
	tab = pressA(t, tab)
	if tab.mode != sshModeApplyConfirm {
		t.Fatalf("All A must stage the apply confirm, mode=%v", tab.mode)
	}
	view := tab.View()
	if !strings.Contains(view, "all hosts") {
		t.Fatalf("All scope title missing:\n%s", view)
	}
	if !strings.Contains(view, "rebuild group fragments: 2") || !strings.Contains(view, "materialize missing keys: 1") {
		t.Fatalf("All scope budget wrong:\n%s", view)
	}
}

// TestSSHApplyUngroupedSidebarRowRefused：「未分组」伪组在 Apply 的
// RenderFilter 里与全量同义（空组过滤），直接映射会伪装成单组重建——
// 拒止并指引走 All（全量同样重建 _ungrouped 片段）。
func TestSSHApplyUngroupedSidebarRowRefused(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	tab.focus = paneGroup
	tab.groupIndex = 2 // 未分组
	out, cmd := tab.Update(runeKey("A"))
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal {
		t.Fatalf("ungrouped row must not stage apply, mode=%v", tab.mode)
	}
	var hint string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			hint = tm.text
		}
	}
	if !strings.Contains(hint, "All") {
		t.Fatalf("ungrouped refusal must point to All, got %q", hint)
	}
}

// TestSSHApplyCancelLeavesNoTrace：确认框 esc/n 取消，临时 HOME 下无任何
// groups/ 落盘（Scenario: 取消无副作用）。
func TestSSHApplyCancelLeavesNoTrace(t *testing.T) {
	for _, cancel := range []string{"esc", "n"} {
		tab, home := newApplyScopeTab(t)
		tab.focus = paneHost
		tab.hostIndex = 1
		tab = pressA(t, tab)
		if tab.mode != sshModeApplyConfirm {
			t.Fatalf("A must open the confirm, mode=%v", tab.mode)
		}
		var out Tab
		if cancel == "esc" {
			out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
		} else {
			out, _ = tab.Update(runeKey("n"))
		}
		tab = out.(*sshTab)
		if tab.mode != sshModeNormal || tab.pendingApply != nil {
			t.Fatalf("%s must cancel the apply confirm: mode=%v staged=%v", cancel, tab.mode, tab.pendingApply != nil)
		}
		if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups")); !os.IsNotExist(err) {
			t.Fatalf("cancel must not create group fragments: %v", err)
		}
	}
}

// TestSSHApplyConfirmExecutesAndToastsSummary：`y` 确认后异步 Apply 重建组
// 片段、落盘缺失私钥、注册 Include，toast 为 design D3 摘要格式
// （Scenario: host 栏 apply 重建所在组）。
func TestSSHApplyConfirmExecutesAndToastsSummary(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	tab.focus = paneHost
	tab.hostIndex = 1
	tab = pressA(t, tab)

	out, cmd := confirmApply(t, tab)
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal || tab.pendingApply != nil {
		t.Fatal("confirm must close the modal and clear the staged apply")
	}
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	for _, want := range []string{"applied: 1 groups", "1 keys materialized", "0 skipped", "include registered", "0 warnings"} {
		if !strings.Contains(toast, want) {
			t.Fatalf("apply toast missing %q: %q", want, toast)
		}
	}
	fragment, err := os.ReadFile(filepath.Join(home, ".ssh", "senv", "groups", "prod.conf"))
	if err != nil || !strings.Contains(string(fragment), "Host web") {
		t.Fatalf("prod fragment missing: %v\n%s", err, fragment)
	}
	info, err := os.Stat(filepath.Join(home, ".ssh", "senv", "keys", "prod", "prod-key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized key: %v mode %o", err, info.Mode().Perm())
	}
	cfg, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil || !strings.Contains(string(cfg), ssh.IncludeLine) {
		t.Fatalf("include line missing: %v\n%s", err, cfg)
	}
}

// TestSSHApplyGroupScopeLeavesOtherGroupsAlone：侧栏选中组 `A` 只重建该组
// 片段，未组片段不动（Scenario: 侧栏按组 apply）。
func TestSSHApplyGroupScopeLeavesOtherGroupsAlone(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	tab.focus = paneGroup
	tab.groupIndex = 1 // prod
	tab = pressA(t, tab)
	out, cmd := confirmApply(t, tab)
	tab = out.(*sshTab)
	runCmd(cmd)
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups", "prod.conf")); err != nil {
		t.Fatalf("prod fragment missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups", "_ungrouped.conf")); !os.IsNotExist(err) {
		t.Fatalf("group scope must not write other groups: %v", err)
	}
}

// TestSSHApplyAllPrunesGhostFragments：All 全量 apply 清理 vault 中已消失组
// 的幽灵片段（Scenario: 侧栏 All 全量 apply）。
func TestSSHApplyAllPrunesGhostFragments(t *testing.T) {
	tab, home := newApplyScopeTab(t)
	// 先直接全量一次，再把 web 挪出 prod：prod.conf 变成幽灵片段。
	if _, err := tab.mgr.SSH.Apply(ssh.RenderFilter{}); err != nil {
		t.Fatal(err)
	}
	if err := tab.mgr.SSH.UpdateHost("web", func(h *storage.HostEntry) error {
		h.Group = "staging"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tab = flushTab(tab, tab.load()).(*sshTab)

	tab.focus = paneGroup
	tab.groupIndex = 0 // All
	tab = pressA(t, tab)
	out, cmd := confirmApply(t, tab)
	tab = out.(*sshTab)
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if !strings.Contains(toast, "applied: 2 groups") {
		t.Fatalf("full apply toast wrong: %q", toast)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups", "prod.conf")); !os.IsNotExist(err) {
		t.Fatalf("ghost fragment must be pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv", "groups", "staging.conf")); err != nil {
		t.Fatalf("staging fragment missing: %v", err)
	}
}

// TestSSHApplyRenderFailureZeroSideEffects：proxyJump 悬空时 `A` 直接报错
// （errMsg，红色错误条载体），不进确认框、零文件副作用
// （design 错误处理策略）。
func TestSSHApplyRenderFailureZeroSideEffects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := t.TempDir()
	sm := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(sm, "pw")
	if err := sm.WithVaultMutation(func(locked *storage.Manager) error {
		return locked.SaveHost("web", &storage.HostEntry{Alias: "web", Hostname: "web", ProxyJump: "gone"}, "pw")
	}); err != nil {
		t.Fatal(err)
	}
	tab := newSSHTab(Managers{SSH: mgr})
	tab.SetSize(100, 24)
	tab = flushTab(tab, tab.load()).(*sshTab)
	tab.focus = paneHost
	tab.hostIndex = 0

	out, cmd := tab.Update(runeKey("A"))
	tab = out.(*sshTab)
	if tab.mode != sshModeNormal {
		t.Fatalf("render failure must not open the confirm, mode=%v", tab.mode)
	}
	var got string
	for _, m := range runCmd(cmd) {
		if em, ok := m.(errMsg); ok {
			got = em.err.Error()
		}
	}
	if !strings.Contains(got, "dangling proxyJump") {
		t.Fatalf("render failure must surface the render error, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "senv")); !os.IsNotExist(err) {
		t.Fatalf("render failure must have zero file side effects: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); !os.IsNotExist(err) {
		t.Fatalf("render failure must not touch ssh config: %v", err)
	}
}

// TestSSHBatchExportDirRejectsSenvTree：批量导出目录填进 ~/.ssh/senv 内部
// （含自身）被内联拒止并提示改用 `A`；用户自有目录通过
// （Scenario: 批量导出目录防护）。
func TestSSHBatchExportDirRejectsSenvTree(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	tab.focus = paneHost
	out, _ := tab.Update(runeKey("a")) // 全选可见集 → 批量导出表单
	tab = out.(*sshTab)
	if tab.sel.SelectionCount() != 2 {
		t.Fatalf("a must select all visible hosts: %d", tab.sel.SelectionCount())
	}
	out, _ = tab.Update(runeKey("x"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("x with a multi-selection must open the batch export form")
	}

	for _, bad := range []string{"~/.ssh/senv/groups", "~/.ssh/senv", "~/.ssh/senv/keys/prod"} {
		tab = submitSSHForm(t, tab, map[string]string{"dir": bad})
		if tab.form == nil {
			t.Fatalf("senv tree dir %q must keep the form open", bad)
		}
		if got := tab.form.errs[tab.form.fieldIndex("dir")]; !strings.Contains(got, "apply export") || !strings.Contains(got, "A") {
			t.Fatalf("inline guard for %q missing: %q", bad, got)
		}
	}

	dir := filepath.Join(t.TempDir(), "out")
	tab = submitSSHForm(t, tab, map[string]string{"dir": dir})
	if tab.form != nil {
		t.Fatalf("user dir must pass: %#v", tab.form.errs)
	}
	for _, alias := range []string{"api", "web"} {
		if _, err := os.Stat(filepath.Join(dir, alias+".conf")); err != nil {
			t.Fatalf("batch export missing %s.conf: %v", alias, err)
		}
	}
}

// TestSSHExportFileRejectsSenvTree：单条导出（预览后 `w`）目标文件填进
// ~/.ssh/senv 内任意位置被内联拒止并提示改用 `A`；用户自有路径通过
// （Scenario: 单条导出目标文件防护）。
func TestSSHExportFileRejectsSenvTree(t *testing.T) {
	tab, _ := newApplyScopeTab(t)
	tab.focus = paneHost
	tab.hostIndex = 1 // web
	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*sshTab)
	if tab.mode != sshModeExportPreview {
		t.Fatalf("x must open the export preview, mode=%v", tab.mode)
	}
	out, _ = tab.Update(runeKey("w"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("w must open the export path form")
	}

	tab = submitSSHForm(t, tab, map[string]string{"path": "~/.ssh/senv/x.conf"})
	if tab.form == nil {
		t.Fatal("senv tree target must keep the form open")
	}
	if got := tab.form.errs[tab.form.fieldIndex("path")]; !strings.Contains(got, "apply export") || !strings.Contains(got, "A") {
		t.Fatalf("inline guard error missing: %q", got)
	}

	target := filepath.Join(t.TempDir(), "web.conf")
	tab = submitSSHForm(t, tab, map[string]string{"path": target})
	if tab.form != nil {
		t.Fatalf("user path must pass: %#v", tab.form.errs)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "Host web") {
		t.Fatalf("exported file wrong: %v\n%s", err, data)
	}
}
