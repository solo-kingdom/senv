package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// textTab renders Text data: left column = groups, right column = text block
// metadata (key/size/updated). Content is never shown in the list; it is only
// visible via vim editing or detail view.
type textTab struct {
	mgr Managers
	// form 非 nil 时表示打开了一个结构化表单（重命名/导入等）。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd
	// focusAfterLoad 是下一次加载后要定位到的 key（重命名后停在新名字上）。
	focusAfterLoad string
	width, height  int

	groups       []textGroupRow
	itemsByGroup map[string][]textItemRow
	groupIndex   int
	loaded       bool

	itemIndex int
	focusLeft bool

	deref     bool
	filterBox Filter
	// sel 仅承载多选集（游标仍由 groupIndex/itemIndex 承载，随侧栏改造统一）。
	sel List

	input textinput.Model
	mode  textMode
}

type textGroupRow struct {
	name     string
	isAll    bool
	keyCount int
}

type textItemRow struct {
	group     string
	key       string
	size      int
	updatedAt string
}

type textMode int

const (
	textModeNormal textMode = iota
	textModeDeleteConfirm
	textModeExportPath
	textModeNewKey
	textModeAddGroup
	textModeFilter
	textModeBatchDeleteConfirm
	textModeBatchExportPath
	textModeDeleteGroupConfirm
)

func newTextTab(mgr Managers) *textTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &textTab{mgr: mgr, focusLeft: true, input: ti}
}

func (t *textTab) Title() string { return "Text" }

func (t *textTab) Bindings() []KeyAction {
	return []KeyAction{
		actUp, actDown, actLeft, actRight,
		actTop, actBottom, actPageUp, actPageDn,
		actEdit, actNew, actDelete, actRename, actImport,
		actSelect, actSelectAll,
		{[]string{"y"}, "copy", grpItem},
		actExport,
		{[]string{"+"}, "new group", grpGroup},
		{[]string{"D"}, "deref on/off", grpGroup},
		actFilter, actRefresh,
	}
}

// cursorForFocus 返回当前焦点栏的游标位置（翻页用）。
func (t *textTab) cursorForFocus() int {
	if t.focusLeft {
		return t.groupIndex
	}
	return t.itemIndex
}

func (t *textTab) InputMode() bool {
	if t.form != nil {
		return true
	}
	switch t.mode {
	case textModeFilter, textModeExportPath, textModeNewKey, textModeAddGroup,
		textModeDeleteGroupConfirm, textModeBatchDeleteConfirm, textModeBatchExportPath:
		return true
	}
	return false
}

// --- data loading ---

type textLoadedMsg struct {
	groups       []textGroupRow
	itemsByGroup map[string][]textItemRow
	err          error
}

type textReloadMsg struct {
	toast string // 成功提示：非空时随 reload 一并显示
	warn  string // 部分失败提示：非空时随 reload 一并显示
}

func (t *textTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *textTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}

func (t *textTab) load() tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		st := perflog.Start("tui.load-text")
		if mgr == nil {
			st.End(false)
			return textLoadedMsg{err: fmt.Errorf("text manager unavailable")}
		}
		// 单趟快照：分组与全部条目一次批量装载（一次 vault 读锁），并与进程内
		// memo 共享；写操作/pull 应用后由 model 层 Invalidate。
		snap, err := textSnapshot(t.mgr)
		if err != nil {
			st.End(false)
			return textLoadedMsg{err: err}
		}
		groups := make([]textGroupRow, 0, len(snap.Groups))
		itemsByGroup := make(map[string][]textItemRow, len(snap.Groups))
		totalKeys := 0
		for _, g := range snap.Groups {
			// 侧栏范式：空分组也显示（计数 0），与过滤期行为一致
			groups = append(groups, textGroupRow{name: g.Name, keyCount: g.KeyCount})
			itemsByGroup[g.Name] = buildTextItems(g.Name, snap.Items[g.Name])
			totalKeys += len(itemsByGroup[g.Name])
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].name == "default" {
				return true
			}
			if groups[j].name == "default" {
				return false
			}
			return groups[i].name < groups[j].name
		})
		// All 伪组置顶（默认选中）：聚合全部条目（grill D3 侧栏范式）
		groups = append([]textGroupRow{{name: textAllLabel, isAll: true, keyCount: totalKeys}}, groups...)
		st.With("groups", len(groups)).End(true)
		return textLoadedMsg{groups: groups, itemsByGroup: itemsByGroup}
	}
}

