package tui

// tui-review-fixes-core 回归用例：审查 P1 逐条锁定。构造器与断言复用各 Tab
// 既有测试基建（newMCPTestTab/newTestTextManager/fakeAuditWriter 等）。

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// ---------- 1. All 伪组语义（env / text） ----------

// envAllViewTab 构造 All 伪组 + dev/prod 双组的已装载 env Tab。
func envAllViewTab(t *testing.T) *envTab {
	t.Helper()
	mgr := Managers{Env: newTestEnvManager(t), Text: newTestTextManager(t)}
	tab := newEnvTab(mgr)
	tab.loaded = true
	tab.groups = []envGroupRow{
		{name: envAllLabel, isAll: true},
		{name: "dev", isDefault: true},
		{name: "prod"},
	}
	tab.itemsByGroup = map[string][]envItemRow{
		"dev":  {{group: "dev", key: "A", value: "1"}, {group: "dev", key: "B", value: "2"}},
		"prod": {{group: "prod", key: "A", value: "3"}},
	}
	tab.focusLeft = false
	tab.SetSize(80, 20)
	tab.clampCursors()
	return tab
}

// TestEnvAllViewSelectAllUsesRealGroups：All 视图 `a` 的选择标识必须是真实
// 分组（group/key），否则全选静默失效、批量目标错位（审查 env:390）。
func TestEnvAllViewSelectAllUsesRealGroups(t *testing.T) {
	tab := envAllViewTab(t)
	tab = envUpdate(tab, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got := tab.sel.SelectionCount(); got != 3 {
		t.Fatalf("selection count = %d, want 3 (dev/A, dev/B, prod/A)", got)
	}
	if !tab.sel.IsSelected("prod/A") {
		t.Fatal("prod/A not selected: selection keys are not real groups")
	}
}

// TestEnvAllViewRenderPrefix：All 视图聚合行必须显示 group/key 前缀
// （审查 env:1144）。
func TestEnvAllViewRenderPrefix(t *testing.T) {
	tab := envAllViewTab(t)
	view := tab.View()
	if !strings.Contains(view, "prod/A") {
		t.Fatalf("All view missing group/key prefix: %q", clipRunesT(view, 160))
	}
}

// TestEnvDerefAllViewAggregatesGroups：All 视图解引用聚合全部真实分组且按
// group/key 键结果，跨组同名 key 不串值（审查 env:220 + deref 键碰撞）。
func TestEnvDerefAllViewAggregatesGroups(t *testing.T) {
	tab := envAllViewTab(t)
	tab.deref = true
	msg := drainCmd(t, tab.resolveDeref())
	dm, ok := msg.(envDerefMsg)
	if !ok {
		t.Fatalf("unexpected msg %#v", msg)
	}
	if len(dm.results) != 3 {
		t.Fatalf("deref results = %d entries, want 3 (per group/key)", len(dm.results))
	}
	if _, ok := dm.results["prod/A"]; !ok {
		t.Fatalf("results keyed by group/key missing prod/A: %#v", dm.results)
	}
}

// TestEnvSpaceGhostSelectionGuard：游标越界时空格不得写入空选择标识
// （审查 env:385）。
func TestEnvSpaceGhostSelectionGuard(t *testing.T) {
	tab := envAllViewTab(t)
	tab.itemIndex = 99
	tab = envUpdate(tab, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if tab.sel.SelectionCount() != 0 {
		t.Fatalf("ghost selection created: %d", tab.sel.SelectionCount())
	}
}

// TestEnvActivateGuardOnInvalidGroup：分组行无效时 `t` 不得触发激活
// （审查 env:426）。
func TestEnvActivateGuardOnInvalidGroup(t *testing.T) {
	tab := envAllViewTab(t)
	tab.focusLeft = true
	tab.groupIndex = 99
	_, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd != nil {
		t.Fatal("invalid group row must not trigger activation")
	}
}

// TestTextAllViewSelectAllUsesRealGroups：text All 视图 `a` 用真实分组标识
// （审查 text:345/1082）。
func TestTextAllViewSelectAllUsesRealGroups(t *testing.T) {
	mgr := newTestTextManager(t)
	if err := mgr.Set("default", "a1", "v"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddGroup("other", "test"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("other", "b1", "v"); err != nil {
		t.Fatal(err)
	}
	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())
	if tab.groups[0].name != textAllLabel {
		t.Fatalf("expected All view first, got %q", tab.groups[0].name)
	}
	tab.focusLeft = false
	tab = flushText(tab, func() tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}} })
	if tab.sel.SelectionCount() != 2 {
		t.Fatalf("selection count = %d, want 2", tab.sel.SelectionCount())
	}
	if !tab.sel.IsSelected("other/b1") {
		t.Fatal("other/b1 not selected: keys are not real groups")
	}
}

