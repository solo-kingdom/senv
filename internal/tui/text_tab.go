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
		{[]string{"y"}, "复制"},
		actExport,
		{[]string{"+"}, "新建分组"},
		{[]string{"D"}, "解引用切换"},
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

type textReloadMsg struct{}

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
		gs, err := mgr.ListGroups()
		if err != nil {
			st.End(false)
			return textLoadedMsg{err: err}
		}
		groups := make([]textGroupRow, 0, len(gs))
		itemsByGroup := make(map[string][]textItemRow, len(gs))
		totalKeys := 0
		for _, g := range gs {
			// 侧栏范式：空分组也显示（计数 0），与过滤期行为一致
			groups = append(groups, textGroupRow{name: g.Name, keyCount: g.KeyCount})
			infos, err := mgr.List(g.Name)
			if err != nil {
				itemsByGroup[g.Name] = nil
				continue
			}
			itemsByGroup[g.Name] = buildTextItems(g.Name, infos)
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
		return t, warnToast("已取消")
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
				t.sel.Toggle(t.selectionKey())
			}
		case "a":
			if !t.focusLeft {
				keys := make([]string, 0, len(t.filteredItems()))
				for _, it := range t.filteredItems() {
					keys = append(keys, t.currentGroup()+"/"+it.key)
				}
				t.sel.SelectVisible(keys)
			}
		case "e":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("已多选条目：编辑需先缩小到单选")
			}
			return t.editCurrent()
		case "n":
			return t.enterNewKeyMode()
		case "d":
			if t.focusLeft {
				return t.enterDeleteGroupConfirm()
			}
			if t.sel.SelectionCount() > 1 {
				return t.enterBatchDeleteConfirm()
			}
			return t.enterDeleteConfirm()
		case "r":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("已多选条目：重命名需先缩小到单选")
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
			return t, okToast("解引用视图：" + onOff(t.deref) + "（列表仅显示元信息）")
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
		t.groupIndex = clamp(t.groupIndex+delta, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(t.itemIndex+delta, 0, maxLen(items)-1)
	}
}

func (t *textTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, maxLen(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(idx, 0, maxLen(items)-1)
	}
}

func (t *textTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, maxLen(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
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
			return t, warnToast("已取消导出")
		}
		t.sel.ClearSelection() // 提交即清空多选集
		return t, t.doBatchExport(dir, targets)
	case textModeExportPath:
		path := t.input.Value()
		it, ok := t.currentItem()
		t.mode = textModeNormal
		t.input.Blur()
		if !ok || path == "" {
			return t, warnToast("已取消导出")
		}
		return t, t.doExport(t.focusGroup(it), it.key, path)
	case textModeNewKey:
		group, key := parseKeyAddress(t.input.Value(), t.realGroup())
		t.mode = textModeNormal
		t.input.Blur()
		if key == "" {
			return t, warnToast("key 不能为空")
		}
		if group == "" {
			return t, warnToast("请先选择分组，或使用 group:key 形式")
		}
		return t.editKey(group, key)
	case textModeAddGroup:
		name := t.input.Value()
		t.mode = textModeNormal
		t.input.Blur()
		if name == "" {
			return t, warnToast("分组名不能为空")
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
	t.input.Placeholder = "key 或 group:key"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("没有可删除的条目")
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
		return t, warnToast("多选集为空或不含当前分组的条目")
	}
	t.mode = textModeBatchDeleteConfirm
	return t, nil
}

// selectedTargets 返回多选集命中的可见条目（group, key）。
func (t *textTab) selectedTargets() [][2]string {
	var out [][2]string
	for _, it := range t.filteredItems() {
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
		return t, warnToast("多选集为空或不含当前分组的条目")
	}
	t.mode = textModeBatchExportPath
	t.input.SetValue("")
	t.input.Placeholder = "输出目录（每块写入 <目录>/<key>.txt）"
	t.input.Focus()
	return t, textinput.Blink
}

// doBatchDelete 逐条删除选择集；单条失败不中止其余。
func (t *textTab) doBatchDelete(targets [][2]string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		failed := 0
		for _, tgt := range targets {
			if err := mgr.Delete(tgt[0], tgt[1]); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("批量删除完成，%d 条失败", failed)}
		}
		return okToast(fmt.Sprintf("已删除 %d 条", len(targets)))
	}
}