func buildTextItems(group string, infos []text.TextInfo) []textItemRow {
	out := make([]textItemRow, 0, len(infos))
	for _, ti := range infos {
		out = append(out, textItemRow{
			group:     group,
			key:       ti.Key,
			size:      ti.Size,
			updatedAt: ti.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// textAllLabel 是 text 侧栏顶部的 All 伪组标签。
const textAllLabel = "All"

func (t *textTab) currentGroup() string {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return ""
	}
	return t.groups[t.groupIndex].name
}

func (t *textTab) currentGroupRow() (textGroupRow, bool) {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return textGroupRow{}, false
	}
	return t.groups[t.groupIndex], true
}

// realGroup 返回当前选中的真实分组名；All 伪组返回 ""。
func (t *textTab) realGroup() string {
	if row, ok := t.currentGroupRow(); ok && row.isAll {
		return ""
	}
	return t.currentGroup()
}

// focusGroup 返回条目操作应使用的分组名（All 视图取条目自带分组）。
func (t *textTab) focusGroup(it textItemRow) string {
	if it.group != "" {
		return it.group
	}
	return t.currentGroup()
}

func (t *textTab) filteredItems() []textItemRow {
	var all []textItemRow
	if gr, ok := t.currentGroupRow(); ok && gr.isAll {
		for _, rows := range t.itemsByGroup {
			all = append(all, rows...)
		}
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].group != all[j].group {
				return all[i].group < all[j].group
			}
			return all[i].key < all[j].key
		})
	} else {
		all = t.itemsByGroup[t.currentGroup()]
	}
	if t.filterBox.Term() == "" {
		return all
	}
	out := make([]textItemRow, 0, len(all))
	for _, it := range all {
		if t.filterBox.Matches(it.key) {
			out = append(out, it)
		}
	}
	return out
}

func (t *textTab) currentItem() (textItemRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return textItemRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *textTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	// Form results are handled before routing further messages into the open form.
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
	case renameDoneMsg:
		t.focusAfterLoad = msg.key
		return t, tea.Batch(okToast(msg.text), t.load())

	case textLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.groups = msg.groups
		t.itemsByGroup = msg.itemsByGroup
		t.loaded = true
		t.clampCursors()
		if key := t.focusAfterLoad; key != "" {
			t.focusAfterLoad = ""
			for i, it := range t.itemsByGroup[t.currentGroup()] {
				if it.key == key {
					t.itemIndex = i
					t.focusLeft = false
					break
				}
			}
		}
		return t, nil

	case textReloadMsg:
		switch {
		case msg.toast != "":
			return t, tea.Batch(okToast(msg.toast), t.load())
		case msg.warn != "":
			return t, tea.Batch(warnToast(msg.warn), t.load())
		}
		return t, t.load()

	case tea.KeyMsg:
		if t.mode != textModeNormal {
			return t.handleModalKey(msg)
		}

		switch msg.String() {
		case "up", "k":
			t.moveCursor(-1)
		case "down", "j":
			t.moveCursor(1)
		case "left", "h":
			t.focusLeft = true
		case "right", "l":
			t.focusLeft = false
			t.itemIndex = 0 // 切到条目栏定位该分组第一条（侧栏范式）
		case "g":
			t.jumpCursor(0)
		case "G":
			t.jumpCursor(len(t.listForFocus()) - 1)
		case "pgup":
			t.jumpCursor(t.cursorForFocus() - pageStep(t.height))
		case "pgdown":
			t.jumpCursor(t.cursorForFocus() + pageStep(t.height))
		case " ", "space":
			if !t.focusLeft {
				if key := t.selectionKey(); key != "" {
					t.sel.Toggle(key)
				}
			}
		case "a":
			if !t.focusLeft {
				keys := make([]string, 0, len(t.filteredItems()))
				for _, it := range t.filteredItems() {
					keys = append(keys, it.group+"/"+it.key)
				}
				t.sel.SelectVisible(keys)
			}
		case "e":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to edit")
			}
			if t.sel.SelectionCount() == 1 && !t.sel.IsSelected(t.selectionKey()) {
				return t, warnToast("selected entry differs from cursor: clear the selection or align the cursor to edit")
			}
			return t.editCurrent()
		case "n":
			return t.enterNewKeyMode()
		case "d":
			if t.focusLeft {
				return t.enterDeleteGroupConfirm()
			}
			// 批量安全动词作用于选择集（spec：选择集非空即批量，含 1 条）；
			// 空选择集回落游标单条。
			if t.sel.SelectionCount() > 0 {
				return t.enterBatchDeleteConfirm()
			}
			return t.enterDeleteConfirm()
		case "r":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to rename")
			}
			return t.enterRenameMode()
		case "i":
			return t.enterImportMode()
		case "y":
			return t, t.doCopy()
		case "x":
			if t.sel.SelectionCount() > 1 {
				return t.enterBatchExportPath()
			}
			return t.enterExportMode()
		case "+":
			return t.enterAddGroupMode()
		case "D":
			// The text list shows metadata only (no content), so dereference has
			// no visual effect on the list; it would apply to a detail/export view.
			t.deref = !t.deref
			return t, okToast("dereference view: " + onOff(t.deref) + " (list shows metadata only)")
		case "/":
			return t.enterFilterMode()
		}
	}
	return t, nil
}

