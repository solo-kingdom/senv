package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// envTab renders the Env data: left column = groups (with active ● marker),
// right column = key=value of the selected group (values masked by default).
type envTab struct {
	mgr Managers
	// form 非 nil 时表示打开了一个结构化表单（重命名等）；formSubmit 是该
	// 表单提交后的动作。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd
	// focusAfterLoad 是下一次加载后要定位到的 key（重命名后停在新名字上）。
	focusAfterLoad string
	width, height  int

	groups       []envGroupRow
	itemsByGroup map[string][]envItemRow // cached items per group
	groupIndex   int
	loaded       bool

	itemIndex int
	focusLeft bool

	maskRevealed bool                   // per-cursor unmask toggle
	deref        bool                   // show dereferenced values (task 6.1)
	derefResults map[string]derefResult // resolved values for the current group
	derefGroup   string                 // group derefResults corresponds to
	derefErrors  int                    // count of failed resolutions in current view
	filterBox    Filter
	// sel 仅承载多选集（游标仍由 groupIndex/itemIndex 承载，随侧栏改造统一）。
	sel List

	input textinput.Model
	mode  envMode
}

type envGroupRow struct {
	name        string
	description string
	isActive    bool
	isDefault   bool
	isAll       bool // All 伪组（侧栏顶部固定项）
	varCount    int
}

type envItemRow struct {
	group string // 所属分组（All 视图行前缀与选择标识用）
	key   string
	value string // raw stored value
}

type envMode int

const (
	envModeNormal envMode = iota
	envModeEditValue
	envModeDeleteConfirm
	envModeAddGroup
	envModeFilter
	envModeDeleteGroupConfirm
	envModeBatchDeleteConfirm
)

func newEnvTab(mgr Managers) *envTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &envTab{mgr: mgr, focusLeft: true, input: ti}
}

func (t *envTab) Title() string { return "Env" }

func (t *envTab) Bindings() []KeyAction {
	if t.form != nil {
		return formBindings()
	}
	switch t.mode {
	case envModeFilter:
		return filterBindings(false)
	case envModeEditValue:
		return []KeyAction{
			{[]string{"enter"}, "save", grpForm, false},
			{[]string{"esc"}, "cancel", grpForm, false},
		}
	case envModeAddGroup:
		return []KeyAction{
			{[]string{"enter"}, "create", grpForm, false},
			{[]string{"esc"}, "cancel", grpForm, false},
		}
	case envModeDeleteConfirm, envModeDeleteGroupConfirm:
		return confirmBindings()
	case envModeBatchDeleteConfirm:
		return []KeyAction{
			{[]string{"enter/y"}, "delete all", grpConfirm, false},
			{[]string{"esc/n"}, "cancel", grpConfirm, false},
		}
	}
	nav := navBindings(true)
	if t.focusLeft {
		// 侧栏：组操作进底栏；仍可从侧栏触发的条目动词只进 `?`
		groupOps := []KeyAction{
			{[]string{"t"}, "toggle group active", grpGroup, false},
			actRename, actDelete,
			{[]string{"+"}, "new group", grpGroup, false},
		}
		if row, ok := t.currentGroupRow(); ok && row.isAll {
			for i := range groupOps {
				groupOps[i].NoBar = true
			}
		}
		return append(append(nav, groupOps...),
			KeyAction{[]string{"n"}, "new", grpItem, true},
			KeyAction{[]string{"e"}, "edit", grpItem, true},
			KeyAction{[]string{"y"}, "copy", grpItem, true},
			KeyAction{[]string{"D"}, "deref on/off", grpGroup, true},
			actFilter, actRefresh,
		)
	}
	return append(nav,
		actEdit, actNew, actDelete, actRename,
		actSelect, actSelectAll,
		KeyAction{[]string{"v"}, "toggle value visibility", grpItem, false},
		KeyAction{[]string{"y"}, "copy", grpItem, false},
		KeyAction{[]string{"D"}, "deref on/off", grpGroup, false},
		KeyAction{[]string{"t"}, "toggle group active", grpGroup, true},
		KeyAction{[]string{"+"}, "new group", grpGroup, true},
		actFilter, actRefresh,
	)
}

