package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// cardKind 给 infoCard 的值上色：对应参考图里 Status 用绿色、时间用青色。
type cardKind int

const (
	cardPlain cardKind = iota
	cardOK
	cardCyan
	cardDanger
	cardWarn
)

// cardRow 是信息卡里的一行「标签 : 值」。Label 与 Value 都为空时画空行。
type cardRow struct {
	Label string
	Value string
	Kind  cardKind
}

// infoCard 画一张圆角信息卡：标题嵌在上边框，内容是左对齐标签、冒号槽、
// 带语义色的值，样式对齐参考图中的 Status / Last sync 卡片。
func infoCard(title string, rows []cardRow, width int) string {
	if width < 8 {
		width = 8
	}
	inner := width - 2
	contentW := maxInt(inner-2, 1)
	labelW := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.Label); w > labelW {
			labelW = w
		}
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Label == "" && r.Value == "" {
			lines = append(lines, "")
			continue
		}
		label := r.Label
		if pad := labelW - lipgloss.Width(label); pad > 0 {
			label += strings.Repeat(" ", pad)
		}
		lines = append(lines, formatCardLine(label, r.Value, r.Kind, contentW))
	}
	return infoCardBlock(title, strings.Join(lines, "\n"), width)
}

// infoCardBlock 用同一套边框把已经排好的正文（可含样式）装进信息卡。
func infoCardBlock(title string, body string, width int) string {
	if width < 8 {
		width = 8
	}
	return boxWithTitle(title, body, width-2, -1, false)
}

// applyPane 把窗口化列表（windowedPane 的输出）装进标题嵌在上边框的
// 圆角卡片，尺寸约定与原先 paneStyle.Width(w).Height(h) 一致：内宽 w、
// 内高 h，返回块为 (w+2)×(h+2)。仅当首行是 paneTitleStyle 画出的标题
// 时才抬到边框上，空状态等没有标题的正文保持无标题卡片。
func applyPane(content string, width, height int, active bool) string {
	title, body := splitPaneTitle(content)
	return boxWithTitle(title, body, width, height, active)
}

// boxWithTitle 画一张标题嵌在上边框的圆角卡。width/height 是边框以内的
// 内容尺寸；height < 0 时按正文行数收缩。焦点面板的边框用强调色。
func boxWithTitle(title, body string, width, height int, active bool) string {
	if width < 4 {
		width = 4
	}
	contentW := maxInt(width-2, 1)
	borderFg := colorDim
	if active {
		borderFg = colorAccent
	}
	border := lipgloss.NewStyle().Foreground(borderFg).Background(colorSurface)
	titleSt := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Bold(true)
	fill := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface)

	titleText := ""
	tw := 0
	if strings.TrimSpace(title) != "" {
		titleText = " " + title + " "
		tw = lipgloss.Width(titleText)
		if tw > width-1 {
			titleText = " " + truncateWidth(title, maxInt(width-3, 1)) + " "
			tw = lipgloss.Width(titleText)
		}
	}
	dashes := maxInt(width-tw, 0)
	top := border.Render("╭") + titleSt.Render(titleText) + border.Render(strings.Repeat("─", dashes)+"╮")

	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	if height >= 0 {
		if len(bodyLines) > height {
			bodyLines = bodyLines[:height]
		}
		for len(bodyLines) < height {
			bodyLines = append(bodyLines, "")
		}
	}
	mids := make([]string, 0, len(bodyLines))
	for _, raw := range bodyLines {
		line := fitCardContent(raw, contentW)
		mids = append(mids, border.Render("│")+fill.Render(" ")+line+fill.Render(" ")+border.Render("│"))
	}
	bot := border.Render("╰" + strings.Repeat("─", width) + "╯")
	out := make([]string, 0, 2+len(mids))
	out = append(out, top)
	out = append(out, mids...)
	out = append(out, bot)
	return strings.Join(out, "\n")
}

func splitPaneTitle(content string) (title, body string) {
	if content == "" {
		return "", ""
	}
	lines := strings.Split(content, "\n")
	if isPaneTitleLine(lines[0]) {
		title = strings.TrimSpace(ansi.Strip(lines[0]))
		return title, strings.Join(lines[1:], "\n")
	}
	return "", content
}

// isPaneTitleLine 判断一行是不是 paneTitleStyle 渲染出的标题（用该样式
// 的开启 SGR 做标记），避免把「loading groups…」这类空状态抬进边框。
func isPaneTitleLine(s string) bool {
	sample := paneTitleStyle.Render("X")
	i := strings.Index(sample, "X")
	if i <= 0 {
		return false
	}
	return strings.Contains(s, sample[:i])
}

func formatCardLine(label, value string, kind cardKind, width int) string {
	lab := lipgloss.NewStyle().Foreground(colorMuted).Background(colorSurface).Render(label)
	colon := lipgloss.NewStyle().Foreground(colorDim).Background(colorSurface).Render(" : ")
	vs := lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface)
	switch kind {
	case cardOK:
		vs = vs.Foreground(colorOk)
	case cardCyan:
		vs = vs.Foreground(colorCyan)
	case cardDanger:
		vs = vs.Foreground(colorDanger)
	case cardWarn:
		vs = vs.Foreground(colorWarn)
	}
	val := vs.Render(value)
	return fitCardContent(lab+colon+val, width)
}

func fitCardContent(s string, width int) string {
	s = truncateWidth(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += lipgloss.NewStyle().Foreground(colorFg).Background(colorSurface).Render(strings.Repeat(" ", pad))
	}
	return s
}

func valueKindStyle(kind cardKind) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(colorFg)
	switch kind {
	case cardOK:
		st = st.Foreground(colorOk)
	case cardCyan:
		st = st.Foreground(colorCyan)
	case cardDanger:
		st = st.Foreground(colorDanger)
	case cardWarn:
		st = st.Foreground(colorWarn)
	}
	return st
}