// --- navigation ---

func (t *textTab) listForFocus() []string {
	if t.focusLeft {
		out := make([]string, len(t.groups))
		for i, g := range t.groups {
			out[i] = g.name
		}
		return out
	}
	items := t.filteredItems()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.key
	}
	return out
}

func (t *textTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(t.itemIndex+delta, 0, len(items)-1)
	}
}

func (t *textTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, len(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(idx, 0, len(items)-1)
	}
}

func (t *textTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, len(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, len(t.filteredItems())-1)
}

// focusJump positions the cursor at (group, key) for search-result navigation.
func (t *textTab) focusJump(group, key string) {
	t.filterBox.Clear()
	t.mode = textModeNormal
	for i, g := range t.groups {
		if g.name == group {
			t.groupIndex = i
			break
		}
	}
	items := t.itemsByGroup[t.currentGroup()]
	for i, it := range items {
		if it.key == key {
			t.itemIndex = i
			break
		}
	}
	t.focusLeft = false
}

// --- modal handling ---

func (t *textTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if t.mode == textModeBatchDeleteConfirm {
		targets := t.selectedTargets()
		switch msg.String() {
		case "enter", "y":
			t.mode = textModeNormal
			t.sel.ClearSelection()
			return t, t.doBatchDelete(targets)
		case "esc", "n":
			t.mode = textModeNormal
			return t, nil
		}
		return t, nil
	}
	if t.mode == textModeDeleteGroupConfirm {
		name := t.currentGroup()
		t.mode = textModeNormal
		switch msg.String() {
		case "enter", "y":
			return t, t.doDeleteGroup(name)
		default:
			return t, nil
		}
	}
	if t.mode == textModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = textModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(t.focusGroup(it), it.key)
		default:
			t.mode = textModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == textModeFilter {
			t.filterBox.Clear() // esc 清词并退出
		}
		t.mode = textModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	if t.mode == textModeFilter {
		switch msg.String() {
		case "backspace":
			if t.filterBox.Backspace() {
				t.itemIndex = 0
			}
			return t, nil
		}
		if isPrintable(msg) {
			t.filterBox.Append(msg.String())
			t.itemIndex = 0
		}
		return t, nil
	}

	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

func (t *textTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case textModeBatchExportPath:
		dir := strings.TrimSpace(t.input.Value())
		targets := t.selectedTargets()
		t.mode = textModeNormal
		t.input.Blur()
		if dir == "" || len(targets) == 0 {
			return t, warnToast("export cancelled")
		}
		t.sel.ClearSelection() // 提交即清空多选集
		return t, t.doBatchExport(dir, targets)
	case textModeExportPath:
		path := t.input.Value()
		it, ok := t.currentItem()
		t.mode = textModeNormal
		t.input.Blur()
		if !ok || path == "" {
			return t, warnToast("export cancelled")
		}
		return t, t.doExport(t.focusGroup(it), it.key, path)
	case textModeNewKey:
		group, key := parseKeyAddress(t.input.Value(), t.realGroup())
		t.mode = textModeNormal
		t.input.Blur()
		if key == "" {
			return t, warnToast("key cannot be empty")
		}
		if group == "" {
			return t, warnToast("select a group first, or use group:key form")
		}
		return t.editKey(group, key)
	case textModeAddGroup:
		name := t.input.Value()
		t.mode = textModeNormal
		t.input.Blur()
		if name == "" {
			return t, warnToast("group name cannot be empty")
		}
		return t, t.doAddGroup(name)
	}
	t.mode = textModeNormal
	return t, nil
}