// cursorForFocus 返回当前焦点栏的游标位置（翻页用）。
func (t *envTab) cursorForFocus() int {
	if t.focusLeft {
		return t.groupIndex
	}
	return t.itemIndex
}

func (t *envTab) InputMode() bool {
	if t.form != nil {
		return true
	}
	switch t.mode {
	case envModeFilter, envModeEditValue,
		envModeAddGroup, envModeDeleteConfirm, envModeDeleteGroupConfirm,
		envModeBatchDeleteConfirm:
		return true
	}
	return false
}

// --- data loading ---

// envLoadedMsg carries the freshly loaded env data into the tab.
type envLoadedMsg struct {
	groups       []envGroupRow
	itemsByGroup map[string][]envItemRow
	err          error
}

// envReloadMsg signals that a mutation succeeded and data should be reloaded.
type envReloadMsg struct{}

// envDerefMsg carries freshly resolved values for the current group (task 6.1).
type envDerefMsg struct {
	group   string
	results map[string]derefResult
}

func (t *envTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// Reload re-reads data in the background; the top level calls it after a
// background sync applies remote changes. stale-while-revalidate：重载期间
// 旧数据保持可见（不清回占位），完成后 envLoadedMsg 静默替换并保留光标与
// 表单状态。
func (t *envTab) Reload() tea.Cmd {
	return t.load()
}

func (t *envTab) load() tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		st := perflog.Start("tui.load-env")
		if mgr == nil {
			st.End(false)
			return envLoadedMsg{err: fmt.Errorf("env manager unavailable")}
		}
		// 单趟快照：分组列表与全部变量一次批量装载，并与搜索/deref/AI 共享。
		allVars, gis, err := envSnapshot(t.mgr)
		if err != nil {
			st.End(false)
			return envLoadedMsg{err: err}
		}
		groups := make([]envGroupRow, 0, len(gis))
		itemsByGroup := make(map[string][]envItemRow, len(gis))
		itemCount := 0
		for _, gi := range gis {
			// Hide groups that have no keys, except the default group which is
			// always shown as a stable landing point.
			if gi.VarCount == 0 && !gi.IsDefault {
				continue
			}
			groups = append(groups, envGroupRow{
				name:        gi.Name,
				description: gi.Description,
				isActive:    gi.IsActive,
				isDefault:   gi.IsDefault,
				varCount:    gi.VarCount,
			})
			itemsByGroup[gi.Name] = buildEnvItems(gi.Name, allVars[gi.Name])
			itemCount += len(itemsByGroup[gi.Name])
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].isDefault != groups[j].isDefault {
				return groups[i].isDefault
			}
			return groups[i].name < groups[j].name
		})
		// All 伪组置顶（默认选中）：聚合全部条目（grill D3 侧栏范式）
		groups = append([]envGroupRow{{name: envAllLabel, isAll: true, varCount: itemCount}}, groups...)
		st.With("groups", len(groups), "items", itemCount).End(true)
		return envLoadedMsg{groups: groups, itemsByGroup: itemsByGroup}
	}
}

// buildEnvItems converts a group's key->value map into a key-sorted item list.
func buildEnvItems(group string, vars map[string]string) []envItemRow {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]envItemRow, 0, len(keys))
	for _, k := range keys {
		items = append(items, envItemRow{group: group, key: k, value: vars[k]})
	}
	return items
}

// envAllLabel 是 env 侧栏顶部的 All 伪组标签。
const envAllLabel = "All"

// resolveDeref computes dereferenced values for the current group's items
// (all of them, so a filter change does not invalidate the cache). In the All
// pseudo-group view the items of every real group are aggregated, each
// resolving against its own group.
func (t *envTab) resolveDeref() tea.Cmd {
	mgr := t.mgr
	group := t.currentGroup()
	items := t.itemsByGroup[group]
	if group == envAllLabel {
		items = nil
		for _, g := range t.groups {
			if g.isAll {
				continue
			}
			items = append(items, t.itemsByGroup[g.name]...)
		}
	}
	return func() tea.Msg {
		results, err := resolveValues(mgr, items)
		if err != nil {
			return errMsg{err: err}
		}
		return envDerefMsg{group: group, results: results}
	}
}

func (t *envTab) currentGroup() string {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return ""
	}
	return t.groups[t.groupIndex].name
}

