package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// 终端安全文本（TUI 渲染不变量的字符层）。
//
// bubbletea 的行差分渲染器假设：View 的每一逻辑行恰好占终端的一物理行，
// 光标落点与它用 ansi.StringWidth 算出的宽度一致。只要有一行在真实终端里
// 比渲染器算的更宽（自动折行）或带了会移动光标的控制字符，alt screen 就
// 会滚动一行；而渲染器只重画「与上一帧不同」的行，被滚走/错位的旧行永远
// 不会被修复——这就是「切 tab 后上一帧 tab 栏/表头残留、出现两条菜单栏」
// 的根因。触发物是记录内容：C1 控制字符（U+009B 即 8-bit CSI、U+0085
// NEL）、ESC 序列、制表符、以及「字素簇宽度」与「逐码点 wcwidth 宽度」不
// 一致的字符（国旗、ZWJ 家庭 emoji、VS16、肤色修饰……），不同终端按不同
// 宽度模型绘制它们。
//
// sanitizeLine 把任意一行收敛到「两种宽度模型都同意」的安全子集：
//   - 只保留 SGR（CSI … m）样式序列，其余 ESC/CSI/OSC/DCS 序列全部丢弃；
//   - C0/C1 控制字符丢弃（\t 变一个空格，\n/\r 及行/段分隔符变空格）；
//   - 零宽字素（孤立 ZWJ/VS/格式字符）丢弃；
//   - 字素簇宽度与 wcwidth 宽度不一致、或含 ZWJ/变体选择符的簇，退化为
//     首码点（首码点自身两种模型也一致时），否则用 "?" 顶替。
// 结果的显示宽度在 grapheme 与 wcwidth 两种模型下完全相同，因此截断/补齐
// 之后，任何主流终端都不会折行，也不会被内容移动光标。

// sanitizeLine 返回 s 的终端安全版本（保留 SGR 样式）。s 应为单行；若含
// 换行，换行会被替换为空格。
func sanitizeLine(s string) string {
	if isPlainASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		c := s[0]
		switch {
		case c == 0x1b:
			seq, n := scanEscape(s)
			if isSGR(seq) {
				b.WriteString(seq)
			}
			s = s[n:]
		case c < 0x80 && c >= 0x20 && c != 0x7f && (len(s) == 1 || s[1] < 0x80):
			// 快路径：可见 ASCII 且后面不跟组合字符。
			b.WriteByte(c)
			s = s[1:]
		case c < 0x20 || c == 0x7f:
			if c == '\t' || c == '\n' || c == '\r' {
				b.WriteByte(' ')
			}
			s = s[1:]
		default:
			cluster, gw := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
			if len(cluster) == 0 {
				// 非法 UTF-8 等：丢一个字节继续。
				s = s[1:]
				continue
			}
			s = s[len(cluster):]
			b.WriteString(safeCluster(cluster, gw))
		}
	}
	return b.String()
}

// isPlainASCII 报告 s 是否只含可见 ASCII（无 ESC/控制字符），可原样输出。
func isPlainASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// safeCluster 把单个字素簇映射到宽度在两种模型下一致的输出。
func safeCluster(cluster string, gw int) string {
	r, size := utf8.DecodeRuneInString(cluster)
	if r == utf8.RuneError && size <= 1 {
		return "?"
	}
	if size == len(cluster) {
		// 单码点。
		switch {
		case r >= 0x80 && r <= 0x9f: // C1 控制（含 8-bit CSI U+009B、NEL U+0085）
			return ""
		case r == 0x2028 || r == 0x2029: // 行/段分隔符：部分终端当换行
			return " "
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r):
			return ""
		case unicode.Is(unicode.Co, r):
			// 私用区（Nerd Font 图标）：字体常画成 2 列溢出，统一换成 1 列占位。
			return "?"
		}
		if gw <= 0 {
			return ""
		}
		if ansi.StringWidthWc(cluster) == gw {
			return cluster
		}
		return strings.Repeat("?", gw)
	}
	// 多码点簇：只放行「两种模型宽度一致、且只是基字符 + 组合附加符」的簇
	//（如分解形式的 é、多数印欧变音）；emoji 序列/国旗等退化为首码点。
	if gw > 0 && ansi.StringWidthWc(cluster) == gw && onlyCombining(cluster[size:]) {
		return cluster
	}
	base := string(r)
	if bw := ansi.StringWidth(base); bw > 0 && bw == ansi.StringWidthWc(base) &&
		!(r >= 0x80 && r <= 0x9f) && !unicode.Is(unicode.Co, r) &&
		!unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Cc, r) &&
		!isRegionalIndicator(r) {
		return base
	}
	if gw <= 0 {
		return ""
	}
	return "?"
}

