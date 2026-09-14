package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// keyPairTab 是 KeyPair 的独立编辑板块：分组侧栏 → KeyPair 列表两栏。
// keypair 支持 import/rename/group 编辑/delete/materialize/详情；host 只以
// 「被 N 个 Host 引用」的引用计数形态出现（hostRefs）。私钥内容绝不进入
// Tab 状态或渲染：列表与详情只含指纹/公钥等安全元数据。
type keyPairTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	// hosts 仅为引用计数（hostRefs）而装载，不渲染 host 列表。
	hosts     []storage.HostEntry
	keyPairs  []ssh.KeyPairSummary
	filterBox Filter // `/` 过滤列表（名称标识）
	sel       List   // 预留多选承载（与 host Tab 同构）

	groupIndex int // 侧栏选中组（groups() 的下标）
	focus      kpPane
	keyIndex   int
	loadErr    string
	detail     *detailOverlay
	// pendingJump parks the cursor on a keypair after a reload (rename/import).
	pendingJump string

	// form 非 nil 时表示打开了一个结构化表单（导入/重命名/group 编辑）。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode            kpMode
	pendingKey      string   // keypair staged for delete/materialize
	pendingForce    bool     // materialize overwrite confirmed
	keyRefs         []string // hosts referencing pendingKey
	materializePath string
	pendingPrune    []ssh.PruneCandidate
}

// kpMode is the tab's confirmation state. Every non-normal mode owns the
// keyboard (InputMode) so global shortcuts cannot interrupt a decision.
type kpMode int

const (
	kpModeNormal kpMode = iota
	kpModeDeleteKey
	kpModeMaterialize
	kpModePrune
)

// kpPane 是 KeyPair Tab 两栏的焦点栏位：侧栏 ↔ 列表，`←→/hl` 切换。
type kpPane int

const (
	kpPaneGroup kpPane = iota
	kpPaneList
)

type kpLoadedMsg struct {
	hosts    []storage.HostEntry
	keyPairs []ssh.KeyPairSummary
	err      error
}

// kpReloadMsg reports a successful vault write; the tab reloads and parks the
// cursor on the named keypair so the effect of the write is visible.
type kpReloadMsg struct {
	toast   string
	keyName string
}

// kpPruneMsg 是 PruneCandidates 的异步结果。
type kpPruneMsg struct {
	candidates []ssh.PruneCandidate
	err        error
}

// kpPruneDoneMsg 是 DeletePrunedFiles 的异步结果。
type kpPruneDoneMsg struct {
	deleted int
	total   int
	err     error
}

func newKeyPairTab(mgr Managers) *keyPairTab {
	return &keyPairTab{mgr: mgr, focus: kpPaneList}
}

func (t *keyPairTab) Title() string { return "KeyPair" }

