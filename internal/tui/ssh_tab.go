package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// sshTab is the editable browser for host records. Hosts support create/edit/
// delete/export; keypair metadata (name/fingerprint) is loaded only to render
// the host rows' inline `key:name(fp)` reference and the host form's keypair
// selector — keypair management lives in the KeyPair Tab. Private-key content
// is never loaded into the tab model.
type sshTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	hosts     []storage.HostEntry
	keyPairs  []ssh.KeyPairSummary // 仅用于 host 行内 key: 片段与表单选择器
	filterBox Filter               // `/` 过滤 Host 栏（alias/hostname/tags/group 标识）
	sel       List                 // 仅承载 host 多选集（space 勾选 / a 全选可见）

	// pendingBatchHosts 批量删除确认页暂存的 host 别名列表。
	pendingBatchHosts []string
	focus             sshPane // 两栏焦点：分组侧栏 / Host 栏
	groupIndex        int     // 侧栏选中组（groups() 的下标）
	hostIndex         int
	loadErr           string
	detail            *detailOverlay
	// pendingJump holds a host alias requested by the global search before the
	// tab finished loading, since there is nothing to point at yet.
	pendingJump string

	// form 非 nil 时表示打开了一个结构化表单（host 编辑、导出路径）；
	// formSubmit 是提交后的动作。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode          sshMode
	pendingHost   string // host staged for delete
	exportLabel   string
	exportContent string
	exportScroll  int // 预览正文行偏移（超高片段可 ↑↓ 滚动，不裁切丢失）

	// pendingApply 暂存 apply 导出确认框的数据：交给 Manager.Apply 的
	// 过滤条件、审计 detail（与 CLI 同口径）与按键时 Render 预算出的计数。
	pendingApply *applyConfirm

	// pendingUnexport 暂存 unexport 确认框的预检结果（注册行 / 片段数）。
	pendingUnexport *unexportConfirm
}

// unexportConfirm 是 unexport 确认框的预检状态（design D2）。
type unexportConfirm struct {
	registered bool
	fragments  int
}

// applyConfirm 是 apply 导出确认框的全部状态（design D2）：执行前用
// Render 预算组片段数/待落盘数/Include 状态/warning 计数，enter/y 确认后
// 按 filter 调 Manager.Apply（design D3）。
type applyConfirm struct {
	filter      ssh.RenderFilter
	label       string // 确认框标题的作用域描述
	detail      string // 审计 detail，与 CLI `host export` 同口径
	groups      int    // 将重建的组片段数（len(Render.Order)）
	pendingKeys int    // Referenced 中落盘目标尚不存在的私钥数（Lstat 同规则）
	warnings    int    // Render 级 warning 计数（明细指路 CLI）
	include     bool   // ~/.ssh/config 已注册 senv Include 行
}

// sshMode is the tab's confirmation/preview state. Every non-normal mode owns
// the keyboard (InputMode) so global shortcuts cannot interrupt a decision.
type sshMode int

const (
	sshModeNormal sshMode = iota
	sshModeDeleteHost
	sshModeExportPreview
	sshModeBatchDeleteHost
	sshModeApplyConfirm
	sshModeUnexport
)

// sshPane 是 SSH Tab 两栏的焦点栏位。左右键（h/l）在
// 侧栏 ↔ Host 之间移动，与 env/config 的侧栏范式一致
// （切组/切入条目栏时重置条目光标）。
type sshPane int

const (
	paneGroup sshPane = iota
	paneHost
)

// sshGroupRow 是分组侧栏一行的数据。组排序：All 伪组置顶 →
// 组名字母序 → 「未分组」置底（仅在有未归类 host 时出现）。
type sshGroupRow struct {
	name        string
	isAll       bool
	isUngrouped bool
	count       int
}

const (
	sshAllLabel       = "All"
	sshUngroupedLabel = "未分组"
)

type sshLoadedMsg struct {
	hosts    []storage.HostEntry
	keyPairs []ssh.KeyPairSummary
	err      error
}

// sshReloadMsg reports a successful vault write; the tab reloads and parks the
// cursor on the named host so the effect of the write is visible.
type sshReloadMsg struct {
	toast     string
	hostAlias string
}

// sshExportMsg carries a rendered OpenSSH fragment for preview before writing.
type sshExportMsg struct {
	label   string
	content string
	err     error
}

// sshUnexportStateMsg 是 UnexportState 预检的异步结果。
type sshUnexportStateMsg struct {
	registered bool
	fragments  int
	err        error
}

func newSSHTab(mgr Managers) *sshTab {
	return &sshTab{mgr: mgr, focus: paneHost}
}

func (t *sshTab) Title() string { return "SSH" }

func (t *sshTab) Bindings() []KeyAction {
	if t.detail != nil {
		return detailBindings()
	}
	if t.form != nil {
		return formBindings()
	}
	if t.filterBox.Active() {
		return filterBindings(true)
	}
	switch t.mode {
	case sshModeDeleteHost, sshModeBatchDeleteHost, sshModeApplyConfirm, sshModeUnexport:
		return confirmBindings()
	case sshModeExportPreview:
		return []KeyAction{
			actUp, actDown, actTop, actBottom, actPageUp, actPageDn,
			{[]string{"w"}, "write file", grpConfirm, false},
			{[]string{"esc"}, "cancel", grpConfirm, false},
		}
	}
	nav := navBindings(true)
	nav = append(nav, actDetail)
	unexport := KeyAction{[]string{"u"}, "unexport", grpItem, false}
	if t.focus == paneHost {
		return append(append(nav, actNew, actEdit, actDelete, actSelect, actSelectAll, actExport, actApply, unexport), actRefresh, actFilter)
	}
	return append(nav, actExport, actApply, unexport, actRefresh, actFilter)
}

func (t *sshTab) InputMode() bool {
	return t.form != nil || t.mode != sshModeNormal || t.filterBox.Active()
}

