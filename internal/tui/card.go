package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	inner := width - 2
	contentW := maxInt(inner-2, 1)
	border := lipgloss.NewStyle().Foreground(colorDim)
	titleSt := lipgloss.NewStyle().Foreground(colorFg).Bold(true)
	fill := lipgloss.NewStyle().Background(colorSurface)

	titleText := " " + title + " "
	tw := lipgloss.Width(titleText)
	if tw > inner-1 {
		titleText = " " + truncateWidth(title, maxInt(inner-3, 1)) + " "
		tw = lipgloss.Width(titleText)
	}
	dashes := maxInt(inner-tw, 0)
	top := border.Render("╭") + titleSt.Render(titleText) + border.Render(strings.Repeat("─", dashes)+"╮")

	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	mids := make([]string, 0, len(bodyLines))
	for _, raw := range bodyLines {
		line := fitCardContent(raw, contentW)
		mids = append(mids, border.Render("│")+fill.Render(" ")+line+fill.Render(" ")+border.Render("│"))
	}
	bot := border.Render("╰" + strings.Repeat("─", inner) + "╯")
	out := make([]string, 0, 2+len(mids))
	out = append(out, top)
	out = append(out, mids...)
	out = append(out, bot)
	return strings.Join(out, "\n")
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
		s += lipgloss.NewStyle().Background(colorSurface).Render(strings.Repeat(" ", pad))
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