func (t *keyPairTab) Bindings() []KeyAction {
	if t.form != nil {
		return []KeyAction{
			{[]string{"tab/↑↓"}, "switch field", grpForm},
			{[]string{"enter"}, "submit", grpForm},
			{[]string{"esc"}, "cancel", grpForm},
		}
	}
	switch t.mode {
	case kpModeDeleteKey:
		return []KeyAction{
			{[]string{"enter/y"}, "confirm", grpConfirm},
			{[]string{"esc/n"}, "cancel", grpConfirm},
			{[]string{"F"}, "force delete (clear host identityKey)", grpConfirm},
		}
	case kpModeMaterialize, kpModePrune:
		return []KeyAction{{[]string{"enter/y"}, "confirm", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	}
	nav := []KeyAction{actUp, actDown, actLeft, actRight, actDetail,
		actTop, actBottom, actPageUp, actPageDn}
	prune := KeyAction{[]string{"p"}, "prune", grpItem}
	if t.focus == kpPaneList {
		return append(append(nav,
			KeyAction{[]string{"n", "i"}, "import keypair", grpItem},
			KeyAction{[]string{"r"}, "rename keypair", grpItem},
			KeyAction{[]string{"e"}, "edit group", grpItem},
			KeyAction{[]string{"d"}, "delete", grpItem},
			KeyAction{[]string{"m"}, "materialize", grpItem},
			prune,
		), actRefresh, actFilter)
	}
	return append(nav, prune, actRefresh, actFilter)
}

func (t *keyPairTab) InputMode() bool {
	return t.form != nil || t.mode != kpModeNormal || t.filterBox.Active()
}

// groups 由当前 keypair 集合派生侧栏行（数据单一来源，渲染/导航共用）。
// 排序与 Host 侧栏同构：All 伪组置顶 → 组名字母序 → 「未分组」置底
// （仅在有未归类 keypair 时出现）。
func (t *keyPairTab) groups() []sshGroupRow {
	counts := map[string]int{}
	ungrouped := 0
	for _, k := range t.keyPairs {
		if k.Group == "" {
			ungrouped++
		} else {
			counts[k.Group]++
		}
	}
	rows := make([]sshGroupRow, 0, len(counts)+2)
	rows = append(rows, sshGroupRow{name: sshAllLabel, isAll: true, count: len(t.keyPairs)})
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rows = append(rows, sshGroupRow{name: name, count: counts[name]})
	}
	if ungrouped > 0 {
		rows = append(rows, sshGroupRow{name: sshUngroupedLabel, isUngrouped: true, count: ungrouped})
	}
	return rows
}

// currentGroupRow 返回侧栏当前选中的组行。
func (t *keyPairTab) currentGroupRow() (sshGroupRow, bool) {
	groups := t.groups()
	if t.groupIndex < 0 || t.groupIndex >= len(groups) {
		return sshGroupRow{}, false
	}
	return groups[t.groupIndex], true
}

// visibleKeyPairs 返回选中组内、再经 `/` 过滤后的可见列表（组内按名称
// 字典序）。空词 = 全量；All 伪组 = 全部 keypair。
func (t *keyPairTab) visibleKeyPairs() []ssh.KeyPairSummary {
	row, ok := t.currentGroupRow()
	keys := t.keyPairs
	if ok && !row.isAll {
		want := row.name
		if row.isUngrouped {
			want = ""
		}
		keys = make([]ssh.KeyPairSummary, 0, len(t.keyPairs))
		for _, k := range t.keyPairs {
			if k.Group == want {
				keys = append(keys, k)
			}
		}
	}
	sorted := make([]ssh.KeyPairSummary, len(keys))
	copy(sorted, keys)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	if t.filterBox.Term() == "" {
		return sorted
	}
	out := make([]ssh.KeyPairSummary, 0, len(sorted))
	for _, k := range sorted {
		if t.filterBox.Matches(k.Name) {
			out = append(out, k)
		}
	}
	return out
}

// hostRefs returns the aliases of hosts whose identityKey is name.
func (t *keyPairTab) hostRefs(name string) []string {
	var refs []string
	for _, host := range t.hosts {
		if host.IdentityKey == name {
			refs = append(refs, host.Alias)
		}
	}
	sort.Strings(refs)
	return refs
}

// enterFilter 进入 `/` 过滤：清词重新开始（与 env/text/config/ssh 一致）。
func (t *keyPairTab) enterFilter() {
	t.filterBox.EnterFresh()
}

func (t *keyPairTab) handleFilterKeys(msg tea.KeyMsg) bool {
	clamp := func() {
		t.keyIndex = clamp(t.keyIndex, 0, len(t.visibleKeyPairs())-1)
		if t.keyIndex < 0 {
			t.keyIndex = 0
		}
	}
	switch msg.String() {
	case "esc":
		t.filterBox.Clear()
		clamp()
		return true
	case "enter":
		t.filterBox.Confirm()
		return true
	case "backspace":
		t.filterBox.Backspace()
		clamp()
		return true
	}
	if isPrintable(msg) {
		t.filterBox.Append(msg.String())
		clamp()
		return true
	}
	return false
}

func (t *keyPairTab) clampFocus() {
	t.keyIndex = clamp(t.keyIndex, 0, len(t.visibleKeyPairs())-1)
	if t.keyIndex < 0 {
		t.keyIndex = 0
	}
}

func (t *keyPairTab) SetSize(width, height int) {
	t.width, t.height = width, height
}

func (t *keyPairTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *keyPairTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}

func (t *keyPairTab) load() tea.Cmd {
	mgr := t.mgr.SSH
	return func() tea.Msg {
		st := perflog.Start("tui.load-keypair")
		if mgr == nil {
			st.End(true)
			return kpLoadedMsg{}
		}
		hosts, err := mgr.ListHosts()
		if err != nil {
			st.End(false)
			return kpLoadedMsg{err: err}
		}
		keyPairs, err := mgr.ListKeyPairs()
		if err != nil {
			st.End(false)
			return kpLoadedMsg{err: err}
		}
		values := make([]storage.HostEntry, 0, len(hosts))
		for _, host := range hosts {
			values = append(values, *host)
		}
		st.With("hosts", len(hosts), "keypairs", len(keyPairs)).End(true)
		return kpLoadedMsg{hosts: values, keyPairs: keyPairs}
	}
}

func (t *keyPairTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && t.form == nil && t.mode == kpModeNormal && t.filterBox.Active() {
		if t.handleFilterKeys(key) {
			return t, nil
		}
	}
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
		t.cancelMode()
		return t, warnToast("cancelled")
	}
	if t.form != nil {
		next, cmd := t.form.Update(msg)
		t.form = next
		return t, cmd
	}

	switch msg := msg.(type) {
	case kpLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.hosts = msg.hosts
		t.keyPairs = msg.keyPairs
		t.clamp()
		t.applyPendingJump()
		return t, nil

	case kpReloadMsg:
		t.cancelMode()
		if msg.keyName != "" {
			t.pendingJump = msg.keyName
		}
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd

	case kpPruneMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		if len(msg.candidates) == 0 {
			return t, okToast("no unreferenced materialized keys")
		}
		t.pendingPrune = msg.candidates
		t.mode = kpModePrune
		return t, nil

	case kpPruneDoneMsg:
		t.cancelMode()
		cmd := t.load()
		if msg.err != nil {
			text := fmt.Sprintf("prune: deleted %d of %d: %s", msg.deleted, msg.total, firstPruneError(msg.err))
			return t, tea.Batch(func() tea.Msg { return warnMsg{text: text} }, cmd)
		}
		return t, tea.Batch(okToast(fmt.Sprintf("deleted %d file(s)", msg.deleted)), cmd)

	case detailCloseMsg:
		t.detail = nil
		return t, nil

	case tea.KeyMsg:
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		if t.mode != kpModeNormal {
			return t.updateMode(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

// updateKey handles the browse/normal keymap.
func (t *keyPairTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		t.moveCursor(-1)
	case "down", "j":
		t.moveCursor(1)
	case "left", "h":
		if t.focus == kpPaneList {
			t.focus = kpPaneGroup
		}
	case "right", "l":
		if t.focus == kpPaneGroup {
			// env 侧栏范式：切入列表栏定位该组第一条。
			t.focus = kpPaneList
			t.keyIndex = 0
		}
	case "/":
		t.enterFilter()
		return t, nil
	case "ctrl+r":
		t.loaded = false
		return t, t.load()
	case "enter":
		if t.focus == kpPaneGroup {
			return t, nil
		}
		return t, t.openDetail()
	case "n", "i":
		if t.focus == kpPaneList {
			return t.enterImportKeyPair()
		}
	case "r":
		if t.focus == kpPaneList {
			return t.enterRenameKeyPair()
		}
	case "e":
		if t.focus == kpPaneList {
			return t.enterEditGroup()
		}
	case "d":
		if t.focus == kpPaneList {
			return t.enterDeleteKey()
		}
	case "m":
		if t.focus == kpPaneList {
			return t.enterMaterialize()
		}
	case "p":
		return t.enterPrune()
	case "g":
		t.jumpFocus(0)
	case "G":
		t.jumpFocus(t.focusListLen() - 1)
	case "pgup":
		t.jumpFocus(t.cursorForFocus() - pageStep(t.height))
	case "pgdown":
		t.jumpFocus(t.cursorForFocus() + pageStep(t.height))
	}
	return t, nil
}

// moveCursor 在焦点栏内移动条目光标；侧栏移动同时重置列表光标
// （切组后条目从头开始，与 env/config/ssh 的侧栏交互一致）。
func (t *keyPairTab) moveCursor(delta int) {
	if t.focus == kpPaneGroup {
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups())-1)
		t.keyIndex = 0
		return
	}
	t.keyIndex = clamp(t.keyIndex+delta, 0, len(t.visibleKeyPairs())-1)
}

// cursorForFocus / jumpFocus / focusListLen 支撑翻页与跳顶底。
func (t *keyPairTab) cursorForFocus() int {
	if t.focus == kpPaneGroup {
		return t.groupIndex
	}
	return t.keyIndex
}

func (t *keyPairTab) focusListLen() int {
	if t.focus == kpPaneGroup {
		return len(t.groups())
	}
	return len(t.visibleKeyPairs())
}

func (t *keyPairTab) jumpFocus(idx int) {
	n := t.focusListLen()
	if n == 0 || idx < 0 {
		idx = 0
	} else if idx > n-1 {
		idx = n - 1
	}
	if t.focus == kpPaneGroup {
		t.groupIndex = idx
		t.keyIndex = 0
		return
	}
	t.keyIndex = idx
}

// updateMode handles the delete/materialize confirmation modals.
func (t *keyPairTab) updateMode(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch t.mode {
	case kpModeDeleteKey:
		switch {
		case len(t.keyRefs) == 0 && (msg.String() == "enter" || msg.String() == "y"):
			return t, t.doDeleteKey(t.pendingKey, false)
		case len(t.keyRefs) > 0 && msg.String() == "F":
			return t, t.doDeleteKey(t.pendingKey, true)
		case msg.String() == "esc" || msg.String() == "n":
			t.cancelMode()
		}
	case kpModeMaterialize:
		switch msg.String() {
		case "enter", "y":
			return t, t.doMaterialize(t.pendingKey, t.pendingForce)
		case "esc", "n":
			t.cancelMode()
		}
	case kpModePrune:
		switch msg.String() {
		case "enter", "y":
			return t, t.doPrune()
		case "esc", "n":
			t.cancelMode()
			return t, warnToast("cancelled")
		}
	}
	return t, nil
}

// cancelMode clears every staged confirmation field.
func (t *keyPairTab) cancelMode() {
	t.mode = kpModeNormal
	t.pendingKey = ""
	t.pendingForce = false
	t.keyRefs = nil
	t.materializePath = ""
	t.pendingPrune = nil
}

// --- write flows ---

func (t *keyPairTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// enterImportKeyPair opens the import form; group 是自由文本归属字段
// （失焦不校验，与 host 表单的 group 同等），导入时写入记录。
func (t *keyPairTab) enterImportKeyPair() (Tab, tea.Cmd) {
	siblings := make([]string, 0, len(t.keyPairs))
	for _, k := range t.keyPairs {
		siblings = append(siblings, k.Name)
	}
	f := newForm("import keypair",
		formField{
			key: "name", label: "name", kind: formText, placeholder: "web-key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("name cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid name")
				}
				for _, name := range siblings {
					if name == v {
						return fmt.Errorf("keypair %s already exists", v)
					}
				}
				return nil
			},
		},
		formField{
			key: "path", label: "private key file", kind: formPath, placeholder: "~/.ssh/id_ed25519",
		},
		formField{
			key: "group", label: "group", kind: formText, placeholder: "prod",
		},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doImportKeyPair(
			strings.TrimSpace(values["name"]),
			strings.TrimSpace(values["path"]),
			strings.TrimSpace(values["group"]),
		)
	})
	return t, nil
}

