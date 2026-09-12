package tui

import "strings"

// KeyAction 是 keymap 注册表的一行：一个动作的触发键与说明。它是键位的
// 唯一真相源——Tab 用 Matches 分发按键，状态栏提示与 `?` 键位总览用 Hint
// 渲染，因此「帮助列出的键」与「实际执行的键」结构性一致，不可能漂移。
type KeyAction struct {
	Keys []string // bubbletea 键名（如 "r"、"ctrl+r"、"pgup"），多键为同义键
	Desc string   // 简体中文说明（键位名保留原文）
}

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

// hintsFromBindings 把动作列表拼成状态栏单行提示（` · ` 分隔）。
func hintsFromBindings(bindings []KeyAction) string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, b.Hint())
	}
	return strings.Join(parts, " · ")
}

// 共享动作：全局动词（grill D7 附录），所有列表 Tab 同义。Tab 的 Update 用
// 它们分发按键，Bindings() 用它们声明键位，两侧共用同一常量。
var (
	actNew     = KeyAction{[]string{"n"}, "新建"}
	actEdit    = KeyAction{[]string{"e"}, "编辑"}
	actRename  = KeyAction{[]string{"r"}, "重命名"}
	actDelete  = KeyAction{[]string{"d"}, "删除"}
	actExport  = KeyAction{[]string{"x"}, "导出"}
	actImport  = KeyAction{[]string{"i"}, "导入"}
	actRefresh = KeyAction{[]string{"ctrl+r"}, "刷新"}
	actFilter  = KeyAction{[]string{"/"}, "过滤"}
	actDetail  = KeyAction{[]string{"enter"}, "详情/查看"}
	actEsc     = KeyAction{[]string{"esc"}, "返回/取消/清过滤"}
	actUp      = KeyAction{[]string{"up", "k"}, "上移"}
	actDown    = KeyAction{[]string{"down", "j"}, "下移"}
	actLeft    = KeyAction{[]string{"left", "h"}, "左栏焦点"}
	actRight   = KeyAction{[]string{"right", "l"}, "右栏焦点"}
	actTop     = KeyAction{[]string{"g"}, "跳顶"}
	actBottom  = KeyAction{[]string{"G"}, "跳底"}
	actPageUp  = KeyAction{[]string{"pgup"}, "上翻页"}
	actPageDn  = KeyAction{[]string{"pgdown"}, "下翻页"}
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
