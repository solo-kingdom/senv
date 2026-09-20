package tui

import (
	"strings"
	"testing"
)

// keymapConsistencyContract 是「Tab 的 Update 分发处理的键 ⊆ Bindings() 注册
// 的键」的显式契约表（design D3）：`?` 总览（help.go newHelpTab）与底栏
// （model.go groupBar，跳过 NoBar）都消费 Bindings()，注册缺失即帮助失明。
// 每行是一个 Tab 状态（含焦点栏/mode）；want 里的每个键都必须能在该状态
// Bindings() 中找到（同义键按 "/" 切分后任一命中即可），absent 里的键必须
// 不出现。新增按键时须同步维护本表——漏登记会让这里的断言变红。
var keymapConsistencyContract = []struct {
	name   string
	new    func() Tab
	want   []string
	absent []string
	// wantDesc 可选：键与其描述子串（帮助可读性契约，如 F 的 force delete
	// 说明须与确认页文案一致）。
	wantDesc [][2]string
}{
	{
		name: "keypair/list",
		new:  func() Tab { return newKeyPairTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "enter", "g", "G",
			"pgup", "pgdown", "/", "ctrl+r", "n", "i", "r", "e", "d", "A", "D", "v", "p"},
		absent: []string{"space", "a", "m"},
	},
	{
		name: "keypair/group",
		new: func() Tab {
			t := newKeyPairTab(Managers{})
			t.focus = kpPaneGroup
			return t
		},
		want: []string{"up", "down", "left", "right", "enter", "g", "G",
			"pgup", "pgdown", "/", "ctrl+r", "p"},
		absent: []string{"space", "a", "v"},
	},
	{
		name: "keypair/delete-confirm",
		new: func() Tab {
			t := newKeyPairTab(Managers{})
			t.mode = kpModeDeleteKey
			return t
		},
		want: []string{"enter", "y", "esc", "n", "F"},
		wantDesc: [][2]string{
			{"F", "force delete"},
		},
	},
	{
		name: "keypair/materialize",
		new: func() Tab {
			t := newKeyPairTab(Managers{})
			t.mode = kpModeMaterialize
			return t
		},
		want: []string{"enter", "y", "esc", "n"},
	},
	{
		name: "keypair/prune",
		new: func() Tab {
			t := newKeyPairTab(Managers{})
			t.mode = kpModePrune
			return t
		},
		want: []string{"enter", "y", "esc", "n"},
	},
	{
		name: "env/group",
		new:  func() Tab { return newEnvTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "t", "r", "d", "+", "n", "e", "y", "D", "/", "ctrl+r"},
		absent: []string{"space", "a", "v"},
	},
	{
		name: "env/items",
		new: func() Tab {
			t := newEnvTab(Managers{})
			t.focusLeft = false
			return t
		},
		want: []string{"up", "down", "left", "right", "v", "g", "G",
			"pgup", "pgdown", "space", "a", "e", "n", "d", "r", "t",
			"+", "y", "D", "/", "ctrl+r"},
	},
	{
		name: "env/delete-confirm",
		new: func() Tab {
			t := newEnvTab(Managers{})
			t.mode = envModeDeleteConfirm
			return t
		},
		want: []string{"enter", "y", "esc", "n"},
	},
	{
		name: "text/group",
		new:  func() Tab { return newTextTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "r", "d", "+", "n", "e", "i", "y", "x", "D", "/", "ctrl+r"},
		absent: []string{"space", "a"},
	},
	{
		name: "text/items",
		new: func() Tab {
			t := newTextTab(Managers{})
			t.focusLeft = false
			return t
		},
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "space", "a", "e", "n", "d", "r", "i",
			"y", "x", "+", "D", "/", "ctrl+r"},
	},
	{
		name: "backup/group",
		new:  func() Tab { return newBackupTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "r", "d", "+", "n", "e", "i", "y", "x", "/", "ctrl+r"},
		absent: []string{"space", "a", "D"},
	},
	{
		name: "backup/items",
		new: func() Tab {
			t := newBackupTab(Managers{})
			t.focusLeft = false
			return t
		},
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "space", "a", "e", "n", "d", "r", "i",
			"y", "x", "+", "/", "ctrl+r"},
		absent: []string{"D"},
	},
	{
		name: "config/items",
		new:  func() Tab { return newConfigTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "enter", "e", "r", "m", "n", "x",
			"space", "a", "i", "I", "u", "U", "d", "/", "ctrl+r"},
	},
	{
		name: "config/group",
		new: func() Tab {
			t := newConfigTab(Managers{})
			t.focusLeft = true
			return t
		},
		want: []string{"up", "down", "left", "right", "g", "G",
			"pgup", "pgdown", "I", "U", "enter", "e", "r", "n", "x", "d", "m", "i", "u", "/", "ctrl+r"},
		absent: []string{"space", "a"},
	},
	{
		name: "config/plan",
		new: func() Tab {
			t := newConfigTab(Managers{})
			t.mode = configModePlan
			return t
		},
		want: []string{"y", "enter", "esc", "n"},
	},
	{
		name: "ai",
		new:  func() Tab { return newAITab(Managers{}) },
		want: []string{"up", "down", "left", "right", "enter", "n", "e", "r", "d",
			"s", "M", "R", "g", "G", "pgup", "pgdown", "/", "ctrl+r"},
	},
	{
		name: "ssh/host",
		new:  func() Tab { return newSSHTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "enter", "n", "e", "d",
			"space", "a", "x", "A", "u", "g", "G", "pgup", "pgdown", "/", "ctrl+r"},
	},
	{
		name: "ssh/group",
		new: func() Tab {
			t := newSSHTab(Managers{})
			t.focus = paneGroup
			return t
		},
		want: []string{"up", "down", "left", "right", "enter", "x", "A", "u",
			"g", "G", "pgup", "pgdown", "/", "ctrl+r"},
		absent: []string{"space", "a"},
	},
	{
		name: "ssh/unexport-confirm",
		new: func() Tab {
			t := newSSHTab(Managers{})
			t.mode = sshModeUnexport
			t.pendingUnexport = &unexportConfirm{registered: true, fragments: 1}
			return t
		},
		want: []string{"enter", "y", "esc", "n"},
	},
	{
		name: "ssh/export-preview",
		new: func() Tab {
			t := newSSHTab(Managers{})
			t.mode = sshModeExportPreview
			t.exportContent = "Host a\n"
			return t
		},
		want: []string{"up", "down", "g", "G", "pgup", "pgdown", "w", "esc"},
	},
	{
		name: "mcp",
		new:  func() Tab { return newMCPTab(Managers{}) },
		want: []string{"up", "down", "left", "right", "enter", "n", "e",
			"space", "a", "d", "s", "x", "X", "u", "U", "i",
			"g", "G", "pgup", "pgdown", "/"},
	},
	{
		name: "mcp/import-report",
		new: func() Tab {
			t := newMCPTab(Managers{})
			t.mode = mcpModeImportReport
			t.importReport = &mcpImportDoneMsg{}
			return t
		},
		want: []string{"esc", "enter"},
	},
}