func (t *keyPairTab) doImportKeyPair(name, path, group string) tea.Cmd {
	if path == "" {
		return warnToast("private key file path cannot be empty")
	}
	mgr := t.mgr.SSH
	mgrs := t.mgr
	return func() tea.Msg {
		if _, err := mgr.ImportKeyPairWithGroup(name, expandHome(path), group, false); err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "import failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "import")
		return kpReloadMsg{toast: "imported keypair " + name, keyName: name}
	}
}

func (t *keyPairTab) enterRenameKeyPair() (Tab, tea.Cmd) {
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair to rename")
	}
	siblings := make([]string, 0, len(t.keyPairs))
	for _, k := range t.keyPairs {
		siblings = append(siblings, k.Name)
	}
	old := key.Name
	f := newForm("rename keypair "+old,
		formField{
			key: "name", label: "new name", kind: formText, value: old, placeholder: "prod-key",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("name cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid name")
				}
				if v != old {
					for _, name := range siblings {
						if name == v {
							return fmt.Errorf("keypair %s already exists", v)
						}
					}
				}
				return nil
			},
		},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doRenameKeyPair(old, strings.TrimSpace(values["name"]))
	})
	return t, nil
}

func (t *keyPairTab) doRenameKeyPair(oldName, newName string) tea.Cmd {
	if oldName == newName {
		return warnToast("name unchanged")
	}
	mgr := t.mgr.SSH
	mgrs := t.mgr
	return func() tea.Msg {
		updated, err := mgr.RenameKeyPair(oldName, newName)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+oldName, false, "rename failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+newName, true, "rename "+oldName)
		toast := "renamed to " + newName
		if len(updated) > 0 {
			toast += fmt.Sprintf(" (updated %d host references)", len(updated))
		}
		return kpReloadMsg{toast: toast, keyName: newName}
	}
}