func (t *envTab) filteredItems() []envItemRow {
	var all []envItemRow
	if gr, ok := t.currentGroupRow(); ok && gr.isAll {
		// All 视图：聚合全部分组条目（行前缀 group/key 由渲染层处理）
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
	out := make([]envItemRow, 0, len(all))
	for _, it := range all {
		if t.filterBox.Matches(it.key) {
			out = append(out, it)
		}
	}
	return out
}

// currentItem returns the currently selected item, if any.
func (t *envTab) currentItem() (envItemRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return envItemRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *envTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	// Form results must be handled before routing further messages into the
	// still-open form.
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

	case envLoadedMsg:
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

	case envReloadMsg:
		// A mutation succeeded; refresh from storage.
		t.derefResults = nil
		t.derefGroup = ""
		cmd := t.load()
		if t.deref {
			return t, tea.Batch(cmd, t.resolveDeref())
		}
		return t, cmd

	case envDerefMsg:
		if msg.group == t.currentGroup() {
			t.derefResults = msg.results
			t.derefGroup = msg.group
			t.derefErrors = 0
			for _, r := range msg.results {
				if r.failed {
					t.derefErrors++
				}
			}
			if t.derefErrors > 0 {
				return t, warnToast(fmt.Sprintf("%d references failed to resolve (showing raw values)", t.derefErrors))
			}
		}
		return t, nil

	case tea.KeyMsg:
		if t.mode != envModeNormal {
			return t.handleModalKey(msg)
		}

		switch msg.String() {
		case "up", "k":
			t.moveCursor(-1)
		case "down", "j":
			t.moveCursor(1)
		case "left", "h":
			t.focusLeft = true
			t.maskRevealed = false
		case "right", "l":
			t.focusLeft = false
			t.maskRevealed = false
			t.itemIndex = 0 // 切到条目栏定位该分组第一条（侧栏范式）
		case "v":
			if !t.focusLeft {
				t.maskRevealed = !t.maskRevealed
			}
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
			return t.enterEditMode()
		case "n":
			return t.enterNewKeyMode()
		case "d":
			if t.focusLeft {
				if row, ok := t.currentGroupRow(); ok && row.isAll {
					return t, warnToast("All cannot be deleted, pick a specific group")
				}
				return t.enterDeleteGroupConfirm()
			}
			if t.sel.SelectionCount() > 1 {
				return t.enterBatchDeleteConfirm()
			}
			return t.enterDeleteConfirm()
		case "r":
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple entries selected: narrow to a single selection to rename")
			}
			if t.focusLeft {
				if row, ok := t.currentGroupRow(); ok && row.isAll {
					return t, warnToast("All cannot be renamed, pick a specific group")
				}
			}
			return t.enterRenameMode()
		case "t":
			row, ok := t.currentGroupRow()
			if !ok {
				return t, nil
			}
			if row.isAll {
				return t, warnToast("All has no activation semantics, pick a specific group")
			}
			if row.isActive {
				return t, t.doDeactivate()
			}
			return t, t.doActivate()
		case "+":
			return t.enterAddGroupMode()
		case "y":
			return t, t.doCopy()
		case "D":
			t.deref = !t.deref
			if t.deref {
				t.derefResults = nil
				t.derefGroup = ""
				return t, tea.Batch(okToast("dereference view: ON"), t.resolveDeref())
			}
			t.derefResults = nil
			t.derefGroup = ""
			return t, okToast("dereference view: OFF")
		case "/":
			return t.enterFilterMode()
		}

		// If dereference is on and the displayed group changed, refresh the
		// resolved values so the right column reflects the current group.
		if t.deref && t.currentGroup() != "" && t.currentGroup() != t.derefGroup {
			return t, t.resolveDeref()
		}

	}
	return t, nil
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// --- navigation helpers ---