// doBatchExport 逐块导出到 dir/<key>.txt；单条失败不中止其余。
func (t *textTab) doBatchExport(dir string, targets [][2]string) tea.Cmd {
	mgr := t.mgr.Text
	return func() tea.Msg {
		failed := 0
		for _, tgt := range targets {
			path := filepath.Join(dir, tgt[1]+".txt")
			if err := mgr.GetToFile(tgt[0], tgt[1], path); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("批量导出完成，%d 条失败", failed)}
		}
		return okToast(fmt.Sprintf("已导出 %d 条到 %s", len(targets), dir))
	}
}

func (t *textTab) enterExportMode() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("没有可导出的条目")
	}
	t.mode = textModeExportPath
	t.input.SetValue("")
	t.input.Placeholder = "输出文件路径"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *textTab) enterAddGroupMode() (Tab, tea.Cmd) {
	t.mode = textModeAddGroup
	t.input.SetValue("")
	t.input.Placeholder = "分组名"
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
		return t, warnToast("没有可删除的分组")
	}
	if group == defaultTextGroup {
		return t, warnToast("default 分组不可删除")
	}
	t.mode = textModeDeleteGroupConfirm
	return t, nil
}

// enterRenameMode renames whatever the focused pane shows: the group (left) or
// the text key (right).
func (t *textTab) enterRenameMode() (Tab, tea.Cmd) {
	if t.focusLeft {
		if row, ok := t.currentGroupRow(); ok && row.isAll {
			return t, warnToast("All 无法重命名，请选择具体分组")
		}
		group := t.currentGroup()
		if group == "" {
			return t, warnToast("没有可重命名的分组")
		}
		if group == defaultTextGroup {
			return t, warnToast("default 分组不可重命名")
		}
		siblings := make(map[string]bool, len(t.groups))
		for _, g := range t.groups {
			siblings[g.name] = true
		}
		old := group
		f := newForm("重命名分组",
			formField{key: "name", label: "新名称", kind: formText, value: old, placeholder: "new-group-name",
				validate: func(v string) error {
					v = strings.TrimSpace(v)
					if v == "" {
						return fmt.Errorf("分组名不能为空")
					}
					if err := storage.ValidateName(v); err != nil {
						return fmt.Errorf("非法分组名")
					}
					if v != old && siblings[v] {
						return fmt.Errorf("分组 %s 已存在", v)
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
		return t, warnToast("没有可重命名的条目")
	}
	group := t.focusGroup(it)
	siblings := make(map[string]bool)
	for _, row := range t.itemsByGroup[group] {
		siblings[row.key] = true
	}
	old := it.key
	f := newForm("重命名文本块",
		formField{key: "key", label: "新 key", kind: formText, value: old, placeholder: "new-key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key 不能为空")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("非法 key")
				}
				if v != old && siblings[v] {
					return fmt.Errorf("key %s 已存在于 %s", v, group)
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
	f := newForm("从文件导入文本块",
		formField{key: "group", label: "分组", kind: formText, value: group, placeholder: "group",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("分组不能为空")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("非法分组名")
				}
				return nil
			}},
		formField{key: "key", label: "key", kind: formText, placeholder: "key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key 不能为空")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("非法 key")
				}
				return nil
			}},
		formField{key: "path", label: "源文件路径", kind: formPath, placeholder: "/path/to/file"},
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
		return t, warnToast("没有可编辑的条目")
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
		recordAudit(t.mgr, session.AuditOpText, textTarget(es.Group, es.Key), false, "edit editor 失败")
		return errMsg{err: fmt.Errorf("editor failed: %w", runErr)}
	}
	if _, ferr := t.mgr.Text.FinishEditor(es); ferr != nil {
		recordAudit(t.mgr, session.AuditOpText, textTarget(es.Group, es.Key), false, "edit 失败")
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
			recordAudit(mgrs, session.AuditOpText, textTarget(group, key), false, "delete 失败")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, key), true, "delete")
		return textReloadMsg{}
	}
}