// enterEditGroup 打开仅含 group 的小表单：group 是唯一可就地编辑的
// keypair 元数据字段（key 材料不可在 TUI 修改）。
func (t *keyPairTab) enterEditGroup() (Tab, tea.Cmd) {
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair selected")
	}
	name := key.Name
	f := newForm("edit group — "+name,
		formField{
			key: "group", label: "group", kind: formText, value: key.Group, placeholder: "prod",
		},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doEditGroup(name, strings.TrimSpace(values["group"]))
	})
	return t, nil
}

func (t *keyPairTab) doEditGroup(name, group string) tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	return func() tea.Msg {
		err := mgr.UpdateKeyPair(name, func(k *storage.KeyPairEntry) error {
			k.Group = group
			return nil
		})
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "edit group failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "edit group")
		return kpReloadMsg{toast: "updated group for " + name, keyName: name}
	}
}

func (t *keyPairTab) enterDeleteKey() (Tab, tea.Cmd) {
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair to delete")
	}
	t.pendingKey = key.Name
	t.keyRefs = t.hostRefs(key.Name)
	t.mode = kpModeDeleteKey
	return t, nil
}

func (t *keyPairTab) doDeleteKey(name string, force bool) tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	refs := append([]string(nil), t.keyRefs...)
	t.cancelMode()
	return func() tea.Msg {
		cleared, err := mgr.DeleteKeyPair(name, force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "delete")
		toast := "deleted keypair " + name
		if force && len(cleared) > 0 {
			toast += fmt.Sprintf(" (cleared %d host references)", len(cleared))
		} else if force && len(refs) > 0 {
			toast += fmt.Sprintf(" (cleared %d host references)", len(refs))
		}
		return kpReloadMsg{toast: toast}
	}
}

