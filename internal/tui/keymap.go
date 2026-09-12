package tui

import "strings"

// KeyAction 是 keymap 注册表的一行：一个动作的触发键、说明与所属分组。它是
// 键位的唯一真相源——Tab 用 Matches 分发按键，状态栏组名提示与 `?` 键位总览
// 用 Group/Hint 渲染，因此「帮助列出的键」与「实际执行的键」结构性一致，不
// 可能漂移。
type KeyAction struct {
	Keys  []string // bubbletea 键名（如 "r"、"ctrl+r"、"pgup"），多键为同义键
	Desc  string   // 英文说明（键位名保留原文）
	Group string   // 状态栏与 `?` 总览的分组名（见下方 grp* 常量）
}

// 状态栏与 `?` 总览共享的分组名。底栏只显示组名（去重、首现顺序），组内键
// 位全量见 `?` 总览；`?` 总览按组分段渲染。
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

// Hint 渲染单行提示：「r 重命名」/「up/k 上移」。
func (k KeyAction) Hint() string {
	return strings.Join(k.Keys, "/") + " " + k.Desc
}

// groupBar 把动作列表折叠成底栏单行提示：组名按首现顺序去重，尾部固定
// `? keys` 指向完整键位总览。底栏长度只随分组数增长，与组内键数无关。
func groupBar(bindings []KeyAction) string {
	seen := make(map[string]bool)
	parts := make([]string, 0, 4)
	for _, b := range bindings {
		g := b.Group
		if g == "" {
			g = "Keys"
		}
		if !seen[g] {
			seen[g] = true
			parts = append(parts, g)
		}
	}
	parts = append(parts, "? keys")
	return strings.Join(parts, " · ")
}

// 共享动作：全局动词（grill D7 附录），所有列表 Tab 同义。Tab 的 Update 用
// 它们分发按键，Bindings() 用它们声明键位，两侧共用同一常量。
var (
	actNew     = KeyAction{[]string{"n"}, "new", grpItem}
	actEdit    = KeyAction{[]string{"e"}, "edit", grpItem}
	actRename  = KeyAction{[]string{"r"}, "rename", grpItem}
	actDelete  = KeyAction{[]string{"d"}, "delete", grpItem}
	actExport  = KeyAction{[]string{"x"}, "export", grpItem}
	actImport  = KeyAction{[]string{"i"}, "import", grpItem}
	actRefresh = KeyAction{[]string{"ctrl+r"}, "refresh", grpFilter}
	actFilter  = KeyAction{[]string{"/"}, "filter", grpFilter}
	actDetail  = KeyAction{[]string{"enter"}, "view", grpItem}
	actEsc     = KeyAction{[]string{"esc"}, "back/cancel", grpNav}
	actUp      = KeyAction{[]string{"up", "k"}, "up", grpNav}
	actDown    = KeyAction{[]string{"down", "j"}, "down", grpNav}
	actLeft    = KeyAction{[]string{"left", "h"}, "left pane", grpNav}
	actRight   = KeyAction{[]string{"right", "l"}, "right pane", grpNav}
	actTop     = KeyAction{[]string{"g"}, "top", grpNav}
	actBottom  = KeyAction{[]string{"G"}, "bottom", grpNav}
	actPageUp  = KeyAction{[]string{"pgup"}, "page up", grpNav}
	actPageDn  = KeyAction{[]string{"pgdown"}, "page down", grpNav}
)

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
