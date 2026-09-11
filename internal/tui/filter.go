package tui

// Filter 是共享的 Tab 内过滤状态机（tui-ux-filter）：`/` 进入输入态、
// 可打印字符追加、backspace 回删、enter 确认退出（保留词）、esc 清词退出。
// 匹配统一走 matchKey（标识符子串、大小写不敏感、绝不匹配值）。
type Filter struct {
	active bool   // 输入态（键盘输入进入过滤器）
	term   string // 过滤词（空 = 不过滤）
}

// Enter 进入输入态（保留既有词，audit 语义）。
func (f *Filter) Enter() { f.active = true }

// EnterFresh 进入输入态并清词（env/text/config 语义：`/` 重新开始）。
func (f *Filter) EnterFresh() { f.active, f.term = true, "" }

// Active 报告是否处于输入态。
func (f *Filter) Active() bool { return f.active }

// Term 返回当前过滤词。
func (f *Filter) Term() string { return f.term }

// Append 追加一个可打印字符。
func (f *Filter) Append(s string) { f.term += s }

// Backspace 回删一个 rune，报告是否删除了字符。
func (f *Filter) Backspace() bool {
	r := []rune(f.term)
	if len(r) == 0 {
		return false
	}
	f.term = string(r[:len(r)-1])
	return true
}

// Confirm 退出输入态，保留过滤词。
func (f *Filter) Confirm() { f.active = false }

// Clear 清词并退出输入态（恢复完整列表）。
func (f *Filter) Clear() { f.active, f.term = false, "" }

// Matches 报告标识符是否命中过滤词；空词恒命中。
func (f *Filter) Matches(identifier string) bool {
	return f.term == "" || matchKey(identifier, f.term)
}

// Prompt 渲染输入态回显（"/term_"）。
func (f *Filter) Prompt() string { return "/" + f.term + "_" }
