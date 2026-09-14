package tui

import "strings"

// KeyAction 是 keymap 注册表的一行：一个动作的触发键、说明与所属分组。它是
// 键位的唯一真相源——Tab 用 Matches 分发按键，底栏 Hint 与 `?` 键位总览
// 用 Group/Hint/NoBar 渲染，因此「帮助列出的键」与「实际执行的键」结构性一致，不
// 可能漂移。
type KeyAction struct {
	Keys  []string // bubbletea 键名（如 "r"、"ctrl+r"、"pgup"），多键为同义键
	Desc  string   // 英文说明（键位名保留原文）
	Group string   // `?` 总览的分组名（见下方 grp* 常量）
	NoBar bool     // true：只进 `?` 总览，不进底栏（导航/次要动词）
}

// 状态栏与 `?` 总览共享的分组名。底栏展开非 NoBar 的 Hint；`?` 总览按组分段。
const (
	grpNav     = "Navigate"
	grpItem    = "Items"
	grpGroup   = "Groups"
	grpFilter  = "Filter"
	grpForm    = "Form"
	grpConfirm = "Confirm"
	grpWizard  = "Wizard"
	grpSearch  = "Search"
)

// Matches 报告某个 bubbletea key.String() 是否触发本动作。
func (k KeyAction) Matches(key string) bool {
	for _, kk := range k.Keys {
		if kk == key {
			return true
		}
	}
	return false
}

// Hint 渲染单行提示：「r rename」/「up/k up」。
func (k KeyAction) Hint() string {
	return strings.Join(k.Keys, "/") + " " + k.Desc
}

// groupBar 把动作列表展成底栏单行：跳过 NoBar（导航与次要键留给 `?`），
// 按注册顺序输出 Hint，尾部固定 `?` 指向完整键位总览。
func groupBar(bindings []KeyAction) string {
	parts := make([]string, 0, len(bindings)+1)
	for _, b := range bindings {
		if b.NoBar {
			continue
		}
		parts = append(parts, b.Hint())
	}
	parts = append(parts, "?")
	return strings.Join(parts, " · ")
}

// 共享动作：全局动词（grill D7 附录），所有列表 Tab 同义。Tab 的 Update 用
// 它们分发按键，Bindings() 用它们声明键位，两侧共用同一常量。
// 导航与 ctrl+r（由 Model 处理）默认 NoBar，底栏只留场景操作键。
var (
	actNew       = KeyAction{[]string{"n"}, "new", grpItem, false}
	actEdit      = KeyAction{[]string{"e"}, "edit", grpItem, false}
	actRename    = KeyAction{[]string{"r"}, "rename", grpItem, false}
	actDelete    = KeyAction{[]string{"d"}, "delete", grpItem, false}
	actExport    = KeyAction{[]string{"x"}, "export", grpItem, false}
	actApply     = KeyAction{[]string{"A"}, "apply export", grpItem, false}
	actImport    = KeyAction{[]string{"i"}, "import", grpItem, false}
	actSelect    = KeyAction{[]string{"space"}, "toggle select", grpItem, false}
	actSelectAll = KeyAction{[]string{"a"}, "select all visible", grpItem, false}
	actRefresh   = KeyAction{[]string{"ctrl+r"}, "refresh", grpFilter, true}
	actFilter    = KeyAction{[]string{"/"}, "filter", grpFilter, false}
	actDetail    = KeyAction{[]string{"enter"}, "view", grpItem, false}
	actEsc       = KeyAction{[]string{"esc"}, "back/cancel", grpNav, false}
	actUp        = KeyAction{[]string{"up", "k"}, "up", grpNav, true}
	actDown      = KeyAction{[]string{"down", "j"}, "down", grpNav, true}
	actLeft      = KeyAction{[]string{"left", "h"}, "left pane", grpNav, true}
	actRight     = KeyAction{[]string{"right", "l"}, "right pane", grpNav, true}
	actTop       = KeyAction{[]string{"g"}, "top", grpNav, true}
	actBottom    = KeyAction{[]string{"G"}, "bottom", grpNav, true}
	actPageUp    = KeyAction{[]string{"pgup"}, "page up", grpNav, true}
	actPageDn    = KeyAction{[]string{"pgdown"}, "page down", grpNav, true}
)

// formBindings 是打开表单时的共享键位（可追加枚举/多行等额外动作）。
func formBindings(extra ...KeyAction) []KeyAction {
	out := make([]KeyAction, 0, 3+len(extra))
	out = append(out, KeyAction{[]string{"tab/↑↓"}, "switch field", grpForm, false})
	out = append(out, extra...)
	out = append(out,
		KeyAction{[]string{"enter"}, "submit", grpForm, false},
		KeyAction{[]string{"esc"}, "cancel", grpForm, false},
	)
	return out
}

// confirmBindings 是确认弹层的共享键位。
func confirmBindings() []KeyAction {
	return []KeyAction{
		{[]string{"enter/y"}, "confirm", grpConfirm, false},
		{[]string{"esc/n"}, "cancel", grpConfirm, false},
	}
}

// detailBindings 是只读详情弹层的共享键位。
func detailBindings() []KeyAction {
	return []KeyAction{
		actUp, actDown, actPageUp, actPageDn,
		{[]string{"esc/enter"}, "close", grpConfirm, false},
	}
}

// filterBindings 是过滤输入态的共享键位（Env/Text 仅 esc 清除，传 confirm=false）。
func filterBindings(confirm bool) []KeyAction {
	if confirm {
		return []KeyAction{
			{[]string{"enter"}, "confirm", grpFilter, false},
			{[]string{"esc"}, "clear", grpFilter, false},
		}
	}
	return []KeyAction{{[]string{"esc"}, "clear", grpFilter, false}}
}

// navBindings 是列表 Tab 通用导航键（均 NoBar，供 `?` 总览）。
func navBindings(withPanes bool) []KeyAction {
	if withPanes {
		return []KeyAction{actUp, actDown, actLeft, actRight, actTop, actBottom, actPageUp, actPageDn}
	}
	return []KeyAction{actUp, actDown, actTop, actBottom, actPageUp, actPageDn}
}

// overlay chrome 预算：search/help overlay 样式为圆角边框 + Padding(1,2)，
// 即 4 行 6 列；searchTab 收到的是全终端尺寸，还要减去外框 chrome
// （tab strip 2 行 + 底栏 1 行 + 外框边框 2 行）。
const (
	overlayRows = 4
	overlayCols = 6
	frameRows   = 5
)

// pageStep 估算上/下翻页的游标步长。各 Tab 视图高度公式不同，这里取
// 「总高减去典型 chrome」的保守值；窗口跟随渲染会自动收拢到游标所在行，
// 步长不精确只影响一次跳几行，不影响正确性。
func pageStep(height int) int {
	p := height - 8
	if p < 1 {
		return 1
	}
	return p
}