// --- modal entry points ---

func (t *textTab) enterNewKeyMode() (Tab, tea.Cmd) {
	t.mode = textModeNewKey
	t.input.SetValue("")
	t.input.Placeholder = "key or group:key"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("no entry to delete")
	}
	t.mode = textModeDeleteConfirm
	return t, nil
}

// selectionKey 返回当前文本块的稳定选择标识（group/key）。
func (t *textTab) selectionKey() string {
	if it, ok := t.currentItem(); ok {
		return it.group + "/" + it.key
	}
	return ""
}

// enterBatchDeleteConfirm 多选集批量删除：一次确认列全部目标。
func (t *textTab) enterBatchDeleteConfirm() (Tab, tea.Cmd) {
	if len(t.selectedTargets()) == 0 {
		return t, warnToast("selection is empty or has no entries in the current group")
	}
	t.mode = textModeBatchDeleteConfirm
	return t, nil
}

// allItems 返回全部文本块（跨分组聚合、稳定排序），不随过滤收窄。
// 批量动作的目标解析以此为准：选择集跨过滤持久，被过滤隐藏的已选项
// MUST 保持在批量目标内（tui-viewer 多选语义）。
func (t *textTab) allItems() []textItemRow {
	var all []textItemRow
	for _, rows := range t.itemsByGroup {
		all = append(all, rows...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].group != all[j].group {
			return all[i].group < all[j].group
		}
		return all[i].key < all[j].key
	})
	return all
}

// selectedTargets 返回多选集命中的全部条目（group, key）——含被过滤隐藏的
// 已选项；空集回落游标的判定由调用方负责。
func (t *textTab) selectedTargets() [][2]string {
	var out [][2]string
	for _, it := range t.allItems() {
		k := it.group + "/" + it.key
		if t.sel.IsSelected(k) {
			out = append(out, [2]string{it.group, it.key})
		}
	}
	return out
}

// enterBatchExportPath 批量导出：选一个目录，每块写入 <目录>/<key>.txt。
func (t *textTab) enterBatchExportPath() (Tab, tea.Cmd) {
	if len(t.selectedTargets()) == 0 {
		return t, warnToast("selection is empty or has no entries in the current group")
	}
	t.mode = textModeBatchExportPath
	t.input.SetValue("")
	t.input.Placeholder = "output dir (each block written to <dir>/<key>.txt)"
	t.input.Focus()
	return t, textinput.Blink
}

// doBatchDelete 逐条删除选择集；单条失败不中止其余。每条写操作逐条审计
// （operation-audit：TUI 写操作与 CLI 同类操作记录同一类事件）；结束后随
// reload 一并提示结果（与单条 doDelete 收尾一致，列表不再滞留已删条目）。
func (t *textTab) doBatchDelete(targets [][2]string) tea.Cmd {
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		failed := 0
		for _, tgt := range targets {
			if err := mgr.Delete(tgt[0], tgt[1]); err != nil {
				recordAudit(mgrs, session.AuditOpText, textTarget(tgt[0], tgt[1]), false, "batch delete failed")
				failed++
				continue
			}
			recordAudit(mgrs, session.AuditOpText, textTarget(tgt[0], tgt[1]), true, "batch delete")
		}
		if failed > 0 {
			return textReloadMsg{warn: fmt.Sprintf("batch delete finished, %d failed", failed)}
		}
		return textReloadMsg{toast: fmt.Sprintf("deleted %d entries", len(targets))}
	}
}

// doBatchExport 逐块导出到 dir/<key>.txt；单条失败不中止其余。目标文件名
// 来自用户数据（key），写入前必须复验路径段（securefs 约束）。
func (t *textTab) doBatchExport(dir string, targets [][2]string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		failed := 0
		for _, tgt := range targets {
			if err := securefs.ValidateSegment(tgt[1]); err != nil {
				failed++
				continue
			}
			path := filepath.Join(dir, tgt[1]+".txt")
			if err := mgr.GetToFile(tgt[0], tgt[1], path); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("batch export finished, %d failed", failed)}
		}
		return okToast(fmt.Sprintf("exported %d entries to %s", len(targets), dir))
	}
}