// TestTextBatchDeleteAuditsAndReloads：批量删除逐条审计并以 reload 收尾
// （审查 text:636/646）。
func TestTextBatchDeleteAuditsAndReloads(t *testing.T) {
	mgr := newTestTextManager(t)
	w := &fakeAuditWriter{}
	for _, k := range []string{"a1", "a2"} {
		if err := mgr.Set("default", k, "v"); err != nil {
			t.Fatal(err)
		}
	}
	tab := newTextTab(Managers{Text: mgr, AuditWriter: w})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())

	msg := drainCmd(t, tab.doBatchDelete([][2]string{{"default", "a1"}, {"default", "a2"}}))
	rl, ok := msg.(textReloadMsg)
	if !ok {
		t.Fatalf("batch delete should end with reload msg, got %#v", msg)
	}
	if rl.toast == "" {
		t.Fatal("success toast missing on batch delete reload")
	}
	if len(w.calls) != 2 {
		t.Fatalf("audit calls = %d, want 2 (one per entry)", len(w.calls))
	}
	for _, c := range w.calls {
		if c.event != session.AuditOpText {
			t.Fatalf("unexpected audit event %#v", c)
		}
		if !strings.Contains(c.target, "text:default:") {
			t.Fatalf("audit target not group/key form: %q", c.target)
		}
	}
}

// TestTextBatchDeleteSingleSelectionTargetsSelection：选择集非空（含 1 条）
// 时 `d` 走批量确认而非游标单条（tui-viewer 多选语义）。
func TestTextBatchDeleteSingleSelectionTargetsSelection(t *testing.T) {
	mgr := newTestTextManager(t)
	for _, k := range []string{"a1", "a2"} {
		if err := mgr.Set("default", k, "v"); err != nil {
			t.Fatal(err)
		}
	}
	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())
	tab.focusLeft = false
	tab.sel.Toggle("default/a2") // 选中 a2，游标停在 a1
	tab = flushText(tab, func() tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}} })
	if tab.mode != textModeBatchDeleteConfirm {
		t.Fatalf("mode = %v, want batch delete confirm", tab.mode)
	}
}

// TestTextEditWarnsWhenSelectionDiverges：选中 1 条但游标在别处时 `e` 提示
// 而非编辑游标项（审查 mcp:395 同类，text 侧锁定）。
func TestTextEditWarnsWhenSelectionDiverges(t *testing.T) {
	mgr := newTestTextManager(t)
	for _, k := range []string{"a1", "a2"} {
		if err := mgr.Set("default", k, "v"); err != nil {
			t.Fatal(err)
		}
	}
	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())
	tab.focusLeft = false
	tab.sel.Toggle("default/a2")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("divergence should produce a warning toast cmd")
	}
	msg := drainCmd(t, cmd)
	if wt, ok := msg.(toastMsg); !ok || !strings.Contains(wt.text, "differs from cursor") {
		t.Fatalf("expected divergence warning, got %#v", msg)
	}
	if out.(*textTab).mode != textModeNormal {
		t.Fatal("edit must not open on divergence")
	}
}

// TestTextBatchExportValidatesSegment：目标 key 未通过路径段校验时计入失败
// 而非写出非法路径（审查 text:653）。
func TestTextBatchExportValidatesSegment(t *testing.T) {
	mgr := newTestTextManager(t)
	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	dir := t.TempDir()
	msg := drainCmd(t, tab.doBatchExport(dir, [][2]string{{"default", "bad/key"}}))
	if _, ok := msg.(warnMsg); !ok {
		t.Fatalf("invalid segment should surface warnMsg, got %#v", msg)
	}
}

// ---------- 2. 过滤感知游标（mcp / ssh / ai） ----------

// TestMCPClampUsesVisibleList：过滤态下 clamp 不得按未过滤列表收口
// （审查 mcp:562）。
func TestMCPClampUsesVisibleList(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "alpha", "run-alpha", nil)
	addMCPProfile(t, tab, "beta", "run-beta", nil)
	tab = loadMCPTab(t, tab)
	tab.filterBox.EnterFresh()
	tab.filterBox.Append("beta")
	tab.serverIndex = 5
	tab.clamp()
	if tab.serverIndex != 0 {
		t.Fatalf("serverIndex = %d, want 0 (visible list has 1 item)", tab.serverIndex)
	}
}

