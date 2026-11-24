package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/ledger"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
)

// ledgerTab 浏览台账：左栏为分组侧栏（含 "All" 伪组），右栏为实体头列表；
// `enter` 打开详情（字段 / 状态 / 时间线），`a` 追加不可变记录，`e` 改单字段，
// `s` 写最近状态。刻意不提供删除记录的入口——删除是显式 CLI 动作。
type ledgerTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	heads  []ledger.Head // 全部实体（跨分组）
	groups []string      // 已排序的分组名（不含 "All"）
	// groupIndex 0 = "All" 伪组；>0 指向 groups[index-1]。
	groupIndex int
	focusLeft  bool
	itemIndex  int

	filterBox Filter
	detail    *detailOverlay

	form       *form
	formSubmit func(values map[string]string) tea.Cmd
}

const ledgerAllLabel = "All"

type ledgerLoadedMsg struct {
	heads []ledger.Head
	err   error
}

// ledgerReloadMsg 请求在写操作后重载并显示一条提示；它同时是 writeDoneMsg，
// 触发底栏同步刷新与后台推送。
type ledgerReloadMsg struct{ toast string }

type ledgerDetailMsg struct {
	group   string
	key     string
	head    *ledger.Head
	records []ledger.RecordWithKey
	err     error
}

func newLedgerTab(mgr Managers) *ledgerTab {
	return &ledgerTab{mgr: mgr, focusLeft: true}
}

func (t *ledgerTab) Title() string { return "Ledger" }

func (t *ledgerTab) Bindings() []KeyAction {
	if t.detail != nil {
		return detailBindings()
	}
	if t.form != nil {
		return formBindings(KeyAction{[]string{"e"}, "edit multiline", grpForm, false})
	}
	if t.filterBox.Active() {
		return filterBindings(true)
	}
	if t.focusLeft {
		return append(navBindings(true), actFilter, actRefresh)
	}
	return append(navBindings(true),
		actDetail,
		KeyAction{[]string{"a"}, "append record", grpItem, false},
		KeyAction{[]string{"e"}, "edit field", grpItem, false},
		KeyAction{[]string{"s"}, "set status", grpItem, false},
		actEsc, actFilter, actRefresh,
	)
}

func (t *ledgerTab) InputMode() bool {
	return t.form != nil || t.detail != nil || t.filterBox.Active()
}

func (t *ledgerTab) SetSize(width, height int) {
	t.width, t.height = width, height
	if t.form != nil {
		t.form.SetSize(width, height)
	}
	if t.detail != nil {
		t.detail.SetSize(width, height)
	}
}

func (t *ledgerTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *ledgerTab) Reload() tea.Cmd { return t.load() }

func (t *ledgerTab) load() tea.Cmd {
	mgr := t.mgr.Ledger
	return func() tea.Msg {
		st := perflog.Start("tui.load-ledger")
		if mgr == nil {
			st.End(false)
			return ledgerLoadedMsg{err: fmt.Errorf("ledger manager unavailable")}
		}
		heads, err := mgr.ListHeads("")
		if err != nil {
			st.End(false)
			return ledgerLoadedMsg{err: err}
		}
		st.With("heads", len(heads)).End(true)
		return ledgerLoadedMsg{heads: heads}
	}
}

// --- derived lists ---

func (t *ledgerTab) currentGroup() string {
	if t.groupIndex <= 0 || t.groupIndex >= len(t.groups)+1 {
		return ""
	}
	return t.groups[t.groupIndex-1]
}

func (t *ledgerTab) baseHeads() []ledger.Head {
	if g := t.currentGroup(); g != "" {
		out := make([]ledger.Head, 0, len(t.heads))
		for _, h := range t.heads {
			if h.Group == g {
				out = append(out, h)
			}
		}
		return out
	}
	return t.heads
}

func (t *ledgerTab) filteredHeads() []ledger.Head {
	base := t.baseHeads()
	if t.filterBox.Term() == "" {
		return base
	}
	out := make([]ledger.Head, 0, len(base))
	for _, h := range base {
		key, _ := ledger.HeadKey(h.Type, h.ID)
		if t.filterBox.Matches(key) || t.filterBox.Matches(h.Description) ||
			t.filterBox.Matches(strings.Join(h.Tags, " ")) {
			out = append(out, h)
		}
	}
	return out
}

func (t *ledgerTab) currentHead() (ledger.Head, bool) {
	items := t.filteredHeads()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return ledger.Head{}, false
	}
	return items[t.itemIndex], true
}

func (t *ledgerTab) focusListLen() int {
	if t.focusLeft {
		return len(t.groups) + 1 // "All" + real groups
	}
	return len(t.filteredHeads())
}

func (t *ledgerTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups))
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(t.itemIndex+delta, 0, t.focusListLen()-1)
}