func (t *textTab) enterExportMode() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("no entry to export")
	}
	t.mode = textModeExportPath
	t.input.SetValue("")
	t.input.Placeholder = "output file path"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterAddGroupMode() (Tab, tea.Cmd) {
	t.mode = textModeAddGroup
	t.input.SetValue("")
	t.input.Placeholder = "group name"
	t.input.Focus()
	return t, textinput.Blink
}

// openForm installs a structured form and the action to run on submit.
func (t *textTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// defaultTextGroup is the group the CLI and {{text:default:key}} references
// resolve to; it is the stable landing point of this tab, so it cannot be
// renamed or deleted from the TUI.
const defaultTextGroup = "default"

func (t *textTab) enterDeleteGroupConfirm() (Tab, tea.Cmd) {
	group := t.realGroup()
	if group == "" {
		return t, warnToast("no group to delete")
	}
	if group == defaultTextGroup {
		return t, warnToast("default group cannot be deleted")
	}
	t.mode = textModeDeleteGroupConfirm
	return t, nil
}

// enterRenameMode renames whatever the focused pane shows: the group (left) or
// the text key (right).
func (t *textTab) enterRenameMode() (Tab, tea.Cmd) {
	if t.focusLeft {
		if row, ok := t.currentGroupRow(); ok && row.isAll {
			return t, warnToast("All cannot be renamed, pick a specific group")
		}
		group := t.currentGroup()
		if group == "" {
			return t, warnToast("no group to rename")
		}
		if group == defaultTextGroup {
			return t, warnToast("default group cannot be renamed")
		}
		siblings := make(map[string]bool, len(t.groups))
		for _, g := range t.groups {
			siblings[g.name] = true
		}
		old := group
		f := newForm("rename group",
			formField{key: "name", label: "new name", kind: formText, value: old, placeholder: "new-group-name",
				validate: func(v string) error {
					v = strings.TrimSpace(v)
					if v == "" {
						return fmt.Errorf("group name cannot be empty")
					}
					if err := storage.ValidateName(v); err != nil {
						return fmt.Errorf("invalid group name")
					}
					if v != old && siblings[v] {
						return fmt.Errorf("group %s already exists", v)
					}
					return nil
				}})
		t.openForm(f, func(values map[string]string) tea.Cmd {
			return t.doRenameGroup(old, strings.TrimSpace(values["name"]))
		})
		return t, nil
	}

	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to rename")
	}
	group := t.focusGroup(it)
	siblings := make(map[string]bool)
	for _, row := range t.itemsByGroup[group] {
		siblings[row.key] = true
	}
	old := it.key
	f := newForm("rename text block",
		formField{key: "key", label: "new key", kind: formText, value: old, placeholder: "new-key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid key")
				}
				if v != old && siblings[v] {
					return fmt.Errorf("key %s already exists in %s", v, group)
				}
				return nil
			}})
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doRenameKey(group, old, strings.TrimSpace(values["key"]))
	})
	return t, nil
}

// enterImportMode opens the file-import form (group + key + path).
func (t *textTab) enterImportMode() (Tab, tea.Cmd) {
	group := t.realGroup() // All 视图回落 default
	if group == "" {
		group = defaultTextGroup
	}
	f := newForm("import text block from file",
		formField{key: "group", label: "group", kind: formText, value: group, placeholder: "group",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("group cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid group name")
				}
				return nil
			}},
		formField{key: "key", label: "key", kind: formText, placeholder: "key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid key")
				}
				return nil
			}},
		formField{key: "path", label: "source file path", kind: formPath, placeholder: "/path/to/file"},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doImport(strings.TrimSpace(values["group"]), strings.TrimSpace(values["key"]), strings.TrimSpace(values["path"]))
	})
	return t, nil
}

func (t *textTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = textModeFilter
	t.filterBox.EnterFresh() // text 语义：`/` 清词重新开始
	return t, nil
}

// --- vim editing (task 8.2): PrepareEditor -> tea.ExecProcess -> FinishEditor ---

// editCurrent opens the selected text block in vim via tea.ExecProcess.
func (t *textTab) editCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to edit")
	}
	return t.editKey(t.focusGroup(it), it.key)
}