// TestMCPSelectionDivergenceWarns：选中 1 条但游标在别处时 e/d 提示而非
// 静默操作游标项（审查 mcp:395/422）。
func TestMCPSelectionDivergenceWarns(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "alpha", "run-alpha", nil)
	addMCPProfile(t, tab, "beta", "run-beta", nil)
	tab = loadMCPTab(t, tab)
	tab.sel.Toggle("beta") // 选中 beta，游标停在 alpha（index 0）
	for _, key := range []string{"e", "d"} {
		out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		mt := out.(*mcpTab)
		if mt.mode != mcpModeNormal {
			t.Fatalf("key %q opened a dialog on divergence (mode=%v)", key, mt.mode)
		}
		if cmd == nil {
			t.Fatalf("key %q should produce a warning toast cmd", key)
		}
		msg := drainCmd(t, cmd)
		if wt, ok := msg.(toastMsg); !ok || !strings.Contains(wt.text, "differs from cursor") {
			t.Fatalf("key %q: expected divergence warning, got %#v", key, msg)
		}
	}
}

// TestMCPReconcileSelectionDropsDeleted：reload 后选择集丢弃已删除档案
// （审查 mcp:340）。
func TestMCPReconcileSelectionDropsDeleted(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "alpha", "run-alpha", nil)
	addMCPProfile(t, tab, "beta", "run-beta", nil)
	tab = loadMCPTab(t, tab)
	tab.sel.Toggle("alpha")
	tab.sel.Toggle("beta")
	// 模拟 beta 被删除后的重载结果
	tab.servers = tab.servers[:1]
	tab.reconcileSelection()
	if tab.sel.SelectionCount() != 1 {
		t.Fatalf("selection count = %d, want 1 after reconcile", tab.sel.SelectionCount())
	}
	if tab.sel.IsSelected("beta") {
		t.Fatal("deleted alias still selected")
	}
}

// TestSSHPendingJumpClearsFilter：jump 按契约先清过滤再在可见列表定位
// （审查 ssh:1264/1271）。
func TestSSHPendingJumpClearsFilter(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "cfg"), filepath.Join(base, "data"))
	if err := store.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	mgr := ssh.NewManager(store, "pw")
	if err := mgr.AddHost(&storage.HostEntry{Alias: "alpha", Hostname: "a.example"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "beta", Hostname: "b.example"}); err != nil {
		t.Fatal(err)
	}
	tab := newSSHTab(Managers{SSH: mgr})
	tab.SetSize(80, 20)
	tab = flushTab(tab, tab.load()).(*sshTab)
	tab.filterBox.EnterFresh()
	tab.filterBox.Append("beta") // 过滤后 alpha 不可见
	tab.pendingJump = "alpha"
	tab.applyPendingJump()
	if tab.filterBox.Term() != "" {
		t.Fatal("filter should be cleared before jump (tui-ux-filter contract)")
	}
	if tab.hostIndex != 0 || tab.focus != paneHost {
		t.Fatalf("jump landed wrong: hostIndex=%d focus=%v", tab.hostIndex, tab.focus)
	}
}

// TestAIFilterInputSwallowsKeys：过滤输入态吞掉导航/实体键（审查 ai:353）。
func TestAIFilterInputSwallowsKeys(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	out, _ := tab.Update(runeKey("/"))
	tab = out.(*aiTab)
	if !tab.filterBox.Active() {
		t.Fatal("filter not active after /")
	}
	before := tab.providerIndex
	for _, key := range []string{"j", "k", "d", "n"} {
		out, _ = tab.Update(runeKey(key))
		tab = out.(*aiTab)
		if tab.mode != aiModeNormal || tab.form != nil {
			t.Fatalf("key %q escaped filter input and opened a dialog", key)
		}
	}
	if !tab.filterBox.Active() {
		t.Fatal("filter should stay active while typing")
	}
	if tab.filterBox.Term() != "jkdn" {
		t.Fatalf("filter term = %q, want jkdn", tab.filterBox.Term())
	}
	if tab.providerIndex != before {
		t.Fatalf("providerIndex moved during filter input: %d -> %d", before, tab.providerIndex)
	}
}

// TestAICurrentProviderFollowsVisibleList：过滤后游标语义 = 可见列表位置，
// 档案解析必须落在用户停留的可见条目上（审查 ai:586）。
func TestAICurrentProviderFollowsVisibleList(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	if _, err := tab.mgr.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "second", BaseURL: "https://second.example.com",
		APIKey: "sk-second", Models: []string{"s1"}, DefaultModel: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	tab = flushTab(tab, tab.load()).(*aiTab)
	tab.filterBox.EnterFresh()
	tab.filterBox.Append("second")
	tab.providerIndex = 0
	p := tab.currentProvider()
	if p == nil || p.Alias != "second" {
		t.Fatalf("current provider = %#v, want second (visible-index semantics)", p)
	}
}

// ---------- 3. 多选与审计（config） ----------

