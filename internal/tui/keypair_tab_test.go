package tui

import (
	"strings"
	"testing"
)

// 本文件锁定 keypair-tab-grouping change 的 KeyPair Tab 行为（openspec
// change keypair-tab-grouping，specs/ssh-assets/spec.md ADDED「TUI KeyPair
// Tab 分组侧栏」）：分组侧栏、引用计数与零引用、名称过滤、group 编辑。
// 数据集复用 ssh_tab_grouping_test.go 的 groupingTestHosts/KeyPairs
//（k1=group prod 被 db+web 引用；old=无组零引用），终端宽 100 列。

func visibleKeyNames(t *testing.T, tab *keyPairTab) []string {
	t.Helper()
	keys := tab.visibleKeyPairs()
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.Name)
	}
	return out
}

// TestKeyPairTabSidebarGroupBrowsing：选中组决定列表集合，组内按名称
// 字典序；All 伪组展示全部（Scenario: 按组浏览 KeyPair / All）。
func TestKeyPairTabSidebarGroupBrowsing(t *testing.T) {
	tab := newGroupingKeyPairTab(t)

	groups := tab.groups()
	// 数据：k1=prod、old=无组 → All / prod / 未分组。
	wantNames := []string{"All", "prod", "未分组"}
	if len(groups) != len(wantNames) {
		t.Fatalf("groups = %v", groups)
	}
	for i, name := range wantNames {
		if groups[i].name != name {
			t.Fatalf("groups[%d].name = %q, want %q (%v)", i, groups[i].name, name, groups)
		}
	}
	if !groups[0].isAll || groups[0].count != 2 || groups[1].count != 1 || groups[2].count != 1 {
		t.Fatalf("group rows wrong: %v", groups)
	}

	// 选中 prod：列表仅 k1。
	tab.focus = kpPaneGroup
	tab.groupIndex = 1
	if got := visibleKeyNames(t, tab); !eqStrings(got, []string{"k1"}) {
		t.Fatalf("prod visible = %v", got)
	}

	// All：全部，按名称序。
	tab.groupIndex = 0
	if got := visibleKeyNames(t, tab); !eqStrings(got, []string{"k1", "old"}) {
		t.Fatalf("All visible = %v", got)
	}
}

// TestKeyPairTabUngroupedFallback：「未分组」仅在有未归类 keypair 时出现
// 且置底；全部有组时 MUST NOT 出现（Scenario: 未分组兜底）。
func TestKeyPairTabUngroupedFallback(t *testing.T) {
	tab := newGroupingKeyPairTab(t)
	groups := tab.groups()
	last := groups[len(groups)-1]
	if !last.isUngrouped || last.name != "未分组" || last.count != 1 {
		t.Fatalf("ungrouped row missing or misplaced: %v", groups)
	}

	for i := range tab.keyPairs {
		if tab.keyPairs[i].Name == "old" {
			tab.keyPairs[i].Group = "qa"
		}
	}
	for _, g := range tab.groups() {
		if g.isUngrouped {
			t.Fatalf("ungrouped row must not appear when every keypair is grouped: %v", tab.groups())
		}
	}
}

// TestKeyPairTabRefCount：行内引用计数 `被 N 个 Host 引用`；零引用灰显
// 「未被引用」；两行均按名字序（Scenario: 引用计数与零引用）。
func TestKeyPairTabRefCount(t *testing.T) {
	tab := newGroupingKeyPairTab(t)
	lines := tab.keyPairListLines(80)
	if len(lines) != 2 {
		t.Fatalf("keypair lines = %d", len(lines))
	}
	// 名字序不动：k1（被 db+web 引用）在前，old（零引用）在后。
	if !strings.Contains(lines[0], "k1") || !strings.Contains(lines[0], "被 2 个 Host 引用") {
		t.Fatalf("refcount line wrong: %q", lines[0])
	}
	if !strings.Contains(lines[1], "old") || !strings.Contains(lines[1], "未被引用") {
		t.Fatalf("unreferenced hint missing: %q", lines[1])
	}
	// 灰显：「未被引用」走 faintTextStyle 渲染（同一样式变量，保证弱化视觉）。
	if !strings.Contains(lines[1], faintTextStyle.Render("未被引用")) {
		t.Fatalf("unreferenced hint must use the faint style: %q", lines[1])
	}
}