// registeredKeySet 把 Bindings() 展开为键Token集合（"enter/y" 视为 enter、y
// 两个同义键，与分发侧 msg.String() 可比；"/" 自身是过滤键，不按分隔符切分）。
func registeredKeySet(bs []KeyAction) map[string]bool {
	set := map[string]bool{}
	for _, b := range bs {
		for _, k := range b.Keys {
			if k == "/" {
				set["/"] = true
				continue
			}
			for _, tok := range strings.Split(k, "/") {
				set[tok] = true
			}
		}
	}
	return set
}

// TestKeymapConsistency 断言契约表每一行的 want 键全部注册进对应状态的
// Bindings()、absent 键全部缺席——分发侧（各 Tab Update 的 switch）处理的
// 键不得在帮助注册表里失明（tui-viewer「键位总览」双向完整性）。
func TestKeymapConsistency(t *testing.T) {
	for _, tc := range keymapConsistencyContract {
		t.Run(tc.name, func(t *testing.T) {
			bs := tc.new().Bindings()
			set := registeredKeySet(bs)
			for _, k := range tc.want {
				if !set[k] {
					t.Fatalf("%s bindings missing %q: %v", tc.name, k, set)
				}
			}
			for _, k := range tc.absent {
				if set[k] {
					t.Fatalf("%s bindings must not contain %q: %v", tc.name, k, set)
				}
			}
			for _, d := range tc.wantDesc {
				found := false
				for _, b := range bs {
					for _, k := range b.Keys {
						if k == d[0] && strings.Contains(b.Desc, d[1]) {
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("%s bindings missing %q with desc %q: %v", tc.name, d[0], d[1], bs)
				}
			}
		})
	}
}

// TestKeymapConsistencyHelpSource 断言契约表每一行的 want 键都出现在帮助渲染
// 路径的数据源里：`?` 总览（newHelpTab）直接快照 Bindings()，本用例把这条
// 消费路径也纳入回归网。
func TestKeymapConsistencyHelpSource(t *testing.T) {
	for _, tc := range keymapConsistencyContract {
		t.Run(tc.name, func(t *testing.T) {
			tab := tc.new()
			help := newHelpTab(tab.Title(), tab)
			set := registeredKeySet(help.bindings)
			for _, k := range tc.want {
				if !set[k] {
					t.Fatalf("help source for %s missing %q: %v", tc.name, k, set)
				}
			}
		})
	}
}