func (t *envTab) listForFocus() []string {
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

func (t *envTab) moveCursor(delta int) {
	if t.focusLeft {
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(t.itemIndex+delta, 0, len(items)-1)
	}
	// Moving the cursor re-masks the previously revealed value.
	t.maskRevealed = false
}

func (t *envTab) jumpCursor(idx int) {
	if t.focusLeft {
		t.groupIndex = clamp(idx, 0, len(t.groups)-1)
		t.itemIndex = 0
	} else {
		items := t.filteredItems()
		t.itemIndex = clamp(idx, 0, len(items)-1)
	}
	t.maskRevealed = false
}

func (t *envTab) clampCursors() {
	t.groupIndex = clamp(t.groupIndex, 0, len(t.groups)-1)
	t.itemIndex = clamp(t.itemIndex, 0, len(t.filteredItems())-1)
}

// focusJump positions the cursor at (group, key) for search-result navigation.
// It clears any filter and mask so the target is visible at its real position.
func (t *envTab) focusJump(group, key string) {
	t.filterBox.Clear()
	t.mode = envModeNormal
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
	t.maskRevealed = false
}

// --- modal handling ---

func (t *envTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	// 批量删除确认：enter/y 提交（提交即清空多选集），esc/n 取消。
	if t.mode == envModeBatchDeleteConfirm {
		targets := t.selectedTargets()
		switch msg.String() {
		case "enter", "y":
			t.mode = envModeNormal
			t.sel.ClearSelection()
			return t, t.doBatchDelete(targets)
		case "esc", "n":
			t.mode = envModeNormal
			return t, nil
		}
		return t, nil
	}

	// Group deletion confirmation uses dedicated keys (no text input).
	if t.mode == envModeDeleteGroupConfirm {
		name := t.currentGroup()
		row, ok := t.currentGroupRow()
		t.mode = envModeNormal
		if !ok {
			return t, nil
		}
		switch msg.String() {
		case "enter", "y":
			return t, t.doDeleteGroup(name, row.isActive)
		default: // esc, n, anything else
			return t, nil
		}
	}

	// Delete confirmation uses dedicated keys (no text input).
	if t.mode == envModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = envModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(t.focusGroup(it), it.key)
		default: // esc, n, anything else
			t.mode = envModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == envModeFilter {
			t.filterBox.Clear() // esc 清词并退出，恢复完整列表
		}
		t.mode = envModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	// Filter mode（共享 Filter 状态机）：输入即过滤。
	if t.mode == envModeFilter {
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

	// Default: feed keystrokes to the text input.
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

// isPrintable reports whether a KeyMsg is a single printable rune.
func (t *envTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case envModeEditValue:
		it, ok := t.currentItem()
		if !ok {
			t.mode = envModeNormal
			return t, nil
		}
		value := t.input.Value()
		group := t.focusGroup(it)
		key := it.key
		t.mode = envModeNormal
		t.input.Blur()
		return t, t.doSet(group, key, value, nil)

	case envModeAddGroup:
		name := t.input.Value()
		t.mode = envModeNormal
		t.input.Blur()
		if name == "" {
			return t, warnToast("group name cannot be empty")
		}
		return t, warnToast("group description is required")
	}
	t.mode = envModeNormal
	return t, nil
}

// --- modal entry points ---

func (t *envTab) enterEditMode() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		return t, warnToast("no entry to edit")
	}
	t.mode = envModeEditValue
	t.input.Placeholder = "value"
	t.input.SetValue(it.value)
	t.input.Focus()
	t.input.CursorEnd()
	return t, textinput.Blink
}

// enterNewKeyMode 新建环境变量走结构化表单（key + 遮蔽 value，grill D6-⑥）。
func (t *envTab) enterNewKeyMode() (Tab, tea.Cmd) {
	group := t.realGroup()
	if group == "" {
		return t, warnToast("select a group first, or pick one in the group sidebar before creating")
	}
	siblings := make(map[string]bool, len(t.itemsByGroup[group]))
	for _, row := range t.itemsByGroup[group] {
		siblings[row.key] = true
	}
	f := newForm("new env var — "+group,
		formField{key: "key", label: "key", kind: formText, placeholder: "DB_HOST",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid key")
				}
				if siblings[v] {
					return fmt.Errorf("key %s already exists in %s", v, group)
				}
				return nil
			}},
		formField{key: "value", label: "value", kind: formSecret, placeholder: "value (echo masked)",
			validate: func(v string) error {
				if v == "" {
					return fmt.Errorf("value cannot be empty")
				}
				return nil
			}},
		optionalDescriptionField(""),
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		desc := strings.TrimSpace(values["description"])
		return t.doSet(group, strings.TrimSpace(values["key"]), values["value"], &desc)
	})
	return t, nil
}