// groups 由当前 host 集合派生侧栏行（数据单一来源，渲染/导航共用）。
// 排序：All 伪组置顶 → 组名字母序 → 「未分组」置底（仅在有未归类 host 时）。
func (t *sshTab) groups() []sshGroupRow {
	counts := map[string]int{}
	ungrouped := 0
	for _, h := range t.hosts {
		if h.Group == "" {
			ungrouped++
		} else {
			counts[h.Group]++
		}
	}
	rows := make([]sshGroupRow, 0, len(counts)+2)
	rows = append(rows, sshGroupRow{name: sshAllLabel, isAll: true, count: len(t.hosts)})
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
func (t *sshTab) currentGroupRow() (sshGroupRow, bool) {
	groups := t.groups()
	if t.groupIndex < 0 || t.groupIndex >= len(groups) {
		return sshGroupRow{}, false
	}
	return groups[t.groupIndex], true
}

// selectGroupName 把侧栏选中组切到指定组名（"" = 未分组）；组不存在时
// 保持现状（调用方用于搜索 jump 等跨组定位）。
func (t *sshTab) selectGroupName(name string) {
	for i, g := range t.groups() {
		if g.isUngrouped && name == "" {
			t.groupIndex = i
			return
		}
		if !g.isAll && !g.isUngrouped && g.name == name {
			t.groupIndex = i
			return
		}
	}
}

// visibleHosts 返回选中组内、再经 `/` 过滤后的 Host 栏可见列表（组内按
// 别名字典序）。空词 = 全量；All 伪组 = 全部 host。
func (t *sshTab) visibleHosts() []storage.HostEntry {
	row, ok := t.currentGroupRow()
	hosts := t.hosts
	if ok && !row.isAll {
		want := row.name
		if row.isUngrouped {
			want = ""
		}
		hosts = make([]storage.HostEntry, 0, len(t.hosts))
		for _, h := range t.hosts {
			if h.Group == want {
				hosts = append(hosts, h)
			}
		}
	}
	// 组内（及 All 视图）按别名字典序；拷贝排序不触碰 t.hosts。
	sorted := make([]storage.HostEntry, len(hosts))
	copy(sorted, hosts)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Alias < sorted[j].Alias })
	if t.filterBox.Term() == "" {
		return sorted
	}
	out := make([]storage.HostEntry, 0, len(sorted))
	for _, h := range sorted {
		if t.filterBox.Matches(h.Alias + " " + h.Hostname + " " + h.Group + " " + strings.Join(h.Tags, " ")) {
			out = append(out, h)
		}
	}
	return out
}

// enterFilter 进入 `/` 过滤：清词重新开始，与 env/text/config 一致。
func (t *sshTab) enterFilter() {
	t.filterBox.EnterFresh()
}