// onlyCombining 报告 s 是否只由非零宽连接/非变体选择符的组合附加符构成。
func onlyCombining(s string) bool {
	for _, r := range s {
		if r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || r == 0x20e3 {
			return false
		}
		if !unicode.Is(unicode.Mn, r) && !unicode.Is(unicode.Me, r) {
			return false
		}
	}
	return true
}

func isRegionalIndicator(r rune) bool { return r >= 0x1f1e6 && r <= 0x1f1ff }

// scanEscape 从 s（以 ESC 开头）读出一条完整的转义序列，返回序列与消耗
// 的字节数。不完整的序列吞到串尾（宁可少显示，也不把半截序列交给终端）。
func scanEscape(s string) (string, int) {
	if len(s) < 2 {
		return s, len(s)
	}
	switch s[1] {
	case '[': // CSI：参数字节 0x30–0x3F、中间字节 0x20–0x2F、终止字节 0x40–0x7E
		for i := 2; i < len(s); i++ {
			c := s[i]
			if c >= 0x40 && c <= 0x7e {
				return s[:i+1], i + 1
			}
			if c < 0x20 || c > 0x7e {
				// 非法字节：序列在此中断（该字节留给后续处理）。
				return s[:i], i
			}
		}
		return s, len(s)
	case ']', 'P', 'X', '^', '_': // OSC / DCS / SOS / PM / APC：到 BEL 或 ST 为止
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return s[:i+1], i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return s[:i+2], i + 2
			}
		}
		return s, len(s)
	default: // 两字节 ESC 序列（含中间字节时多吞几个）
		i := 1
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			i++
		}
		return s[:i], i
	}
}

// isSGR 报告 seq 是否为纯样式序列 CSI [0-9;:]* m（无私有前缀）。
func isSGR(seq string) bool {
	if len(seq) < 3 || seq[0] != 0x1b || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return false
	}
	for i := 2; i < len(seq)-1; i++ {
		c := seq[i]
		if !(c >= '0' && c <= '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}

// plainText 把来自记录/用户的原始文本收敛成单行纯文本：去掉全部转义序列
// 与控制字符，换行/制表折叠为单个空格，并做宽度安全化。用于把数据字段
// （值预览、条目名、审计目标、错误信息……）拼进列表行之前。
func plainText(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == 0x2028 || r == 0x2029 {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return sanitizeLine(b.String())
}

// fitLine 把一行（可含 SGR）安全化并截断/右补齐到恰好 w 显示列。
func fitLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = sanitizeLine(s)
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	if strings.IndexByte(s, 0x1b) >= 0 {
		// 截断可能留下未闭合的样式：收尾复位，避免染色补齐的空白与下一区块。
		s += "\x1b[0m"
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// fitBlock 把多行文本收敛为恰好 w 列 × h 行的矩形：每行 fitLine，多余行
// 裁掉，不足行补空白行。它是布局层给每个区域（tab 栏、内容区、底栏）
// 施加的硬边界——无论区域内部渲染出什么，都不可能撑高或撑宽整帧。
func fitBlock(s string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	var lines []string
	if s != "" {
		lines = strings.Split(s, "\n")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fitLine(l, w)
	}
	return out
}