// enterMaterialize stages a materialize, pre-computing whether the target file
// already exists so an overwrite needs the extra confirmation.
func (t *keyPairTab) enterMaterialize() (Tab, tea.Cmd) {
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair to materialize")
	}
	path, err := ssh.MaterializePath(key.Group, key.Name)
	if err != nil {
		err := err
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	force := false
	if _, statErr := os.Lstat(path); statErr == nil {
		force = true
	}
	t.pendingKey = key.Name
	t.pendingForce = force
	t.materializePath = path
	t.mode = kpModeMaterialize
	return t, nil
}

func (t *keyPairTab) doMaterialize(name string, force bool) tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	t.cancelMode()
	return func() tea.Msg {
		path, err := mgr.Materialize(name, force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "materialize failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "materialize")
		// Only the 落盘路径 is surfaced; the private key body never reaches the UI.
		return toastMsg{text: "written to " + path, level: toastSuccess}
	}
}

// enterPrune 异步取 PruneCandidates（design D3）：空清单 toast 直达，非空
// 进列表+确认同屏 modal。两栏均可用，不读游标。
func (t *keyPairTab) enterPrune() (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	if mgr == nil {
		return t, warnToast("SSH manager not available")
	}
	return t, func() tea.Msg {
		candidates, err := mgr.PruneCandidates()
		return kpPruneMsg{candidates: candidates, err: err}
	}
}

func (t *keyPairTab) doPrune() tea.Cmd {
	mgrs := t.mgr
	candidates := append([]ssh.PruneCandidate(nil), t.pendingPrune...)
	t.cancelMode()
	return func() tea.Msg {
		paths := make([]string, 0, len(candidates))
		for _, c := range candidates {
			paths = append(paths, c.Path)
		}
		deleted, err := ssh.DeletePrunedFiles(paths)
		ok := err == nil
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:prune", ok, fmt.Sprintf("prune %d 个文件", len(deleted)))
		return kpPruneDoneMsg{deleted: len(deleted), total: len(paths), err: err}
	}
}