// TestKeyPairTabNameFilter：`/` 按名称过滤，esc 恢复
// （Scenario: 按名称过滤）。
func TestKeyPairTabNameFilter(t *testing.T) {
	tab := newGroupingKeyPairTab(t)

	tab = typeKPKeys(tab, "/ol")
	if got := tab.visibleKeyPairs(); len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("filtered keypairs = %v", got)
	}
	if _, ok := tab.currentKey(); !ok {
		t.Fatal("cursor should land on the only visible keypair")
	}
	next, _ := tab.Update(runeKey("esc"))
	tab = next.(*keyPairTab)
	if got := tab.visibleKeyPairs(); len(got) != 2 {
		t.Fatalf("esc should restore the full keypair list, got %d", len(got))
	}
}

// TestKeyPairTabEditGroup：`e` 打开仅含 group 的表单，提交后走
// UpdateKeyPair 写库，reload 后侧栏组归属随之变化。
func TestKeyPairTabEditGroup(t *testing.T) {
	tab, _ := newKeyPairCrudTab(t)
	keyPath := writeSSHTUIKey(t, "id_ed25519")
	if _, err := tab.mgr.SSH.ImportKeyPair("web-key", keyPath, false); err != nil {
		t.Fatalf("import keypair: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*keyPairTab)
	// 初始无组 → 侧栏只有 All + 未分组。
	if groups := tab.groups(); len(groups) != 2 || !groups[1].isUngrouped {
		t.Fatalf("initial groups = %v", groups)
	}

	out, _ := tab.Update(runeKey("e"))
	tab = out.(*keyPairTab)
	if tab.form == nil {
		t.Fatal("e should open the group edit form")
	}
	if got := tab.form.Values()["group"]; got != "" {
		t.Fatalf("group field should start empty, got %q", got)
	}
	tab = submitKPForm(t, tab, map[string]string{"group": "prod"})
	if tab.form != nil {
		t.Fatalf("group form still open: %#v", tab.form.errs)
	}
	summary, err := tab.mgr.SSH.GetKeyPairSummary("web-key")
	if err != nil || summary.Group != "prod" {
		t.Fatalf("group not persisted: %+v %v", summary, err)
	}
	// reload 后侧栏：prod 组出现，「未分组」消失。
	groups := tab.groups()
	if len(groups) != 2 || groups[1].name != "prod" || groups[1].count != 1 {
		t.Fatalf("groups after edit = %v", groups)
	}
	for _, g := range tab.groups() {
		if g.isUngrouped {
			t.Fatalf("ungrouped row must disappear after grouping: %v", tab.groups())
		}
	}
}

// TestKeyPairTabMasksPrivateKey：私钥明文绝不进入 Tab 状态或渲染（既有
// 安全语义的 KeyPair Tab 侧锁定；MODIFIED「TUI 编辑与 MCP 只读集成」）。
func TestKeyPairTabMasksPrivateKey(t *testing.T) {
	tab, _ := newKeyPairCrudTab(t)
	if _, err := tab.mgr.SSH.ImportKeyPair("web-key", writeSSHTUIKey(t, "id_ed25519"), false); err != nil {
		t.Fatalf("import keypair: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*keyPairTab)

	if tab.keyPairs[0].PublicKey == "" {
		t.Fatal("fixture key should carry a public key half")
	}
	view := tab.View()
	if strings.Contains(view, "PRIVATE KEY") || strings.Contains(view, tab.keyPairs[0].PublicKey) {
		t.Fatalf("list view must stay metadata-only:\n%s", view)
	}
	// 详情浮层展示公钥与指纹，但不展示私钥。
	out, _ := tab.Update(runeKey("enter"))
	tab = out.(*keyPairTab)
	if tab.detail == nil {
		t.Fatal("enter should open the detail overlay")
	}
	detail := tab.detail.View()
	if !strings.Contains(detail, "fingerprint:") || !strings.Contains(detail, "group:") {
		t.Fatalf("detail missing metadata:\n%s", detail)
	}
	if strings.Contains(detail, "PRIVATE KEY") {
		t.Fatalf("detail leaked private key material:\n%s", detail)
	}
}