func (t *envTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		return t, warnToast("no entry to delete")
	}
	t.mode = envModeDeleteConfirm
	return t, nil
}

// focusGroup 返回条目操作应使用的分组名：All 视图下取条目自带分组，
// 普通视图为当前分组。
func (t *envTab) focusGroup(it envItemRow) string {
	if it.group != "" {
		return it.group
	}
	return t.currentGroup()
}

// realGroup 返回当前选中的真实分组名；All 伪组返回 ""。
func (t *envTab) realGroup() string {
	if row, ok := t.currentGroupRow(); ok && row.isAll {
		return ""
	}
	return t.currentGroup()
}

// selectionKey 返回当前条目的稳定选择标识（group/key）。
func (t *envTab) selectionKey() string {
	if it, ok := t.currentItem(); ok {
		return it.group + "/" + it.key
	}
	return ""
}

// selectedTargets 返回多选集命中的可见条目（group, key）。
func (t *envTab) selectedTargets() [][2]string {
	var out [][2]string
	for _, it := range t.filteredItems() {
		k := it.group + "/" + it.key
		if t.sel.IsSelected(k) {
			out = append(out, [2]string{it.group, it.key})
		}
	}
	return out
}

// enterBatchDeleteConfirm 多选集批量删除：一次确认列全部目标（grill D9）。
func (t *envTab) enterBatchDeleteConfirm() (Tab, tea.Cmd) {
	if len(t.selectedTargets()) == 0 {
		return t, warnToast("selection is empty or has no entries in the current group")
	}
	t.mode = envModeBatchDeleteConfirm
	return t, nil
}

// doBatchDelete 逐条删除选择集；单条失败不中止其余，结束统一提示。
// 多选集在确认提交时已清空（grill D9：提交后不残留失效选择）。
func (t *envTab) doBatchDelete(targets [][2]string) tea.Cmd {
	mgr := t.mgr.Env
	return func() tea.Msg {
		failed := 0
		for _, tgt := range targets {
			if err := mgr.Delete(tgt[0], tgt[1]); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("batch delete finished, %d failed", failed)}
		}
		return okToast(fmt.Sprintf("deleted %d entries", len(targets)))
	}
}

func (t *envTab) enterAddGroupMode() (Tab, tea.Cmd) {
	siblings := make(map[string]bool, len(t.groups))
	for _, g := range t.groups {
		siblings[g.name] = true
	}
	f := newForm("new env group",
		formField{key: "name", label: "name", kind: formText, placeholder: "group-name",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("group name cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid group name")
				}
				if siblings[v] {
					return fmt.Errorf("group %s already exists", v)
				}
				return nil
			}},
		formField{key: "description", label: "description", kind: formText, placeholder: "required: what this group is for",
			validate: func(v string) error {
				if _, err := storage.ValidateDescription(v, false); err != nil {
					return err
				}
				return nil
			}},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doAddGroup(strings.TrimSpace(values["name"]), values["description"])
	})
	return t, nil
}