// handleFilterKeys 处理过滤输入态按键；返回 true 表示按键已被消费。
func (t *sshTab) handleFilterKeys(msg tea.KeyMsg) bool {
	clamp := func() {
		t.hostIndex = clamp(t.hostIndex, 0, len(t.visibleHosts())-1)
		if t.hostIndex < 0 {
			t.hostIndex = 0
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

// visibleHostAliases 返回可见 host 的别名（计数提示用）。
func (t *sshTab) visibleHostAliases() []string {
	hosts := t.visibleHosts()
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h.Alias)
	}
	return out
}

// clampFocus 把 Host 栏条目光标收回可见范围（组光标在 clamp 处理）。
func (t *sshTab) clampFocus() {
	t.hostIndex = clamp(t.hostIndex, 0, len(t.visibleHosts())-1)
	if t.hostIndex < 0 {
		t.hostIndex = 0
	}
}

func (t *sshTab) SetSize(width, height int) {
	t.width, t.height = width, height
	if t.mode == sshModeExportPreview {
		t.exportScroll = clamp(t.exportScroll, 0, t.exportPreviewMaxScroll())
	}
}

func (t *sshTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *sshTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}

func (t *sshTab) load() tea.Cmd {
	mgr := t.mgr.SSH
	return func() tea.Msg {
		st := perflog.Start("tui.load-ssh")
		if mgr == nil {
			st.End(true)
			return sshLoadedMsg{}
		}
		hosts, err := mgr.ListHosts()
		if err != nil {
			st.End(false)
			return sshLoadedMsg{err: err}
		}
		keyPairs, err := mgr.ListKeyPairs()
		if err != nil {
			st.End(false)
			return sshLoadedMsg{err: err}
		}
		values := make([]storage.HostEntry, 0, len(hosts))
		for _, host := range hosts {
			values = append(values, *host)
		}
		st.With("hosts", len(hosts), "keypairs", len(keyPairs)).End(true)
		return sshLoadedMsg{hosts: values, keyPairs: keyPairs}
	}
}

func (t *sshTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && t.form == nil && t.mode == sshModeNormal && t.filterBox.Active() {
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
	case sshLoadedMsg:
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

	case sshReloadMsg:
		t.cancelMode()
		if msg.hostAlias != "" {
			t.pendingJump = msg.hostAlias
		}
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd

	case sshExportMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.exportLabel = msg.label
		t.exportContent = msg.content
		t.exportScroll = 0
		t.mode = sshModeExportPreview
		return t, nil

	case sshUnexportStateMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		if !msg.registered && msg.fragments == 0 {
			return t, okToast("nothing to unexport")
		}
		t.pendingUnexport = &unexportConfirm{registered: msg.registered, fragments: msg.fragments}
		t.mode = sshModeUnexport
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
		if t.mode != sshModeNormal {
			return t.updateMode(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

// updateKey handles the browse/normal keymap.
func (t *sshTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		t.moveCursor(-1)
	case "down", "j":
		t.moveCursor(1)
	case "left", "h":
		if t.focus == paneHost {
			t.focus = paneGroup
		}
	case "right", "l":
		if t.focus == paneGroup {
			// env 侧栏范式：切入条目栏定位该组第一条。
			t.focus = paneHost
			t.hostIndex = 0
		}
	case "/":
		t.enterFilter()
		return t, nil
	case "ctrl+r":
		t.loaded = false
		return t, t.load()
	case "enter":
		if t.focus == paneGroup {
			return t, nil
		}
		return t, t.openDetail()
	case "n":
		if t.focus == paneHost {
			return t.enterHostForm(nil)
		}
	case " ", "space":
		if t.focus == paneHost {
			if host, ok := t.currentHost(); ok {
				t.sel.Toggle(host.Alias)
			}
		}
	case "a":
		if t.focus == paneHost {
			keys := make([]string, 0, len(t.visibleHosts()))
			for _, h := range t.visibleHosts() {
				keys = append(keys, h.Alias)
			}
			t.sel.SelectVisible(keys)
		}
	case "x":
		if t.focus == paneHost && t.sel.SelectionCount() > 1 {
			return t.enterBatchHostExport()
		}
		return t.enterExport()
	case "A":
		return t.enterApply()
	case "u":
		return t.enterUnexport()
	case "d":
		if t.focus == paneHost && t.sel.SelectionCount() > 1 {
			return t.enterBatchDeleteHosts()
		}
		return t.enterDelete()
	case "e":
		if t.focus == paneHost {
			if t.sel.SelectionCount() > 1 {
				return t, warnToast("multiple hosts selected: narrow to a single selection to edit")
			}
			host, ok := t.currentHost()
			if !ok {
				return t, warnToast("no host selected")
			}
			return t.enterHostForm(&host)
		}
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

// moveCursor 在焦点栏内移动条目光标；侧栏移动同时重置 Host 栏光标
// （切组后条目从头开始，与 env/config 的侧栏交互一致）。
func (t *sshTab) moveCursor(delta int) {
	if t.focus == paneGroup {
		t.groupIndex = clamp(t.groupIndex+delta, 0, len(t.groups())-1)
		t.hostIndex = 0
		return
	}
	t.hostIndex = clamp(t.hostIndex+delta, 0, len(t.visibleHosts())-1)
}

// cursorForFocus / jumpFocus / focusListLen 支撑翻页与跳顶底。
func (t *sshTab) cursorForFocus() int {
	if t.focus == paneGroup {
		return t.groupIndex
	}
	return t.hostIndex
}

func (t *sshTab) focusListLen() int {
	if t.focus == paneGroup {
		return len(t.groups())
	}
	// 游标语义 = 过滤可见列表上的位置（与 hostListLines/currentHost 一致）。
	return len(t.visibleHosts())
}

func (t *sshTab) jumpFocus(idx int) {
	n := t.focusListLen()
	if n == 0 || idx < 0 {
		idx = 0
	} else if idx > n-1 {
		idx = n - 1
	}
	if t.focus == paneGroup {
		t.groupIndex = idx
		t.hostIndex = 0
		return
	}
	t.hostIndex = idx
}

// updateMode handles the delete/export confirmation modals.
func (t *sshTab) updateMode(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch t.mode {
	case sshModeDeleteHost:
		switch msg.String() {
		case "enter", "y":
			return t.doDeleteHost(t.pendingHost)
		case "esc", "n":
			t.cancelMode()
		}
	case sshModeBatchDeleteHost:
		switch msg.String() {
		case "enter", "y":
			targets := t.pendingBatchHosts
			t.cancelMode()
			t.sel.ClearSelection() // 提交即清空多选集
			return t, t.doBatchDeleteHosts(targets)
		case "esc", "n":
			t.cancelMode()
		}
	case sshModeExportPreview:
		switch msg.String() {
		case "w":
			content := t.exportContent
			t.cancelMode()
			return t.enterExportPathForm(content)
		case "esc":
			t.cancelMode()
		case "up", "k":
			t.exportScroll--
		case "down", "j":
			t.exportScroll++
		case "pgup":
			t.exportScroll -= t.exportPreviewPageSize()
		case "pgdown":
			t.exportScroll += t.exportPreviewPageSize()
		case "g", "home":
			t.exportScroll = 0
		case "G", "end":
			t.exportScroll = t.exportPreviewMaxScroll()
		}
		t.exportScroll = clamp(t.exportScroll, 0, t.exportPreviewMaxScroll())
	case sshModeApplyConfirm:
		switch msg.String() {
		case "enter", "y":
			return t, t.doApplyExport()
		case "esc", "n":
			t.cancelMode()
		}
	case sshModeUnexport:
		switch msg.String() {
		case "enter", "y":
			return t, t.doUnexport()
		case "esc", "n":
			t.cancelMode()
			return t, warnToast("cancelled")
		}
	}
	return t, nil
}

// cancelMode clears every staged confirmation field.
func (t *sshTab) cancelMode() {
	t.mode = sshModeNormal
	t.pendingHost = ""
	t.exportLabel = ""
	t.exportContent = ""
	t.exportScroll = 0
	t.pendingApply = nil
	t.pendingUnexport = nil
}

// exportPreviewLines 把预览正文拆成行（末尾空行保留，与片段字节一致）。
func (t *sshTab) exportPreviewLines() []string {
	if t.exportContent == "" {
		return nil
	}
	return strings.Split(t.exportContent, "\n")
}

// exportPreviewPageSize 是预览弹层正文可见行数：扣掉标题与底栏 hint。
func (t *sshTab) exportPreviewPageSize() int {
	if t.height <= 4 {
		return 1
	}
	return t.height - 4
}

func (t *sshTab) exportPreviewMaxScroll() int {
	n := len(t.exportPreviewLines())
	page := t.exportPreviewPageSize()
	if n <= page {
		return 0
	}
	return n - page
}

// --- host write flows ---

// enterDelete stages the focused host or keypair for deletion, gathering the
// keypair's referencing hosts so the confirmation can list them.
// enterBatchDeleteHosts 多选集批量删除 host：一次确认列全部目标。
func (t *sshTab) enterBatchDeleteHosts() (Tab, tea.Cmd) {
	targets := t.selectedHostAliases()
	if len(targets) == 0 {
		return t, warnToast("selection is empty")
	}
	t.pendingBatchHosts = targets
	t.mode = sshModeBatchDeleteHost
	return t, nil
}

// selectedHostAliases 返回多选集命中的可见 host 别名。
func (t *sshTab) selectedHostAliases() []string {
	var out []string
	for _, h := range t.visibleHosts() {
		if t.sel.IsSelected(h.Alias) {
			out = append(out, h.Alias)
		}
	}
	return out
}

// doBatchDeleteHosts 逐条删除；单条失败不中止其余。
func (t *sshTab) doBatchDeleteHosts(aliases []string) tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	return func() tea.Msg {
		failed := 0
		for _, alias := range aliases {
			if err := mgr.DeleteHost(alias); err != nil {
				failed++
				recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, false, "delete failed")
				continue
			}
			recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, true, "delete")
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("batch delete finished, %d failed", failed)}
		}
		return sshReloadMsg{toast: fmt.Sprintf("deleted %d hosts", len(aliases))}
	}
}

// enterBatchHostExport 批量导出：选目录，每个 host 写入 <目录>/<alias>.conf。
func (t *sshTab) enterBatchHostExport() (Tab, tea.Cmd) {
	targets := t.selectedHostAliases()
	if len(targets) == 0 {
		return t, warnToast("selection is empty")
	}
	f := newForm("batch export host snippets",
		formField{
			key: "dir", label: "output directory (your own)", kind: formPath, placeholder: "~/ssh-snippets",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("output directory cannot be empty")
				}
				return rejectSenvTreePath(v)
			},
		},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		dir := strings.TrimSpace(values["dir"])
		targets := t.selectedHostAliases()
		t.sel.ClearSelection()
		return t.doBatchExportHosts(dir, targets)
	})
	return t, nil
}

