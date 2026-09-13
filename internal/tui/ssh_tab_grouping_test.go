package tui

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// 本文件锁定 ssh-tui-grouping change 的 TUI 行为（openspec change
// ssh-tui-grouping，specs/ssh-assets/spec.md ADDED Requirements）：
// 分组侧栏、行内 tags、Host 栏四维匹配。KeyPair 引用计数/过滤的用例随
// keypair-tab-grouping change 迁至 KeyPair Tab（见文件内 keyPairTab 用例）。

// groupingTestHosts / groupingTestKeyPairs 是 SSH 与 KeyPair 两个 Tab 共用
// 的固定数据集：
//
//	prod:  db(key:k1) / rich(#a #b #c #d) / web(#gpu #p0, key:k1)
//	dev:   edge
//	无组:  misc(#gpu)
//	keypair: k1(group prod，被 db+web 引用) / old(无组，零引用)
func groupingTestHosts() []storage.HostEntry {
	return []storage.HostEntry{
		{Alias: "web", Hostname: "w1", Group: "prod", Tags: []string{"gpu", "p0"}, IdentityKey: "k1"},
		{Alias: "db", Hostname: "d1", Group: "prod", IdentityKey: "k1"},
		{Alias: "rich", Hostname: "r1", Group: "prod", Tags: []string{"a", "b", "c", "d"}},
		{Alias: "edge", Hostname: "e1", Group: "dev"},
		{Alias: "misc", Hostname: "m1", Tags: []string{"gpu"}},
	}
}

func groupingTestKeyPairs() []ssh.KeyPairSummary {
	return []ssh.KeyPairSummary{
		{Name: "k1", Fingerprint: "fp", Group: "prod"},
		{Name: "old", Fingerprint: "fp"},
	}
}

// newGroupingSSHTab 构造一个已装载的两栏 SSH Tab（100 列，与既有用例一致；
// 80 列会截断行内片段）。
func newGroupingSSHTab(t *testing.T) *sshTab {
	t.Helper()
	tab := newSSHTab(Managers{})
	tab.SetSize(100, 24)
	next, cmd := tab.Update(sshLoadedMsg{hosts: groupingTestHosts(), keyPairs: groupingTestKeyPairs()})
	if cmd != nil {
		t.Fatal("loaded update unexpectedly returned a command")
	}
	return next.(*sshTab)
}

// newGroupingKeyPairTab 构造同一数据集下的已装载 KeyPair Tab。
func newGroupingKeyPairTab(t *testing.T) *keyPairTab {
	t.Helper()
	tab := newKeyPairTab(Managers{})
	tab.SetSize(100, 24)
	next, cmd := tab.Update(kpLoadedMsg{hosts: groupingTestHosts(), keyPairs: groupingTestKeyPairs()})
	if cmd != nil {
		t.Fatal("loaded update unexpectedly returned a command")
	}
	return next.(*keyPairTab)
}

func visibleAliases(t *testing.T, tab *sshTab) []string {
	t.Helper()
	hosts := tab.visibleHosts()
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h.Alias)
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSSHTabSidebarGroupBrowsing：选中组决定中间栏集合，组内按别名字典序；
// All 伪组展示全部（Requirement: TUI SSH 分组侧栏）。
func TestSSHTabSidebarGroupBrowsing(t *testing.T) {
	tab := newGroupingSSHTab(t)

	groups := tab.groups()
	wantNames := []string{"All", "dev", "prod", "未分组"}
	if len(groups) != len(wantNames) {
		t.Fatalf("groups = %v", groups)
	}
	for i, name := range wantNames {
		if groups[i].name != name {
			t.Fatalf("groups[%d].name = %q, want %q (%v)", i, groups[i].name, name, groups)
		}
	}
	if groups[0].count != 5 || groups[2].count != 3 || groups[3].count != 1 {
		t.Fatalf("group counts wrong: %v", groups)
	}

	// 选中 prod：中间栏仅该组 host，别名字典序。
	tab.focus = paneGroup
	tab.groupIndex = 2 // prod
	if got := visibleAliases(t, tab); !eqStrings(got, []string{"db", "rich", "web"}) {
		t.Fatalf("prod visible = %v", got)
	}

	// All：全部 host。
	tab.groupIndex = 0
	if got := visibleAliases(t, tab); !eqStrings(got, []string{"db", "edge", "misc", "rich", "web"}) {
		t.Fatalf("All visible = %v", got)
	}
}

// TestSSHTabUngroupedFallback：「未分组」仅在有未归类 host 时出现，置底；
// 全部 host 有分组时 MUST NOT 出现（Scenario: 未分组兜底/全部 Host 均有分组）。
func TestSSHTabUngroupedFallback(t *testing.T) {
	tab := newGroupingSSHTab(t)
	groups := tab.groups()
	last := groups[len(groups)-1]
	if !last.isUngrouped || last.name != "未分组" || last.count != 1 {
		t.Fatalf("ungrouped row missing or misplaced: %v", groups)
	}

	// 给 misc 归组后「未分组」消失。
	for i := range tab.hosts {
		if tab.hosts[i].Alias == "misc" {
			tab.hosts[i].Group = "dev"
		}
	}
	for _, g := range tab.groups() {
		if g.isUngrouped {
			t.Fatalf("ungrouped row must not appear when every host is grouped: %v", tab.groups())
		}
	}
}