func (t *ledgerTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, len(t.groups))
		t.itemIndex = 0
		return
	}
	t.itemIndex = clamp(idx, 0, t.focusListLen()-1)
}

func (t *ledgerTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, len(t.groups))
	t.itemIndex = clamp(t.itemIndex, 0, maxInt(len(t.filteredHeads())-1, 0))
}

// focusJump 定位到某个实体（全局搜索跳转）。
func (t *ledgerTab) focusJump(group, key string) {
	t.filterBox.Clear()
	t.detail = nil
	t.focusLeft = false
	t.itemIndex = 0
	if group != "" {
		for i, g := range t.groups {
			if g == group {
				t.groupIndex = i + 1
				break
			}
		}
	}
	for i, h := range t.filteredHeads() {
		if hk, _ := ledger.HeadKey(h.Type, h.ID); hk == key {
			t.itemIndex = i
			return
		}
	}
}

// --- update ---

func (t *ledgerTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case formSubmitMsg:
		submit := t.formSubmit
		t.form = nil
		t.formSubmit = nil
		if submit == nil {
			return t, nil
		}
		return t, submit(msg.values)
	case formCancelMsg:
		t.form = nil
		t.formSubmit = nil
		return t, warnToast("cancelled")
	}
	if t.form != nil {
		next, cmd := t.form.Update(msg)
		t.form = next
		return t, cmd
	}

	switch msg := msg.(type) {
	case ledgerLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.heads = msg.heads
		t.groups = t.groupNames(msg.heads)
		t.loaded = true
		t.clampCursors()
		return t, nil

	case ledgerReloadMsg:
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd

	case ledgerDetailMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.detail = newDetailOverlay(msg.key, ledgerDetailLines(msg.head, msg.records))
		t.detail.SetSize(t.width, t.height)
		return t, nil

	case detailCloseMsg:
		t.detail = nil
		return t, nil

	case tea.KeyMsg:
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		if t.filterBox.Active() {
			return t.updateFilter(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

func (t *ledgerTab) groupNames(heads []ledger.Head) []string {
	seen := map[string]struct{}{}
	for _, h := range heads {
		if h.Group != "" {
			seen[h.Group] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

func (t *ledgerTab) updateFilter(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.filterBox.Clear()
		t.clampCursors()
	case "enter":
		t.filterBox.Confirm()
		t.clampCursors()
	case "backspace":
		if t.filterBox.Backspace() {
			t.clampCursors()
		}
	default:
		if r := msg.Runes; len(r) == 1 {
			t.filterBox.Append(string(r))
			t.clampCursors()
		}
	}
	return t, nil
}

func (t *ledgerTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		t.moveCursor(-1)
	case "down", "j":
		t.moveCursor(1)
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "g":
		t.jumpCursor(0)
	case "G":
		t.jumpCursor(t.focusListLen() - 1)
	case "pgup":
		t.jumpCursor(t.cursorForFocus() - pageStep(t.height))
	case "pgdown":
		t.jumpCursor(t.cursorForFocus() + pageStep(t.height))
	case "enter":
		return t.openDetail()
	case "a":
		if !t.focusLeft {
			return t.enterAppend()
		}
	case "e":
		if !t.focusLeft {
			return t.enterEditField()
		}
	case "s":
		if !t.focusLeft {
			return t.enterSetStatus()
		}
	case "/":
		t.filterBox.EnterFresh()
	}
	return t, nil
}

func (t *ledgerTab) cursorForFocus() int {
	if t.focusLeft {
		return t.groupIndex
	}
	return t.itemIndex
}

func (t *ledgerTab) openDetail() (Tab, tea.Cmd) {
	h, ok := t.currentHead()
	if !ok {
		return t, nil
	}
	mgr := t.mgr.Ledger
	group := h.Group
	key, err := ledger.HeadKey(h.Type, h.ID)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	return t, func() tea.Msg {
		head, err := mgr.GetHead(group, key)
		if err != nil {
			return ledgerDetailMsg{err: err}
		}
		recs, err := mgr.Log(group, key, 20)
		if err != nil {
			return ledgerDetailMsg{err: err}
		}
		return ledgerDetailMsg{group: group, key: key, head: head, records: recs}
	}
}

func (t *ledgerTab) enterAppend() (Tab, tea.Cmd) {
	h, ok := t.currentHead()
	if !ok {
		return t, warnToast("no entity selected")
	}
	key, _ := ledger.HeadKey(h.Type, h.ID)
	f := newForm("append ledger record · "+key,
		formField{key: "summary", label: "summary", kind: formText, placeholder: "one-line title",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("summary is required")
				}
				return nil
			}},
		formField{key: "kind", label: "kind", kind: formEnum, value: "op", options: []string{"op", "note", "correction"}},
		formField{key: "at", label: "date", kind: formText, placeholder: "YYYY-MM-DD (optional)"},
		formField{key: "agent", label: "agent", kind: formText, placeholder: "operator agent (optional)"},
		formField{key: "body", label: "body", kind: formEditor},
	)
	f.editExternal = func(index int, current string) tea.Cmd {
		return externalEditorCmd(index, "senv-ledger-*", current)
	}
	t.form = f
	t.form.SetSize(t.width, t.height)
	t.formSubmit = func(values map[string]string) tea.Cmd {
		return t.doAppend(h.Group, key, values)
	}
	return t, nil
}

func (t *ledgerTab) enterEditField() (Tab, tea.Cmd) {
	h, ok := t.currentHead()
	if !ok {
		return t, warnToast("no entity selected")
	}
	key, _ := ledger.HeadKey(h.Type, h.ID)
	f := newForm("edit field · "+key,
		formField{key: "name", label: "field name", kind: formText, placeholder: "e.g. location",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("field name is required")
				}
				return nil
			}},
		formField{key: "value", label: "value", kind: formText, placeholder: "empty deletes the field"},
	)
	t.form = f
	t.form.SetSize(t.width, t.height)
	t.formSubmit = func(values map[string]string) tea.Cmd {
		return t.doEditField(h.Group, key, strings.TrimSpace(values["name"]), values["value"])
	}
	return t, nil
}