// doBatchExportHosts 逐个导出；单条失败不中止其余并汇总。
func (t *sshTab) doBatchExportHosts(dir string, aliases []string) tea.Cmd {
	mgr := t.mgr.SSH
	return func() tea.Msg {
		failed := 0
		for _, alias := range aliases {
			// TUI 不呈现缺失 keypair 的 warning（design Non-goal）
			rr, err := mgr.Render(ssh.RenderFilter{Host: alias})
			var content string
			if err == nil {
				for _, g := range rr.Order {
					content += rr.Fragments[g]
				}
			}
			if err != nil {
				failed++
				continue
			}
			path := filepath.Join(expandHome(dir), alias+".conf")
			if err := storage.WriteSensitiveFile(path, []byte(content), 0o700, 0o600); err != nil {
				failed++
			}
		}
		if failed > 0 {
			return warnMsg{text: fmt.Sprintf("batch export finished, %d failed", failed)}
		}
		return sshReloadMsg{toast: fmt.Sprintf("exported %d hosts to %s", len(aliases), dir)}
	}
}

func (t *sshTab) enterDelete() (Tab, tea.Cmd) {
	if t.focus != paneHost {
		return t, nil
	}
	host, ok := t.currentHost()
	if !ok {
		return t, warnToast("no host to delete")
	}
	t.pendingHost = host.Alias
	t.mode = sshModeDeleteHost
	return t, nil
}

// enterExport renders the selected host fragment (or every host when the
// group sidebar is focused) and shows it for preview before any write.
func (t *sshTab) enterExport() (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	alias := ""
	label := "all hosts"
	if t.focus == paneHost {
		host, ok := t.currentHost()
		if !ok {
			return t, warnToast("no host to export")
		}
		alias = host.Alias
		label = "host " + alias
	}
	return t, func() tea.Msg {
		// TUI 预览不呈现缺失 keypair 的 warning（design Non-goal）
		rr, err := mgr.Render(ssh.RenderFilter{Host: alias})
		var content string
		if err == nil {
			for _, g := range rr.Order {
				content += rr.Fragments[g]
			}
		}
		return sshExportMsg{label: label, content: content, err: err}
	}
}

// --- apply export flow (design D1–D3) ---

// enterApply 把 `A` 键落到具体作用域并弹出确认框（design D1/D2）：host 栏
// = 游标 host 所在组（Apply 的写入单元是整组片段，与 CLI 一致）；侧栏 =
// 选中组，All = 全量重建。预算用 Render 同步计算——Render 失败（如
// proxyJump 悬空）按键直接报错、不进确认框，零副作用（与 CLI 两阶段一致）。
func (t *sshTab) enterApply() (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	if mgr == nil {
		return t, warnToast("SSH manager not available")
	}
	var filter ssh.RenderFilter
	var label, detail string
	if t.focus == paneHost {
		host, ok := t.currentHost()
		if !ok {
			return t, warnToast("no host selected")
		}
		filter = ssh.RenderFilter{Host: host.Alias}
		detail = "export --host " + host.Alias
		if host.Group == "" {
			// Apply 对未分组 host 的 Host 过滤会归一化成空组过滤，即全量
			// 渲染（internal/ssh 既有语义，消费方不改）：标题如实呈现。
			label = fmt.Sprintf("all groups (host %s is ungrouped)", host.Alias)
		} else {
			label = fmt.Sprintf("group %s (host %s)", host.Group, host.Alias)
		}
	} else {
		row, ok := t.currentGroupRow()
		if !ok {
			return t, warnToast("no group selected")
		}
		switch {
		case row.isAll:
			label = "all hosts"
			detail = "export"
		case row.isUngrouped:
			// design D1 只定义 All/命名组两种作用域；空组在 RenderFilter
			// 里与全量同义，直接映射会让「未分组」看起来像单组重建，拒止
			// 并指引走 All（全量同样重建 _ungrouped 片段）。
			return t, warnToast("ungrouped hosts rebuild via All (full apply)")
		default:
			filter = ssh.RenderFilter{Group: row.name}
			label = "group " + row.name
			detail = "export --group " + row.name
		}
	}

	// 预算与 Apply 同一归一化规则：Host 过滤先映射到所在组再 Render。
	eff := filter
	if eff.Host != "" {
		if host, ok := t.hostByAlias(eff.Host); ok {
			eff = ssh.RenderFilter{Group: host.Group}
		}
	}
	rr, err := mgr.Render(eff)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	pending := 0
	for _, entry := range rr.Referenced {
		target, err := ssh.MaterializePath(entry.Group, entry.Name)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			pending++
		}
	}
	t.pendingApply = &applyConfirm{
		filter:      filter,
		label:       label,
		detail:      detail,
		groups:      len(rr.Order),
		pendingKeys: pending,
		warnings:    len(rr.Warnings),
		include:     includeRegistered(),
	}
	t.mode = sshModeApplyConfirm
	return t, nil
}