func firstPruneError(err error) string {
	s := err.Error()
	s = strings.TrimPrefix(s, "prune: ")
	if i := strings.Index(s, "; "); i >= 0 {
		return s[:i]
	}
	return s
}

// --- lookups ---

func (t *keyPairTab) currentKey() (ssh.KeyPairSummary, bool) {
	keys := t.visibleKeyPairs()
	if t.keyIndex < 0 || t.keyIndex >= len(keys) {
		return ssh.KeyPairSummary{}, false
	}
	return keys[t.keyIndex], true
}

// --- detail ---

// openDetail shows the full metadata record for the focused keypair. The
// private key body is never loaded into the tab, and only the public half
// is shown.
func (t *keyPairTab) openDetail() tea.Cmd {
	key, ok := t.currentKey()
	if !ok {
		return warnToast("no keypair selected")
	}
	t.detail = newDetailOverlay("KeyPair "+key.Name, keyPairDetailLines(key))
	t.detail.SetSize(t.width, t.height)
	return nil
}

// keyPairDetailLines renders keypair metadata. The private key body is never
// loaded into the tab, and only the public half is shown.
func keyPairDetailLines(k ssh.KeyPairSummary) []string {
	fp := k.Fingerprint
	if fp == "" {
		fp = " (not derived; may be a passphrase-encrypted private key)"
	}
	lines := []string{
		"name:        " + k.Name,
		"fingerprint: " + fp,
		"group:       " + orDash(k.Group),
		"comment:     " + orDash(k.Comment),
		"imported:    " + k.ImportedAt.Local().Format("2006-01-02 15:04:05"),
		"public key:",
	}
	if k.PublicKey == "" {
		return append(lines, "  -")
	}
	return append(lines, "  "+k.PublicKey)
}

// --- cursor ---

// focusJump positions the cursor on a keypair (group, name). Group is accepted
// for interface parity with the other grouped tabs; the cursor lands on the
// name within the currently selected group view.
func (t *keyPairTab) focusJump(_, key string) {
	t.pendingJump = key
	t.applyPendingJump()
}

// applyPendingJump resolves a pending keypair name once the data is available
// (a just-finished write). 按过滤契约（tui-ux-filter）先清过滤保证目标可见。
func (t *keyPairTab) applyPendingJump() {
	if t.pendingJump == "" {
		return
	}
	t.filterBox.Clear()
	for i, k := range t.visibleKeyPairs() {
		if k.Name == t.pendingJump {
			t.keyIndex = i
			t.focus = kpPaneList
			t.pendingJump = ""
			break
		}
	}
}

func (t *keyPairTab) clamp() {
	groups := t.groups()
	if t.groupIndex >= len(groups) {
		t.groupIndex = len(groups) - 1
	}
	if t.groupIndex < 0 {
		t.groupIndex = 0
	}
	t.keyIndex = clamp(t.keyIndex, 0, len(t.visibleKeyPairs())-1)
	if t.keyIndex < 0 {
		t.keyIndex = 0
	}
}

// --- view ---