// newLoadedConfigTab 构造已装载、含指定条目（每组 1 条，组名=条目名）的
// config Tab。
func newLoadedCfgTab(t *testing.T, names ...string) *configTab {
	t.Helper()
	mgr := newTestConfigManager(t)
	for _, name := range names {
		src := writeSourceFile(t, "key: "+name+"\n")
		if err := mgr.Create(name, src, filepath.Join(t.TempDir(), name), name, ""); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(80, 20)
	return flushConfig(tab, tab.load())
}

// selectByName 直接把指定条目加入多选集（绕过导航的状态注入）。
func selectByName(tab *configTab, name string) *configTab {
	tab.sel.Toggle(name)
	return tab
}

// TestConfigSpaceKeyToggles：空格键（" "）必须切换勾选（审查 config:481）。
func TestConfigSpaceKeyToggles(t *testing.T) {
	tab := newLoadedCfgTab(t, "alpha")
	tab.focusLeft = false
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeySpace})
	ct := out.(*configTab)
	if ct.sel.SelectionCount() != 1 {
		t.Fatalf("spacebar toggle failed: selection = %d", ct.sel.SelectionCount())
	}
}

// TestConfigSelectedNamesIncludeHidden：被过滤隐藏的已选项保持在批量计划
// 范围内（审查 config:892）。
func TestConfigSelectedNamesIncludeHidden(t *testing.T) {
	tab := newLoadedCfgTab(t, "alpha", "beta")
	tab.focusLeft = false
	tab = selectByName(tab, "alpha")
	tab.filterBox.EnterFresh()
	tab.filterBox.Append("beta")
	names := tab.selectedNames()
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("selectedNames = %v, want [alpha] (hidden selection kept)", names)
	}
}

// TestConfigReconcileSelectionDropsDeleted：数据变化后选择集丢弃已删除条目
// （审查 config:1011）。
func TestConfigReconcileSelectionDropsDeleted(t *testing.T) {
	tab := newLoadedCfgTab(t, "alpha", "beta")
	tab = selectByName(tab, "beta")
	// 模拟 beta 被删除后的重载数据
	tab.itemsByGroup["beta"] = nil
	tab.reconcileSelection()
	if tab.sel.SelectionCount() != 0 {
		t.Fatalf("stale selection survived: %d", tab.sel.SelectionCount())
	}
}

// TestConfigEnterSelectionPlanNilGuard：Config 管理器缺失时批量计划入口
// 不 panic、以警告回落（审查 config:856）。
func TestConfigEnterSelectionPlanNilGuard(t *testing.T) {
	tab := newLoadedCfgTab(t, "alpha")
	tab = selectByName(tab, "alpha")
	tab.mgr.Config = nil
	out, cmd := tab.enterSelectionPlan("install")
	if cmd == nil {
		t.Fatal("nil manager should produce a warning toast cmd")
	}
	msg := drainCmd(t, cmd)
	if wt, ok := msg.(toastMsg); !ok || wt.level != toastWarn {
		t.Fatalf("expected warn toast, got %#v", msg)
	}
	if out.(*configTab).mode != configModeNormal {
		t.Fatal("plan mode must not open without manager")
	}
}

var _ = fmt.Sprintf
var _ = config.Scope{}

// TestTextAllViewCopyUsesRealGroup：All 视图复制按条目真实分组取值——vault
// 查找必须通过；剪贴板工具缺失时报错只能是 clipboard 层面而非「分组不存在」
// （审查 text:960；剪贴板行为因环境而异，两类结果均视为通过）。
func TestTextAllViewCopyUsesRealGroup(t *testing.T) {
	mgr := newTestTextManager(t)
	if err := mgr.AddGroup("prod", "test"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("prod", "secret-key", "v"); err != nil {
		t.Fatal(err)
	}
	tab := newTextTab(Managers{Text: mgr})
	tab.SetSize(80, 20)
	tab = flushText(tab, tab.load())
	// All 伪组视图，游标停在 prod/secret-key
	for i, g := range tab.groups {
		if g.isAll {
			tab.groupIndex = i
		}
	}
	it, ok := tab.currentItem()
	if !ok || it.group != "prod" {
		t.Fatalf("All view current item = %#v, want prod group entry", it)
	}
	msg := drainCmd(t, tab.doCopy())
	switch m := msg.(type) {
	case toastMsg:
		if !strings.Contains(m.text, "copied") {
			t.Fatalf("unexpected toast %#v", m)
		}
	case errMsg:
		if !strings.Contains(m.err.Error(), "clipboard") {
			t.Fatalf("copy failed below clipboard layer (group lookup broken): %v", m.err)
		}
	default:
		t.Fatalf("unexpected copy result %#v", msg)
	}
}