// doApplyExport 异步执行 Manager.Apply（design D3，同 doBatchExportHosts
// 的 tea.Cmd 模式）；结果以摘要 toast 呈现，部分失败走红色错误条 + 首条
// 错误，已成功项不回滚（与 CLI 语义一致）。
func (t *sshTab) doApplyExport() tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	st := t.pendingApply
	t.cancelMode()
	return func() tea.Msg {
		res, err := mgr.Apply(st.filter)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHHost, "host:export", false, st.detail+" 失败")
			return errMsg{err: fmt.Errorf("apply export failed: %s", firstApplyError(res, err))}
		}
		recordAudit(mgrs, session.AuditOpSSHHost, "host:export", true, st.detail)
		return toastMsg{text: applySummary(res), level: toastSuccess}
	}
}

// enterUnexport 异步预检 UnexportState（design D2）：无可撤回项直接 toast，
// 否则进入确认框。两栏均可用，不读游标。
func (t *sshTab) enterUnexport() (Tab, tea.Cmd) {
	if t.mgr.SSH == nil {
		return t, warnToast("SSH manager not available")
	}
	return t, func() tea.Msg {
		registered, fragments, err := ssh.UnexportState()
		return sshUnexportStateMsg{registered: registered, fragments: fragments, err: err}
	}
}

// doUnexport 异步执行 Manager.Unexport，与 CLI 同一编排；结果 toast 实报。
func (t *sshTab) doUnexport() tea.Cmd {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	t.cancelMode()
	return func() tea.Msg {
		unregistered, groupsRemoved, err := mgr.Unexport()
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHHost, "host:unexport", false, "unexport 失败")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHHost, "host:unexport", true, "unexport")
		return toastMsg{text: unexportSummary(unregistered, groupsRemoved), level: toastSuccess}
	}
}

func unexportSummary(unregistered, groupsRemoved bool) string {
	if !unregistered && !groupsRemoved {
		return "nothing to unexport"
	}
	var parts []string
	if unregistered {
		parts = append(parts, "include removed")
	}
	if groupsRemoved {
		parts = append(parts, "group fragments deleted")
	}
	return "unexported: " + strings.Join(parts, ", ")
}

// applySummary 把 ApplyResult 折叠成单行摘要（design D3 固定格式）。
func applySummary(res *ssh.ApplyResult) string {
	include := "include existed"
	if res.Registered {
		include = "include registered"
	}
	return fmt.Sprintf("applied: %d groups, %d keys materialized, %d skipped, %s, %d warnings",
		len(res.Written), len(res.Materialized), len(res.KeysSkipped), include, len(res.Warnings))
}

// firstApplyError 取 Apply 部分失败汇总的首条错误（res 缺失时退回 err 本身）。
func firstApplyError(res *ssh.ApplyResult, err error) string {
	if res != nil && len(res.Errors) > 0 {
		return res.Errors[0]
	}
	return err.Error()
}

// includeRegistered 报告 ~/.ssh/config 是否已含 senv 的 Include 行（逐行
// 精确匹配，与 ssh.RegisterInclude 的判定同规则；只读，不创建文件）。
func includeRegistered() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == ssh.IncludeLine {
			return true
		}
	}
	return false
}

// rejectSenvTreePath 校验导出目标不得落在 senv 自有的 ~/.ssh/senv 树内
// （design D4）：该树由 apply 导出（`A` / `senv host export`）全权维护，
// 外来文件会被幽灵清理删除。展开 `~` 后做前缀判定；纯前端校验，不挡
// CLI --output（CLI 纯渲染是用户显式意图）。
func rejectSenvTreePath(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil // 空值交给各表单的非空校验
	}
	root, err := ssh.SenvDir()
	if err != nil {
		return nil // 定位不到 senv 树时不误判，交给写入阶段报错
	}
	target := expandHome(v)
	if target == root || strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return fmt.Errorf("~/.ssh/senv is owned by apply export (press A); foreign files there are cleaned as ghosts — pick your own path")
	}
	return nil
}