// openForm installs a structured form and the action to run on submit.
func (t *envTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// enterDeleteGroupConfirm asks before deleting the focused group; a group that
// is currently active needs the extra activation warning (task 3.1).
func (t *envTab) enterDeleteGroupConfirm() (Tab, tea.Cmd) {
	row, ok := t.currentGroupRow()
	if !ok {
		return t, warnToast("no group to delete")
	}
	if row.isDefault {
		return t, warnToast("default group cannot be deleted")
	}
	t.mode = envModeDeleteGroupConfirm
	return t, nil
}

// enterRenameMode opens the rename form for whatever the focused pane shows:
// the group (left) or the env key (right).
func (t *envTab) enterRenameMode() (Tab, tea.Cmd) {
	if t.focusLeft {
		row, ok := t.currentGroupRow()
		if !ok {
			return t, warnToast("no group to rename")
		}
		if row.isDefault {
			return t, warnToast("default group cannot be renamed")
		}
		siblings := make(map[string]bool, len(t.groups))
		for _, g := range t.groups {
			siblings[g.name] = true
		}
		old := row.name
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
	f := newForm("rename env var",
		formField{key: "key", label: "new key", kind: formText, value: old, placeholder: "NEW_KEY",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("key cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid key")
				}
				if err := storage.ValidateEnvKey(v); err != nil {
					return fmt.Errorf("invalid key: %v", err)
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

func (t *envTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = envModeFilter
	t.filterBox.EnterFresh() // env 语义：`/` 清词重新开始
	return t, nil
}

// --- manager operations (executed in a command goroutine) ---

func (t *envTab) doSet(group, key, value string, description *string) tea.Cmd {
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		var err error
		if description != nil {
			err = mgr.SetWithDescription(group, key, value, description)
		} else {
			err = mgr.Set(group, key, value)
		}
		if err != nil {
			recordAudit(mgrs, session.AuditOpEnv, envTarget(group, key), false, "set failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, envTarget(group, key), true, "set")
		return envReloadMsg{}
	}
}

func (t *envTab) doDelete(group, key string) tea.Cmd {
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Delete(group, key); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, envTarget(group, key), false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, envTarget(group, key), true, "delete")
		return envReloadMsg{}
	}
}

// doRenameKey atomically renames an env key (value untouched) and leaves the
// cursor on the new key after the reload.
func (t *envTab) doRenameKey(group, oldKey, newKey string) tea.Cmd {
	if oldKey == newKey {
		return warnToast("key unchanged")
	}
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameKey(group, oldKey, newKey); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, envTarget(group, oldKey), false, "rename failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, envTarget(group, newKey), true, "rename "+oldKey)
		return renameDoneMsg{group: group, key: newKey, text: "renamed to " + newKey}
	}
}

// doRenameGroup renames a group and keeps its variables and activation state.
func (t *envTab) doRenameGroup(oldName, newName string) tea.Cmd {
	if oldName == newName {
		return warnToast("group name unchanged")
	}
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.RenameGroup(oldName, newName); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, "env:group:"+oldName, false, "rename group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, "env:group:"+newName, true, "rename group "+oldName)
		return envReloadMsg{}
	}
}

// doDeleteGroup deletes a group; allowActive records the user's explicit
// confirmation for a group that is currently active.
func (t *envTab) doDeleteGroup(name string, allowActive bool) tea.Cmd {
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.DeleteGroup(name, allowActive); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, false, "delete group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, true, "delete group")
		return envReloadMsg{}
	}
}

func (t *envTab) doActivate() tea.Cmd {
	name := t.currentGroup()
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.ActivateGroup(name); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, false, "activate failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, true, "activate")
		return envReloadMsg{}
	}
}

func (t *envTab) doDeactivate() tea.Cmd {
	name := t.currentGroup()
	if g, ok := t.currentGroupRow(); ok && g.isDefault {
		return warnToast("default group cannot be deactivated")
	}
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.DeactivateGroup(name); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, false, "deactivate failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, true, "deactivate")
		return envReloadMsg{}
	}
}

func (t *envTab) doAddGroup(name, description string) tea.Cmd {
	mgr := t.mgr.Env
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.AddGroup(name, description); err != nil {
			recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, false, "add group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpEnv, "env:group:"+name, true, "add group")
		return envReloadMsg{}
	}
}

func (t *envTab) doCopy() tea.Cmd {
	it, ok := t.currentItem()
	if !ok {
		return warnToast("nothing to copy")
	}
	value := it.value
	key := it.key
	return func() tea.Msg {
		if err := copyToClipboard(value); err != nil {
			return errMsg{err: err}
		}
		return toastMsg{text: "copied " + key, level: toastSuccess}
	}
}

func (t *envTab) currentGroupRow() (envGroupRow, bool) {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return envGroupRow{}, false
	}
	return t.groups[t.groupIndex], true
}

// --- view ---