func (t *keyPairTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("KeyPair") + "\n" + truncateRunes("⚠ "+t.loadErr, maxInt(t.width, 1))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if t.loaded && len(t.keyPairs) == 0 && t.mode == kpModeNormal && t.form == nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("KeyPair"),
			emptyStateStyle.Render("no keypairs yet; press i to import a private key, then Ctrl+R to refresh"))
	}
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	} else if t.mode != kpModeNormal {
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

// viewBaseAt renders the two panes: 分组侧栏 → KeyPair 列表。宽度分配与
// env/config 同构：侧栏 width/4（cap 26，min 16），列表吃剩余（预留 5 列
// 栏间 chrome：1 间隙 + 两栏各 2 列边框）。
func (t *keyPairTab) viewBaseAt(height int) string {
	sideW := t.width / 4
	if sideW > 26 {
		sideW = 26
	}
	if sideW < 16 {
		sideW = 16
	}
	listW := t.width - sideW - 5
	if listW < 4 {
		listW = 4
	}

	// 加载态（env 范式）：几何常驻、框内提示，避免装载期布局跳动或误显空态。
	if !t.loaded {
		side := emptyStateStyle.Render("loading keypairs…")
		list := emptyStateStyle.Render("loading keypairs…")
		if t.focus == kpPaneGroup {
			side = activePaneStyle.Width(sideW).Height(height).Render(side)
			list = paneStyle.Width(listW).Height(height).Render(list)
		} else {
			side = paneStyle.Width(sideW).Height(height).Render(side)
			list = activePaneStyle.Width(listW).Height(height).Render(list)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, side, strings.Repeat(" ", 1), list)
	}

	keyLines := t.keyPairListLines(maxInt(listW-4, 8))

	side := t.renderSidebarPane(sideW, height)
	listTitle := fmt.Sprintf("KeyPairs (%d) · private keys masked", len(t.visibleKeyPairs()))
	if t.filterBox.Active() {
		listTitle += "  " + t.filterBox.Prompt()
	}
	list := windowedPane(listTitle, keyLines, t.keyIndex, height, listW)

	if t.focus == kpPaneGroup {
		side = activePaneStyle.Width(sideW).Height(height).Render(side)
		list = paneStyle.Width(listW).Height(height).Render(list)
	} else {
		side = paneStyle.Width(sideW).Height(height).Render(side)
		list = activePaneStyle.Width(listW).Height(height).Render(list)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, side, strings.Repeat(" ", 1), list)
}

// renderSidebarPane 渲染分组侧栏（复用共享 renderSidebar/SidebarRow，
// 与 env/config/ssh 视觉一致）。
func (t *keyPairTab) renderSidebarPane(width, height int) string {
	groups := t.groups()
	rows := make([]SidebarRow, 0, len(groups))
	for i, g := range groups {
		marker := " "
		if g.isAll {
			marker = "◯"
		}
		rows = append(rows, SidebarRow{
			Marker:   marker,
			Name:     g.name,
			Count:    g.count,
			Selected: i == t.groupIndex && t.focus == kpPaneGroup,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

// keyPairListLines renders one row per keypair: `name · fingerprint` plus an
// inline reference count (`被 N 个 Host 引用`); zero-reference keypairs show a
// faint 「未被引用」 hint. The list keeps name order regardless of refcount.
func (t *keyPairTab) keyPairListLines(width int) []string {
	keys := t.visibleKeyPairs()
	lines := make([]string, 0, len(keys))
	for i, key := range keys {
		label := key.Fingerprint
		if label == "" {
			label = "pubkey: none"
		}
		line := key.Name + " · " + label
		if refs := len(t.hostRefs(key.Name)); refs > 0 {
			line += fmt.Sprintf("  被 %d 个 Host 引用", refs)
		} else {
			line += "  " + faintTextStyle.Render("未被引用")
		}
		line = truncateWidth(line, width)
		lines = append(lines, cursorLine(line, i == t.keyIndex))
	}
	return lines
}

func (t *keyPairTab) renderModal() string {
	switch t.mode {
	case kpModeDeleteKey:
		if len(t.keyRefs) == 0 {
			return modalBox(t.width, t.height, "delete keypair "+t.pendingKey+"?", "the private key cannot be recovered after deletion.", "enter/y confirm · esc/n cancel")
		}
		var b strings.Builder
		b.WriteString("these hosts still reference the keypair:\n")
		for _, alias := range t.keyRefs {
			b.WriteString("  · " + alias + "\n")
		}
		b.WriteString("\ndeletion is refused by default. press F to force delete and clear identityKey on these hosts.")
		return modalBox(t.width, t.height, "keypair "+t.pendingKey+" still referenced", b.String(), "F force delete · esc cancel")
	case kpModeMaterialize:
		body := "will write the private key in plaintext to:\n" + t.materializePath + " (0600)."
		if t.pendingForce {
			body += "\n⚠ target file exists and will be overwritten."
		}
		return modalBox(t.width, t.height, "materialize keypair "+t.pendingKey, body, "enter/y confirm · esc/n cancel")
	case kpModePrune:
		var b strings.Builder
		for _, c := range t.pendingPrune {
			b.WriteString("  " + c.Path)
			if c.InVault {
				b.WriteString(" (keypair still in vault)")
			}
			b.WriteByte('\n')
		}
		return modalBox(t.width, t.height, "prune unreferenced keys?", strings.TrimRight(b.String(), "\n"),
			fmt.Sprintf("enter/y delete %d file(s) · esc/n cancel", len(t.pendingPrune)))
	}
	return ""
}

// Compile-time guard: *keyPairTab satisfies Tab.
var _ Tab = (*keyPairTab)(nil)