// doRenameKey atomically renames a text block (content untouched).
func (t *textTab) doRenameKey(group, oldKey, newKey string) tea.Cmd {
	if oldKey == newKey {
		return warnToast("key 未变化")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameKey(group, oldKey, newKey); err != nil {
			recordAudit(mgrs, session.AuditOpText, textTarget(group, oldKey), false, "rename 失败")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, newKey), true, "rename "+oldKey)
		return renameDoneMsg{group: group, key: newKey, text: "已重命名为 " + newKey}
	}
}

// doRenameGroup renames a text group and keeps every block inside it.
func (t *textTab) doRenameGroup(oldName, newName string) tea.Cmd {
	if oldName == newName {
		return warnToast("分组名未变化")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameGroup(oldName, newName); err != nil {
			recordAudit(mgrs, session.AuditOpText, "text:group:"+oldName, false, "rename group 失败")
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
			recordAudit(mgrs, session.AuditOpText, "text:group:"+name, false, "delete group 失败")
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
		return warnToast("源文件路径不能为空")
	}
	mgr := t.mgr.Text
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.SetFromFile(group, key, path); err != nil {
			recordAudit(mgrs, session.AuditOpText, textTarget(group, key), false, "import 失败")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpText, textTarget(group, key), true, "import "+path)
		return renameDoneMsg{group: group, key: key, text: "已导入 " + key}
	}
}

func (t *textTab) doCopy() tea.Cmd {
	it, ok := t.currentItem()
	if !ok {
		return warnToast("没有可复制的内容")
	}
	mgr := t.mgr.Text
	group := t.currentGroup()
	key := it.key
	return func() tea.Msg {
		if err := mgr.GetToClipboard(group, key); err != nil {
			return errMsg{err: err}
		}
		return toastMsg{text: "已复制 " + key, level: toastSuccess}
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
			recordAudit(mgrs, session.AuditOpText, "text:group:"+name, false, "add group 失败")
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
		return emptyStateStyle.Render("加载分组中…")
	}
	if len(t.groups) == 0 {
		return emptyStateStyle.Render("暂无分组 — 按 + 新建")
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
		return emptyStateStyle.Render("加载文本块中…")
	}
	group := t.currentGroup()
	if group == "" {
		return emptyStateStyle.Render("请先选择分组")
	}
	items := t.filteredItems()
	header := group
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	visibleKeys := make([]string, 0, len(items))
	for _, it := range items {
		visibleKeys = append(visibleKeys, group+"/"+it.key)
	}
	header += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(visibleKeys))
	if len(items) == 0 {
		hint := "该分组暂无文本块"
		if t.filterBox.Term() != "" {
			hint = "no keys match /" + t.filterBox.Term()
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	var lines []string
	for i, it := range items {
		keyLabel := it.key
		if group == "" {
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
		return modalBox(fmt.Sprintf("删除 %d 条文本块？", len(targets)),
			strings.TrimRight(b.String(), "\n"), "enter/y 全部删除 · esc/n 取消")
	case textModeBatchExportPath:
		return modalBox("批量导出到目录", t.input.View(), "enter 导出 · esc 取消")
	case textModeDeleteGroupConfirm:
		group := t.currentGroup()
		body := fmt.Sprintf("将删除分组 %s 及其全部文本块。", group)
		return modalBox("删除分组 "+group+"？", body, "enter/y 确认 · esc/n 取消")
	case textModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox("删除 "+it.key+"？", "", "enter/y 确认 · esc/n 取消")
	case textModeExportPath:
		return modalBox("导出到文件", t.input.View(), "enter 导出 · esc 取消")
	case textModeNewKey:
		return modalBox("新建文本块 — key 或 group:key", t.input.View(), "enter 打开 vim · esc 取消")
	case textModeAddGroup:
		return modalBox("新建分组", t.input.View(), "enter 创建 · esc 取消")
	case textModeFilter:
		return modalBox("过滤 key（忽略大小写）", "/"+t.filterBox.Term()+"_", "esc 清除")
	}
	return ""
}

// Compile-time guard: *textTab satisfies Tab.
var _ Tab = (*textTab)(nil)