func (t *envTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *envTab) View() string {
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	} else if t.mode != envModeNormal {
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *envTab) viewBaseAt(h int) string {
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

func (t *envTab) renderGroups(width, height int) string {
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
		} else if g.isActive {
			marker = "●"
		}
		name := g.name
		if g.isAll {
			name = "All"
		} else if g.isDefault {
			name += " (default)"
		}
		rows = append(rows, SidebarRow{
			Marker:   marker,
			Name:     name,
			Count:    g.varCount,
			Selected: i == t.groupIndex && t.focusLeft,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

func (t *envTab) renderItems(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading env vars…")
	}
	group := t.currentGroup()
	if group == "" {
		return emptyStateStyle.Render("select a group first")
	}
	items := t.filteredItems()
	header := group
	if row, ok := t.currentGroupRow(); ok && row.description != "" && !row.isAll {
		header += " — " + truncateRunes(row.description, 40)
	}
	if t.filterBox.Term() != "" {
		header += "  /" + t.filterBox.Term()
	}
	visibleKeys := make([]string, 0, len(items))
	for _, it := range items {
		visibleKeys = append(visibleKeys, it.group+"/"+it.key)
	}
	header += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(visibleKeys))
	if len(items) == 0 {
		hint := "no env vars in this group"
		if t.filterBox.Term() != "" {
			hint = "no keys match /" + t.filterBox.Term()
		}
		return lipgloss.JoinVertical(lipgloss.Left, paneTitleStyle.Render(header), emptyStateStyle.Render(hint))
	}
	inner := width - 2
	var lines []string
	for i, it := range items {
		shown := it.value
		failed := false
		if t.deref {
			if dr, ok := t.derefResults[it.group+"/"+it.key]; ok {
				shown = dr.resolved
				failed = dr.failed
			}
		}
		revealed := i == t.itemIndex && t.maskRevealed
		if !revealed {
			shown = maskValue(shown)
		}
		keyLabel := it.key
		if group == envAllLabel {
			keyLabel = it.group + "/" + keyLabel
		}
		if t.sel.IsSelected(it.group + "/" + it.key) {
			keyLabel = "[x] " + keyLabel
		}
		if failed {
			keyLabel = "⚠ " + keyLabel
		}
		// Leave 2 cols for the cursor marker so Width-wrap cannot inflate the pane.
		shown = truncateRunes(shown, maxInt(4, inner-2-len([]rune(keyLabel))-1))
		var line string
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ "+keyLabel+"=") + renderValue(shown, revealed)
		} else {
			line = keyLabel + "=" + renderValue(shown, false)
		}
		lines = append(lines, line)
	}
	return windowedPane(header, lines, t.itemIndex, height, width)
}

// renderModal renders the active modal input/confirm panel.
func (t *envTab) renderModal() string {
	switch t.mode {
	case envModeEditValue:
		return modalBox(t.width, t.height, "edit "+t.currentItemKeyLabel(), t.input.View(), "enter save · esc cancel")
	case envModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox(t.width, t.height, "delete "+it.key+"?", "", "enter/y confirm · esc/n cancel")
	case envModeBatchDeleteConfirm:
		targets := t.selectedTargets()
		var b strings.Builder
		for _, tgt := range targets {
			fmt.Fprintf(&b, "%s/%s\n", tgt[0], tgt[1])
		}
		return modalBox(t.width, t.height, fmt.Sprintf("delete %d variables?", len(targets)),
			strings.TrimRight(b.String(), "\n"), "enter/y delete all · esc/n cancel")
	case envModeDeleteGroupConfirm:
		row, _ := t.currentGroupRow()
		hint := "enter/y confirm · esc/n cancel"
		body := fmt.Sprintf("group %s and its %d variables will be deleted.", row.name, row.varCount)
		if row.isActive {
			body += "\nthis group is currently active; its variables will no longer appear in exports after deletion."
		}
		return modalBox(t.width, t.height, "delete group "+row.name+"?", body, hint)
	case envModeAddGroup:
		return modalBox(t.width, t.height, "new group", t.input.View(), "enter create · esc cancel")
	case envModeFilter:
		return modalBox(t.width, t.height, "filter keys (case insensitive)", "/"+t.filterBox.Term()+"_", "esc clear")
	}
	return ""
}

func (t *envTab) currentItemKeyLabel() string {
	if it, ok := t.currentItem(); ok {
		return it.key
	}
	return "?"
}

// modalBox renders a bordered modal line with a title, body and hint.
// --- masking ---

func maskValue(v string) string {
	if v == "" {
		return "***"
	}
	r := []rune(v)
	const prefix = 3
	if len(r) <= prefix {
		return string(r[:1]) + "***"
	}
	return string(r[:prefix]) + "***"
}

func renderValue(v string, revealed bool) string {
	if revealed {
		return v
	}
	return maskedValueStyle.Render(v)
}

// Compile-time guard: *envTab satisfies Tab.
var _ Tab = (*envTab)(nil)