func (t *sshTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// enterHostForm opens the host create/edit form. existing == nil means create
// (alias is editable); otherwise the alias is fixed and only shown in the
// title, because renaming a host alias is out of scope.
func (t *sshTab) enterHostForm(existing *storage.HostEntry) (Tab, tea.Cmd) {
	keyOptions := make([]string, 0, len(t.keyPairs))
	for _, k := range t.keyPairs {
		keyOptions = append(keyOptions, k.Name)
	}
	hostOptions := make([]string, 0, len(t.hosts))
	for _, h := range t.hosts {
		if existing != nil && h.Alias == existing.Alias {
			continue
		}
		hostOptions = append(hostOptions, h.Alias)
	}
	knownHost := func(alias string) bool {
		for _, h := range t.hosts {
			if h.Alias == alias {
				return true
			}
		}
		return false
	}
	knownKey := func(name string) bool {
		for _, k := range t.keyPairs {
			if k.Name == name {
				return true
			}
		}
		return false
	}

	base := storage.HostEntry{}
	if existing != nil {
		base = *existing
	}

	title := "new host"
	fields := make([]formField, 0, 8)
	if existing == nil {
		title = "new host"
		fields = append(fields, formField{
			key: "alias", label: "alias", kind: formText, value: base.Alias, placeholder: "web",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("alias cannot be empty")
				}
				if err := storage.ValidateName(v); err != nil {
					return fmt.Errorf("invalid alias")
				}
				if knownHost(v) {
					return fmt.Errorf("host %s already exists", v)
				}
				return nil
			},
		})
	} else {
		title = "edit host " + existing.Alias
	}
	fields = append(fields,
		formField{
			key: "hostname", label: "hostname", kind: formText, value: base.Hostname, placeholder: "10.0.0.9",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return fmt.Errorf("hostname cannot be empty")
				}
				if strings.ContainsAny(v, "\r\n\x00") {
					return fmt.Errorf("hostname cannot contain newlines or NUL")
				}
				return nil
			},
		},
		formField{
			key: "user", label: "user", kind: formText, value: base.User, placeholder: "deploy",
			validate: func(v string) error {
				if strings.ContainsAny(v, "\r\n\x00") {
					return fmt.Errorf("user cannot contain newlines or NUL")
				}
				return nil
			},
		},
		formField{
			key: "port", label: "port", kind: formText, value: portValue(base.Port), placeholder: "22",
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 || n > 65535 {
					return fmt.Errorf("port must be an integer between 0 and 65535")
				}
				return nil
			},
		},
		formField{
			key: "proxyJump", label: "proxyJump", kind: formRef, value: base.ProxyJump, options: hostOptions, optional: true,
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				if existing != nil && v == existing.Alias {
					return fmt.Errorf("host cannot use itself as proxyJump")
				}
				if !knownHost(v) {
					return fmt.Errorf("proxyJump %s does not exist", v)
				}
				return nil
			},
		},
		formField{
			key: "identityKey", label: "identityKey", kind: formRef, value: base.IdentityKey, options: keyOptions, optional: true,
			validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				if !knownKey(v) {
					return fmt.Errorf("keypair %s does not exist", v)
				}
				return nil
			},
		},
		formField{
			// group 是自由文本归属字段：失焦不校验（与 tags 同等），由侧栏
			// 按值聚合；空值 = 未分组。
			key: "group", label: "group", kind: formText, value: base.Group, placeholder: "prod",
		},
		formField{
			key: "tags", label: "tags", kind: formText, value: strings.Join(base.Tags, ", "), placeholder: "prod, web",
		},
		optionalDescriptionField(base.Description),
		formField{
			key: "extra", label: "extra", kind: formEditor, value: renderExtraText(base.Extra),
			validate: func(v string) error {
				_, err := parseExtraText(v)
				return err
			},
		},
	)
	f := newForm(title, fields...)
	f.editExternal = func(index int, current string) tea.Cmd {
		return externalEditorCmd(index, "senv-host-extra-*", current)
	}
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doSubmitHost(existing, values)
	})
	return t, nil
}

func (t *sshTab) doSubmitHost(existing *storage.HostEntry, values map[string]string) tea.Cmd {
	port := 0
	if raw := strings.TrimSpace(values["port"]); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			err := fmt.Errorf("invalid port: %s", raw)
			return func() tea.Msg { return errMsg{err: err} }
		}
		port = n
	}
	extra, err := parseExtraText(values["extra"])
	if err != nil {
		err := err
		return func() tea.Msg { return errMsg{err: err} }
	}
	entry := storage.HostEntry{
		Hostname:    strings.TrimSpace(values["hostname"]),
		User:        strings.TrimSpace(values["user"]),
		Port:        port,
		ProxyJump:   strings.TrimSpace(values["proxyJump"]),
		IdentityKey: strings.TrimSpace(values["identityKey"]),
		Group:       strings.TrimSpace(values["group"]),
		Tags:        parseTagsText(values["tags"]),
		Description: strings.TrimSpace(values["description"]),
		Extra:       extra,
	}
	mgr := t.mgr.SSH
	mgrs := t.mgr
	if existing == nil {
		alias := strings.TrimSpace(values["alias"])
		entry.Alias = alias
		return func() tea.Msg {
			if err := mgr.AddHost(&entry); err != nil {
				recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, false, "add failed")
				return errMsg{err: err}
			}
			recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, true, "add")
			return sshReloadMsg{toast: "created host " + alias, hostAlias: alias}
		}
	}
	alias := existing.Alias
	return func() tea.Msg {
		err := mgr.UpdateHost(alias, func(h *storage.HostEntry) error {
			h.Hostname = entry.Hostname
			h.User = entry.User
			h.Port = entry.Port
			h.ProxyJump = entry.ProxyJump
			h.IdentityKey = entry.IdentityKey
			h.Group = entry.Group
			h.Tags = entry.Tags
			h.Description = entry.Description
			h.Extra = entry.Extra
			return nil
		})
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, false, "edit failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, true, "edit")
		return sshReloadMsg{toast: "updated host " + alias, hostAlias: alias}
	}
}

func (t *sshTab) doDeleteHost(alias string) (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	t.cancelMode()
	return t, func() tea.Msg {
		if err := mgr.DeleteHost(alias); err != nil {
			recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHHost, "host:"+alias, true, "delete")
		return sshReloadMsg{toast: "deleted host " + alias}
	}
}

func (t *sshTab) enterExportPathForm(content string) (Tab, tea.Cmd) {
	f := newForm("export to file",
		formField{
			key: "path", label: "target file (your own)", kind: formPath, placeholder: "~/ssh-snippets/web.conf",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("target path cannot be empty")
				}
				return rejectSenvTreePath(v)
			},
		},
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return doWriteExport(strings.TrimSpace(values["path"]), content)
	})
	return t, nil
}

// doWriteExport writes the previewed fragment with 0700/0600 permissions, the
// same discipline the CLI uses for `host export --out`.
func doWriteExport(path, content string) tea.Cmd {
	return func() tea.Msg {
		target := expandHome(path)
		if err := storage.WriteSensitiveFile(target, []byte(content), 0o700, 0o600); err != nil {
			return errMsg{err: err}
		}
		return toastMsg{text: "written to " + target, level: toastSuccess}
	}
}

// --- lookups ---

func (t *sshTab) currentHost() (storage.HostEntry, bool) {
	hosts := t.visibleHosts()
	if t.hostIndex < 0 || t.hostIndex >= len(hosts) {
		return storage.HostEntry{}, false
	}
	return hosts[t.hostIndex], true
}

func (t *sshTab) hostByAlias(alias string) (storage.HostEntry, bool) {
	for _, h := range t.hosts {
		if h.Alias == alias {
			return h, true
		}
	}
	return storage.HostEntry{}, false
}

// --- parsing helpers ---