func (t *ledgerTab) enterSetStatus() (Tab, tea.Cmd) {
	h, ok := t.currentHead()
	if !ok {
		return t, warnToast("no entity selected")
	}
	key, _ := ledger.HeadKey(h.Type, h.ID)
	f := newForm("set status · "+key,
		formField{key: "text", label: "status", kind: formText, placeholder: "e.g. online, disk 53%",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("status text is required")
				}
				return nil
			}},
		formField{key: "level", label: "level", kind: formEnum, value: "", options: []string{"ok", "warn", "critical"}, optional: true},
	)
	t.form = f
	t.form.SetSize(t.width, t.height)
	t.formSubmit = func(values map[string]string) tea.Cmd {
		return t.doSetStatus(h.Group, key, values["text"], values["level"])
	}
	return t, nil
}

// --- manager operations (executed in a command goroutine) ---

func ledgerBy(agent string) ledger.By {
	host, _ := os.Hostname()
	return ledger.By{Host: host, User: os.Getenv("USER"), Agent: agent}
}

func (t *ledgerTab) doAppend(group, key string, values map[string]string) tea.Cmd {
	mgr := t.mgr.Ledger
	mgrs := t.mgr
	in := ledger.RecordInput{
		Kind:    values["kind"],
		Summary: strings.TrimSpace(values["summary"]),
		Body:    values["body"],
		At:      parseLedgerDateInput(values["at"]),
		By:      ledgerBy(strings.TrimSpace(values["agent"])),
	}
	return func() tea.Msg {
		_, recKey, err := mgr.Append(group, key, in)
		if err != nil {
			recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+recKey, false, "append failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+recKey, true, "append")
		return ledgerReloadMsg{toast: "record appended"}
	}
}

func (t *ledgerTab) doEditField(group, key, name, value string) tea.Cmd {
	mgr := t.mgr.Ledger
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.EditField(group, key, name, value); err != nil {
			recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+key, false, "edit failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+key, true, "edit "+name)
		return ledgerReloadMsg{toast: "field saved"}
	}
}

func (t *ledgerTab) doSetStatus(group, key, text, level string) tea.Cmd {
	mgr := t.mgr.Ledger
	mgrs := t.mgr
	entry := ledger.StatusEntry{Text: strings.TrimSpace(text), Level: level, By: ledgerBy("")}
	return func() tea.Msg {
		if err := mgr.SetStatus(group, key, entry); err != nil {
			recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+key, false, "status failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpLedger, "ledger:"+group+":"+key, true, "status")
		return ledgerReloadMsg{toast: "status updated"}
	}
}

func parseLedgerDateInput(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

// --- view ---

func (t *ledgerTab) View() string {
	if t.detail != nil {
		return t.detail.View()
	}
	if t.loaded && len(t.heads) == 0 && t.form == nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("Ledger"),
			emptyStateStyle.Render("no ledger entities yet; create one with senv ledger create"))
	}
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *ledgerTab) viewBaseAt(height int) string {
	leftW := t.width / 5
	if leftW > 26 {
		leftW = 26
	}
	if leftW < 16 {
		leftW = 16
	}
	rightW := t.width - leftW - 5
	if rightW < 4 {
		rightW = 4
	}
	if !t.loaded {
		left := emptyStateStyle.Render("loading ledger…")
		right := emptyStateStyle.Render("loading ledger…")
		if t.focusLeft {
			left = applyPane(left, leftW, height, true)
			right = applyPane(right, rightW, height, false)
		} else {
			left = applyPane(left, leftW, height, false)
			right = applyPane(right, rightW, height, true)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
	left := t.renderGroups(leftW, height)
	right := t.renderHeads(rightW, height)
	if t.focusLeft {
		left = applyPane(left, leftW, height, true)
		right = applyPane(right, rightW, height, false)
	} else {
		left = applyPane(left, leftW, height, false)
		right = applyPane(right, rightW, height, true)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (t *ledgerTab) renderGroups(width, height int) string {
	rows := make([]SidebarRow, 0, len(t.groups)+1)
	counts := map[string]int{}
	for _, h := range t.heads {
		counts[h.Group]++
	}
	rows = append(rows, SidebarRow{
		Marker:   " ",
		Name:     ledgerAllLabel,
		Count:    len(t.heads),
		Selected: t.groupIndex == 0 && t.focusLeft,
	})
	for i, g := range t.groups {
		rows = append(rows, SidebarRow{
			Marker:   " ",
			Name:     g,
			Count:    counts[g],
			Selected: t.groupIndex == i+1 && t.focusLeft,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

func (t *ledgerTab) renderHeads(width, height int) string {
	items := t.filteredHeads()
	label := t.currentGroup()
	if label == "" {
		label = ledgerAllLabel
	}
	header := fmt.Sprintf("%s (%d)", label, len(items))
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	if len(items) == 0 {
		hint := "no entities"
		if t.filterBox.Term() != "" {
			hint = "no entities match /" + t.filterBox.Term()
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	typeW, idW := 10, 24
	descW := inner - 2 - typeW - idW - 2
	if descW < 8 {
		descW = 8
	}
	lines := make([]string, 0, len(items))
	for i, h := range items {
		key, _ := ledger.HeadKey(h.Type, h.ID)
		row := fmt.Sprintf("%s %s %s",
			padRight(truncateWidth(h.Type, typeW), typeW),
			padRight(truncateWidth(key, idW), idW),
			truncateWidth(h.Description, descW))
		selected := i == t.itemIndex
		if selected {
			lines = append(lines, selectedLineStyle.Render(cursorPrefix(true)+truncateWidth(row, inner-2)))
		} else {
			lines = append(lines, unselectedLineStyle.Render(cursorPrefix(false)+truncateWidth(row, inner-2)))
		}
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

// ledgerDetailLines 把实体头 + 记录渲染成详情弹层的文本行。
func ledgerDetailLines(h *ledger.Head, records []ledger.RecordWithKey) []string {
	var lines []string
	if h == nil {
		return lines
	}
	lines = append(lines, "group: "+h.Group)
	if h.Description != "" {
		lines = append(lines, "description: "+h.Description)
	}
	if len(h.Tags) > 0 {
		lines = append(lines, "tags: "+strings.Join(h.Tags, ", "))
	}
	lines = append(lines, "updated: "+h.UpdatedAt.Local().Format("2006-01-02 15:04"))
	if len(h.Fields) > 0 {
		lines = append(lines, "", "fields:")
		for _, f := range h.Fields {
			lines = append(lines, "  "+f.Name+": "+f.Value)
		}
	}
	if len(h.Status) > 0 {
		lines = append(lines, "", "status:")
		for _, s := range h.Status {
			suffix := ""
			if s.Level != "" {
				suffix = " [" + s.Level + "]"
			}
			lines = append(lines, "  "+s.At.Local().Format("2006-01-02 15:04")+suffix+"  "+s.Text)
		}
	}
	lines = append(lines, "", "records (newest first):")
	if len(records) == 0 {
		lines = append(lines, "  (none)")
	}
	for _, r := range records {
		head := fmt.Sprintf("  %s  [%s] %s", r.At.Local().Format("2006-01-02 15:04"), r.Kind, r.Summary)
		lines = append(lines, head)
		by := r.By
		if by.Host != "" || by.User != "" || by.Agent != "" {
			lines = append(lines, "    by "+formatLedgerBy(by))
		}
		for _, bodyLine := range strings.Split(strings.TrimRight(r.Body, "\n"), "\n") {
			if strings.TrimSpace(bodyLine) == "" {
				continue
			}
			lines = append(lines, "    "+bodyLine)
		}
	}
	return lines
}

func formatLedgerBy(by ledger.By) string {
	parts := make([]string, 0, 3)
	if by.Host != "" {
		parts = append(parts, by.Host)
	}
	if by.User != "" {
		parts = append(parts, by.User)
	}
	if by.Agent != "" {
		parts = append(parts, by.Agent)
	}
	return strings.Join(parts, " · ")
}

// Compile-time guard.
var _ Tab = (*ledgerTab)(nil)