// editKey prepares an editor session for (group,key) and suspends the TUI.
func (t *textTab) editKey(group, key string) (Tab, tea.Cmd) {
	mgr := t.mgr.Text
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("text manager unavailable")} }
	}
	session, err := mgr.PrepareEditor(group, key)
	if err != nil {
		err := err
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	return t, tea.ExecProcess(session.EditorCommand(), func(runErr error) tea.Msg {
		return t.finishAfterEdit(session, runErr)
	})
}

// finishAfterEdit is the post-editor callback: on editor failure it cleans up
// the temp file and reports an error without persisting; otherwise it commits
// the (possibly unchanged) edit. Extracted so task 11.3 (editor failure) is
// unit-testable without a real TTY/editor.
func (t *textTab) finishAfterEdit(es *text.EditorSession, runErr error) tea.Msg {
	if runErr != nil {
		// Editor failed/absent: clean up the temp file, do not persist.
		os.Remove(es.TmpPath)
		recordAudit(t.mgr, session.AuditOpText, textTarget(es.Group, es.Key), false, "edit editor failed")
		return errMsg{err: fmt.Errorf("editor failed: %w", runErr)}
	}
	if _, ferr := t.mgr.Text.FinishEditor(es); ferr != nil {
		recordAudit(t.mgr, session.AuditOpText, textTarget(es.Group, es.Key), false, "edit failed")
		return errMsg{err: ferr}
	}
	recordAudit(t.mgr, session.AuditOpText, textTarget(es.Group, es.Key), true, "edit")
	return textReloadMsg{}
}

// --- manager operations ---

func (t *textTab) doDelete(group, key string) tea.Cmd {
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Delete(group, key); err != nil {
			recordAudit(mgrs, session.AuditOpText, textTarget(group, key), false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, key), true, "delete")
		return textReloadMsg{}
	}
}