// parseTagsText splits a comma-separated tag field, dropping empty entries.
func parseTagsText(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseExtraText parses the `$EDITOR` extra-attributes buffer: one KEY=VALUE
// per line, blank lines and `#` comments ignored. Keys must be non-empty and
// whitespace-free, matching ssh.Manager's validation.
func parseExtraText(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("extra must be KEY=VALUE, got %q", line)
		}
		if strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("extra key cannot contain whitespace: %q", key)
		}
		out[key] = value
	}
	return out, nil
}

// renderExtraText renders extra attributes as an editable KEY=VALUE buffer.
func renderExtraText(extra map[string]string) string {
	if len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	for _, key := range sortedKeys(extra) {
		b.WriteString(key + "=" + extra[key] + "\n")
	}
	return b.String()
}

func portValue(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

// expandHome resolves a leading ~ to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// --- detail ---

// openDetail shows the full host record (panes truncate).
func (t *sshTab) openDetail() tea.Cmd {
	if t.focus == paneGroup {
		return nil
	}
	host, ok := t.currentHost()
	if !ok {
		return warnToast("no host selected")
	}
	t.detail = newDetailOverlay("Host "+host.Alias, t.hostDetailLines(host))
	t.detail.SetSize(t.width, t.height)
	return nil
}

// hostDetailLines renders every host field, resolving identityKey to its
// keypair name + fingerprint. The private key is never involved.
func (t *sshTab) hostDetailLines(h storage.HostEntry) []string {
	lines := []string{
		"alias:       " + h.Alias,
		"hostname:    " + orDash(h.Hostname),
		"user:        " + orDash(h.User),
	}
	if h.Port != 0 {
		lines = append(lines, fmt.Sprintf("port:        %d", h.Port))
	}
	lines = append(lines,
		"proxyJump:   "+orDash(h.ProxyJump),
		"identityKey: "+orDash(h.IdentityKey),
		"group:       "+orDash(h.Group),
		"description: "+orDash(h.Description),
	)
	if h.IdentityKey != "" {
		summary := ""
		for _, k := range t.keyPairs {
			if k.Name == h.IdentityKey {
				summary = shortFingerprint(k.Fingerprint)
				break
			}
		}
		if summary == "" {
			lines = append(lines, "  keypair:   ⚠ reference missing")
		} else {
			lines = append(lines, "  keypair:   "+h.IdentityKey+"  "+summary)
		}
	}
	lines = append(lines,
		"tags:        "+orDash(strings.Join(h.Tags, ", ")),
		"extra:",
	)
	for _, k := range sortedKeys(h.Extra) {
		lines = append(lines, "  "+k+"="+h.Extra[k])
	}
	if len(h.Extra) == 0 {
		lines = append(lines, "  -")
	}
	return append(lines, "updated:     "+h.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
}

// --- cursor ---

// focusJump positions the cursor on a host alias (global search target).
func (t *sshTab) focusJump(group, alias string) {
	t.pendingJump = alias
	t.applyPendingJump()
}

// applyPendingJump resolves a pending host alias once the data is available
// (global search target or a just-finished write). 按过滤契约
// （tui-ux-filter）先清过滤保证目标可见，选中目标所在组，再在可见列表上定位。
func (t *sshTab) applyPendingJump() {
	if t.pendingJump == "" {
		return
	}
	t.filterBox.Clear()
	if host, ok := t.hostByAlias(t.pendingJump); ok {
		t.selectGroupName(host.Group)
	}
	for i, h := range t.visibleHosts() {
		if h.Alias == t.pendingJump {
			t.hostIndex = i
			t.focus = paneHost
			t.pendingJump = ""
			break
		}
	}
}

func (t *sshTab) clamp() {
	groups := t.groups()
	if t.groupIndex >= len(groups) {
		t.groupIndex = len(groups) - 1
	}
	if t.groupIndex < 0 {
		t.groupIndex = 0
	}
	t.hostIndex = clamp(t.hostIndex, 0, len(t.visibleHosts())-1)
	if t.hostIndex < 0 {
		t.hostIndex = 0
	}
}

// --- view ---

func (t *sshTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("SSH") + "\n" + truncateWidth("⚠ "+t.loadErr, maxInt(t.width-2, 8))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if t.loaded && len(t.hosts) == 0 && len(t.keyPairs) == 0 && t.mode == sshModeNormal && t.form == nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("SSH"),
			emptyStateStyle.Render("no SSH assets yet; press n to create a host (keypairs live in the KeyPair tab), then Ctrl+R to refresh"))
	}
	overlay := ""
	if t.form != nil {
		overlay = t.form.View()
	} else if t.mode != sshModeNormal {
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

// viewBaseAt renders the two panes: 分组侧栏 → Host 列表。宽度分配与
// env/config 同构：侧栏 width/4（cap 26，min 16），Host 栏吃剩余（预留
// 5 列栏间 chrome：1 间隙 + 两栏各 2 列边框）。
func (t *sshTab) viewBaseAt(height int) string {
	sideW := t.width / 4
	if sideW > 26 {
		sideW = 26
	}
	if sideW < 16 {
		sideW = 16
	}
	hostW := t.width - sideW - 5
	if hostW < 4 {
		hostW = 4
	}

	// 加载态（env 范式）：几何常驻、框内提示，避免装载期布局跳动或误显空态。
	if !t.loaded {
		side := emptyStateStyle.Render("loading SSH assets…")
		host := emptyStateStyle.Render("loading SSH assets…")
		if t.focus == paneGroup {
			side = activePaneStyle.Width(sideW).Height(height).Render(side)
			host = paneStyle.Width(hostW).Height(height).Render(host)
		} else {
			side = paneStyle.Width(sideW).Height(height).Render(side)
			host = activePaneStyle.Width(hostW).Height(height).Render(host)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, side, strings.Repeat(" ", 1), host)
	}

	hostLines := t.hostListLines(maxInt(hostW-4, 8))

	side := t.renderSidebarPane(sideW, height, "")
	hostTitle := fmt.Sprintf("Hosts (%d)", len(t.visibleHosts()))
	if t.filterBox.Active() {
		hostTitle += "  " + t.filterBox.Prompt()
	}
	hostTitle += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(t.visibleHostAliases()))
	host := windowedPane(hostTitle, hostLines, t.hostIndex, height, hostW)

	if t.focus == paneGroup {
		side = activePaneStyle.Width(sideW).Height(height).Render(side)
		host = paneStyle.Width(hostW).Height(height).Render(host)
	} else {
		side = paneStyle.Width(sideW).Height(height).Render(side)
		host = activePaneStyle.Width(hostW).Height(height).Render(host)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, side, strings.Repeat(" ", 1), host)
}

// renderSidebarPane 渲染分组侧栏（复用共享 renderSidebar/SidebarRow，
// 与 env/config 视觉一致）；loadingHint 非空时渲染加载态占位。
func (t *sshTab) renderSidebarPane(width, height int, loadingHint string) string {
	if loadingHint != "" {
		return loadingHint
	}
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
			Selected: i == t.groupIndex && t.focus == paneGroup,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

// hostListLines renders one row per host: `alias → user@host:port` plus the
// associated keypair name and fingerprint summary.
func (t *sshTab) hostListLines(width int) []string {
	byName := make(map[string]ssh.KeyPairSummary, len(t.keyPairs))
	for _, k := range t.keyPairs {
		byName[k.Name] = k
	}
	hosts := t.visibleHosts()
	lines := make([]string, 0, len(hosts))
	for i, host := range hosts {
		line := truncateWidth(hostListLabel(host, byName), width)
		lines = append(lines, cursorLine(line, i == t.hostIndex))
	}
	return lines
}

// hostListLabel 拼装一行 Host：`alias → user@host:port  key:name(fp)`，行尾
// 追加 tags 片段（`#tag` 前缀、最多 2 个、超出 `+n`）；group 不在行内渲染
// （由侧栏表达）。截断由调用方 truncateWidth 统一处理。
func hostListLabel(host storage.HostEntry, keys map[string]ssh.KeyPairSummary) string {
	target := orDash(host.Hostname)
	if host.User != "" {
		target = host.User + "@" + target
	}
	if host.Port != 0 {
		target += ":" + strconv.Itoa(host.Port)
	}
	line := host.Alias + " → " + target
	if host.IdentityKey != "" {
		summary, ok := keys[host.IdentityKey]
		if !ok {
			line += "  key:" + host.IdentityKey + " ⚠missing"
		} else if fp := shortFingerprint(summary.Fingerprint); fp != "" {
			line += "  key:" + host.IdentityKey + "(" + fp + ")"
		} else {
			line += "  key:" + host.IdentityKey
		}
	}
	if len(host.Tags) > 0 {
		shown := host.Tags
		extra := 0
		if len(shown) > 2 {
			extra = len(shown) - 2
			shown = shown[:2]
		}
		parts := make([]string, 0, len(shown)+1)
		for _, tag := range shown {
			parts = append(parts, "#"+tag)
		}
		if extra > 0 {
			parts = append(parts, fmt.Sprintf("+%d", extra))
		}
		line += "  " + strings.Join(parts, " ")
	}
	return line
}

// shortFingerprint condenses a SHA256 fingerprint for inline display.
func shortFingerprint(fp string) string {
	if fp == "" {
		return ""
	}
	return truncateWidth(fp, 16)
}

func (t *sshTab) renderModal() string {
	switch t.mode {
	case sshModeBatchDeleteHost:
		var b strings.Builder
		for _, alias := range t.pendingBatchHosts {
			b.WriteString(alias + "\n")
		}
		return modalBox(t.width, t.height, fmt.Sprintf("delete %d hosts?", len(t.pendingBatchHosts)),
			strings.TrimRight(b.String(), "\n"), "enter/y delete all · esc/n cancel")
	case sshModeDeleteHost:
		body := "cannot be undone after deletion."
		if host, ok := t.hostByAlias(t.pendingHost); ok {
			body = "hostname: " + orDash(host.Hostname) + "\ncannot be undone after deletion."
		}
		return modalBox(t.width, t.height, "delete host "+t.pendingHost+"?", body, "enter/y confirm · esc/n cancel")
	case sshModeExportPreview:
		lines := t.exportPreviewLines()
		page := t.exportPreviewPageSize()
		start := clamp(t.exportScroll, 0, t.exportPreviewMaxScroll())
		end := start + page
		if end > len(lines) {
			end = len(lines)
		}
		body := ""
		if end > start {
			body = strings.Join(lines[start:end], "\n")
		}
		title := "export OpenSSH snippet — " + t.exportLabel
		if len(lines) > page && page > 0 {
			title = fmt.Sprintf("%s  %d–%d/%d", title, start+1, end, len(lines))
		}
		hint := "w write file · esc cancel"
		if len(lines) > page {
			hint = "↑↓/jk/pgup/pgdn scroll · " + hint
		}
		return modalBox(t.width, t.height, title, body, hint)
	case sshModeApplyConfirm:
		st := t.pendingApply
		include := "registered"
		if !st.include {
			include = "not registered (will be added)"
		}
		body := fmt.Sprintf("rebuild group fragments: %d\nmaterialize missing keys: %d\ninclude: %s\nwarnings: %d (details via CLI: senv host export)",
			st.groups, st.pendingKeys, include, st.warnings)
		return modalBox(t.width, t.height, "apply export — "+st.label, body, "enter/y apply · esc/n cancel")
	case sshModeUnexport:
		st := t.pendingUnexport
		if st == nil {
			return ""
		}
		var b strings.Builder
		if st.registered {
			b.WriteString("  - remove senv Include line from ~/.ssh/config\n")
		}
		if st.fragments > 0 {
			fmt.Fprintf(&b, "  - delete %d group fragment(s) under ~/.ssh/senv/groups/\n", st.fragments)
		}
		b.WriteString("  - materialized private keys under ~/.ssh/senv/keys/ are kept")
		return modalBox(t.width, t.height, "unexport senv ssh export?", strings.TrimRight(b.String(), "\n"),
			"enter/y confirm · esc/n cancel")
	}
	return ""
}