// TestSSHTabTagsInline：行尾 tags 片段 `#tag` 前缀、最多 2 个、超出 `+n`；
// 无 tags 不渲染（Requirement: TUI Host 行内 tags 展示）。
func TestSSHTabTagsInline(t *testing.T) {
	tab := newGroupingSSHTab(t)
	view := tab.View()
	if !strings.Contains(view, "#gpu #p0") {
		t.Fatalf("tags fragment missing (web):\n%s", view)
	}
	if !strings.Contains(view, "#a #b +2") {
		t.Fatalf("overflow tags must collapse to +n (rich):\n%s", view)
	}
	// 无 tags 的 host（edge）行内不得出现 # 片段。
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "edge →") && strings.Contains(line, "#") {
			t.Fatalf("tagless host row rendered a tags fragment: %q", line)
		}
	}
}

// TestSSHTabKeyPairRefCount：行内引用计数复用 hostRefs()；零引用灰显
// 「未被引用」；排序保持名字序（承接原「KeyPair 栏引用计数与过滤」，
// 呈现迁至 KeyPair Tab）。
func TestSSHTabKeyPairRefCount(t *testing.T) {
	tab := newGroupingKeyPairTab(t)
	lines := tab.keyPairListLines(80)
	if len(lines) != 2 {
		t.Fatalf("keypair lines = %d", len(lines))
	}
	// 名字序不动：k1 在前。
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
	// 视图层（100 列）：零引用提示仍可见（引用计数行在窄栏截断属预期）。
	if view := tab.View(); !strings.Contains(view, "未被引用") {
		t.Fatalf("view missing unreferenced hint:\n%s", view)
	}
}

// TestSSHTabKeyPairPaneFilter：KeyPair 列表 `/` 按名称过滤，esc 恢复
// （Scenario: 按名称过滤）。
func TestSSHTabKeyPairPaneFilter(t *testing.T) {
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

// TestSSHTabHostFilterFourDimensions：Host 栏 `/` 匹配 alias + hostname +
// tags + group（Requirement: SSH 过滤与全局搜索匹配范围）。
func TestSSHTabHostFilterFourDimensions(t *testing.T) {
	cases := []struct {
		term string
		want []string
	}{
		{"db", []string{"db"}},                  // alias
		{"w1", []string{"web"}},                 // hostname
		{"gpu", []string{"misc", "web"}},        // tag
		{"prod", []string{"db", "rich", "web"}}, // group
		{"dev", []string{"edge"}},               // group（单组）
	}
	for _, tc := range cases {
		tab := newGroupingSSHTab(t)
		tab.focus = paneHost
		tab = typeKeys(tab, "/"+tc.term)
		if got := visibleAliases(t, tab); !eqStrings(got, tc.want) {
			t.Fatalf("/%s visible = %v, want %v", tc.term, got, tc.want)
		}
	}
}

// TestSSHTabSearchMatchesGroupAndTags：全局搜索 SSH 类目纳入 group+tags
// （Scenario: 全局搜索命中 group / 全局搜索命中 tag）。
func TestSSHTabSearchMatchesGroupAndTags(t *testing.T) {
	mgrs := newFullManagers(t)
	if err := mgrs.SSH.AddHost(&storage.HostEntry{
		Alias: "web", Hostname: "10.0.0.9", Group: "prod", Tags: []string{"gpu"},
	}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	all := gatherAll(t, mgrs)

	for _, needle := range []string{"prod", "gpu"} {
		hits := searchFor(all, needle)
		var saw bool
		for _, r := range hits {
			if r.resultType == typeSSH && r.key == "web" {
				saw = true
				if r.preview != "10.0.0.9" {
					t.Fatalf("preview drifted: %q", r.preview)
				}
			}
		}
		if !saw {
			t.Fatalf("search %q did not hit SSH host web (group/tags not matched)", needle)
		}
	}
}

// typeKeys feeds printable keys (plus esc/enter) through the tab Update loop.
func typeKeys(tab *sshTab, keys string) *sshTab {
	for _, k := range keys {
		next, _ := tab.Update(runeKey(string(k)))
		tab = next.(*sshTab)
	}
	return tab
}

// typeKPKeys feeds printable keys through the keypair tab Update loop.
func typeKPKeys(tab *keyPairTab, keys string) *keyPairTab {
	for _, k := range keys {
		next, _ := tab.Update(runeKey(string(k)))
		tab = next.(*keyPairTab)
	}
	return tab
}