// doRenameKey atomically renames a text block (content untouched).
func (t *textTab) doRenameKey(group, oldKey, newKey string) tea.Cmd {
	if oldKey == newKey {
		return warnToast("key unchanged")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameKey(group, oldKey, newKey); err != nil {
			recordAudit(mgrs, session.AuditOpText, textTarget(group, oldKey), false, "rename failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, newKey), true, "rename "+oldKey)
		return renameDoneMsg{group: group, key: newKey, text: "renamed to " + newKey}
	}
}

// doRenameGroup renames a text group and keeps every block inside it.
func (t *textTab) doRenameGroup(oldName, newName string) tea.Cmd {
	if oldName == newName {
		return warnToast("group name unchanged")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameGroup(oldName, newName); err != nil {
			recordAudit(mgrs, session.AuditOpText, "text:group:"+oldName, false, "rename group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, "text:group:"+newName, true, "rename group "+oldName)
		return textReloadMsg{}
	}
}

// doDeleteGroup deletes a text group and all of its blocks.
func (t *textTab) doDeleteGroup(name string) tea.Cmd {
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.DeleteGroup(name); err != nil {
			recordAudit(mgrs, session.AuditOpText, "text:group:"+name, false, "delete group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, "text:group:"+name, true, "delete group")
		return textReloadMsg{}
	}
}

// doImport encrypts a local file into a text block (SetFromFile); the source
// file itself is left untouched.
func (t *textTab) doImport(group, key, path string) tea.Cmd {
	if path == "" {
		return warnToast("source file path cannot be empty")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.SetFromFile(group, key, path); err != nil {
			recordAudit(mgrs, session.AuditOpText, textTarget(group, key), false, "import failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, key), true, "import "+path)
		return renameDoneMsg{group: group, key: key, text: "imported " + key}
	}
}

func (t *textTab) doCopy() tea.Cmd {
	it, ok := t.currentItem()
	if !ok {
		return warnToast("nothing to copy")
	}
	mgr := t.mgr.Text
	// 条目真实分组：All 伪组视图下 currentGroup() 是 "All"，不是存储分组。
	group := it.group
	key := it.key
	return func() tea.Msg {
		if err := mgr.GetToClipboard(group, key); err != nil {
			return errMsg{err: err}
		}
		return toastMsg{text: "copied " + key, level: toastSuccess}
	}
}

func (t *textTab) doExport(group, key, path string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		if err := mgr.GetToFile(group, key, path); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *textTab) doAddGroup(name string) tea.Cmd {
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.AddGroup(name); err != nil {
			recordAudit(mgrs, session.AuditOpText, "text:group:"+name, false, "add group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, "text:group:"+name, true, "add group")
		return textReloadMsg{}
	}
}

// --- view ---

func (t *textTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *textTab) View() string {
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	} else if t.mode != textModeNormal {
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *textTab) viewBaseAt(h int) string {
	leftW := t.width / 4
	if leftW > 26 {
		leftW = 26
	}
	if leftW < 16 {
		leftW = 16
	}
	// Reserve 5 cols for inter-pane chrome: 1 gap + 2 left-pane border +
	// 2 right-pane border (lipgloss draws borders outside Width).
	rightW := t.width - leftW - 5
	if rightW < 4 {
		rightW = 4
	}

	left := t.renderGroups(leftW, h)
	right := t.renderItems(rightW, h)

	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(h).Render(left)
		right = paneStyle.Width(rightW).Height(h).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(h).Render(left)
		right = activePaneStyle.Width(rightW).Height(h).Render(right)
	}
	gap := lipgloss.NewStyle().Width(1).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, gap, right)
}

func (t *textTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups…")
	}
	if len(t.groups) == 0 {
		return emptyStateStyle.Render("no groups yet — press + to create one")
	}
	rows := make([]SidebarRow, 0, len(t.groups))
	for i, g := range t.groups {
		marker := " "
		if g.isAll {
			marker = "◯"
		}
		name := g.name
		if g.isAll {
			name = "All"
		} else if g.name == "default" {
			name += " (default)"
		}
		rows = append(rows, SidebarRow{
			Marker:   marker,
			Name:     name,
			Count:    g.keyCount,
			Selected: i == t.groupIndex && t.focusLeft,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

func (t *textTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading text blocks…")
	}
	group := t.currentGroup()
	if group == "" {
		return emptyStateStyle.Render("select a group first")
	}
	items := t.filteredItems()
	header := group
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	visibleKeys := make([]string, 0, len(items))
	for _, it := range items {
		visibleKeys = append(visibleKeys, it.group+"/"+it.key)
	}
	header += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(visibleKeys))
	if len(items) == 0 {
		hint := "no text blocks in this group"
		if t.filterBox.Term() != "" {
			hint = "no keys match /" + t.filterBox.Term()
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	var lines []string
	for i, it := range items {
		keyLabel := it.key
		if group == textAllLabel {
			keyLabel = it.group + "/" + keyLabel
		}
		if t.sel.IsSelected(it.group + "/" + it.key) {
			keyLabel = "[x] " + keyLabel
		}
		line := truncateRunes(fmt.Sprintf("%-24s %8d b  %s", keyLabel, it.size, it.updatedAt), inner-2)
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

func (t *textTab) renderModal() string {
	switch t.mode {
	case textModeBatchDeleteConfirm:
		targets := t.selectedTargets()
		var b strings.Builder
		for _, tgt := range targets {
			fmt.Fprintf(&b, "%s/%s\n", tgt[0], tgt[1])
		}
		return modalBox(t.width, t.height, fmt.Sprintf("delete %d text blocks?", len(targets)),
			strings.TrimRight(b.String(), "\n"), "enter/y delete all · esc/n cancel")
	case textModeBatchExportPath:
		return modalBox(t.width, t.height, "batch export to directory", t.input.View(), "enter export · esc cancel")
	case textModeDeleteGroupConfirm:
		group := t.currentGroup()
		body := fmt.Sprintf("group %s and all its text blocks will be deleted.", group)
		return modalBox(t.width, t.height, "delete group "+group+"?", body, "enter/y confirm · esc/n cancel")
	case textModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox(t.width, t.height, "delete "+it.key+"?", "", "enter/y confirm · esc/n cancel")
	case textModeExportPath:
		return modalBox(t.width, t.height, "export to file", t.input.View(), "enter export · esc cancel")
	case textModeNewKey:
		return modalBox(t.width, t.height, "new text block — key or group:key", t.input.View(), "enter open vim · esc cancel")
	case textModeAddGroup:
		return modalBox(t.width, t.height, "new group", t.input.View(), "enter create · esc cancel")
	case textModeFilter:
		return modalBox(t.width, t.height, "filter keys (case insensitive)", "/"+t.filterBox.Term()+"_", "esc clear")
	}
	return ""
}

// Compile-time guard: *textTab satisfies Tab.
var _ Tab = (*textTab)(nil)
