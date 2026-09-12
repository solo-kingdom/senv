package tui

import (
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

// sshTab is the editable browser for host records and keypair metadata. Hosts
// support create/edit/delete/export; keypairs support import/rename/delete/
// materialize. Private-key content is never loaded into the tab model: only the
// file path and the derived fingerprint/public key are ever read.
type sshTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	hosts     []storage.HostEntry
	keyPairs  []ssh.KeyPairSummary
	filterBox Filter // `/` 过滤左栏主列表（alias/hostname 标识）
	sel       List   // 仅承载 host 多选集（space 勾选 / a 全选可见）

	// pendingBatchHosts 批量删除确认页暂存的 host 别名列表。
	pendingBatchHosts []string
	focusLeft         bool
	hostIndex         int
	keyIndex          int
	loadErr           string
	detail            *detailOverlay
	// pendingJump holds a host alias requested by the global search before the
	// tab finished loading, since there is nothing to point at yet.
	pendingJump string
	// pendingKeyJump parks the cursor on a keypair after a rename/import reload.
	pendingKeyJump string

	// form 非 nil 时表示打开了一个结构化表单（host 编辑、keypair 导入/重命名、
	// 导出路径）；formSubmit 是提交后的动作。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode            sshMode
	pendingHost     string   // host staged for delete
	pendingKey      string   // keypair staged for delete/materialize
	pendingForce    bool     // materialize overwrite confirmed
	keyRefs         []string // hosts referencing pendingKey
	materializePath string
	exportLabel     string
	exportContent   string
}

// sshMode is the tab's confirmation/preview state. Every non-normal mode owns
// the keyboard (InputMode) so global shortcuts cannot interrupt a decision.
type sshMode int

const (
	sshModeNormal sshMode = iota
	sshModeDeleteHost
	sshModeDeleteKey
	sshModeMaterialize
	sshModeExportPreview
	sshModeBatchDeleteHost
)

type sshLoadedMsg struct {
	hosts    []storage.HostEntry
	keyPairs []ssh.KeyPairSummary
	err      error
}

// sshReloadMsg reports a successful vault write; the tab reloads and parks the
// cursor on the named entry so the effect of the write is visible.
type sshReloadMsg struct {
	toast     string
	hostAlias string
	keyName   string
}

// sshExportMsg carries a rendered OpenSSH fragment for preview before writing.
type sshExportMsg struct {
	label   string
	content string
	err     error
}

func newSSHTab(mgr Managers) *sshTab {
	return &sshTab{mgr: mgr, focusLeft: true}
}

func (t *sshTab) Title() string { return "SSH" }

func (t *sshTab) Bindings() []KeyAction {
	if t.form != nil {
		return []KeyAction{
			{[]string{"tab/↑↓"}, "switch field", grpForm},
			{[]string{"enter"}, "submit", grpForm},
			{[]string{"esc"}, "cancel", grpForm},
		}
	}
	switch t.mode {
	case sshModeDeleteHost, sshModeDeleteKey, sshModeMaterialize, sshModeBatchDeleteHost:
		return []KeyAction{{[]string{"enter/y"}, "confirm", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	case sshModeExportPreview:
		return []KeyAction{{[]string{"w"}, "write file", grpConfirm}, {[]string{"esc"}, "cancel", grpConfirm}}
	}
	nav := []KeyAction{actUp, actDown, actLeft, actRight, actDetail,
		actTop, actBottom, actPageUp, actPageDn}
	if t.focusLeft {
		return append(append(nav, actNew, actEdit, actDelete, actExport), actRefresh, actFilter)
	}
	return append(append(nav,
		KeyAction{[]string{"i"}, "import keypair", grpItem},
		KeyAction{[]string{"r"}, "rename keypair", grpItem},
		KeyAction{[]string{"m"}, "materialize", grpItem},
		KeyAction{[]string{"d"}, "delete", grpItem},
		KeyAction{[]string{"x"}, "export OpenSSH fragment", grpItem},
	), actRefresh, actFilter)
}

func (t *sshTab) InputMode() bool {
	return t.form != nil || t.mode != sshModeNormal || t.filterBox.Active()
}

// visibleHosts 返回过滤后的左栏可见列表（空词 = 全量）。
func (t *sshTab) visibleHosts() []storage.HostEntry {
	if t.filterBox.Term() == "" {
		return t.hosts
	}
	out := make([]storage.HostEntry, 0, len(t.hosts))
	for _, h := range t.hosts {
		if t.filterBox.Matches(h.Alias + " " + h.Hostname) {
			out = append(out, h)
		}
	}
	return out
}

// enterFilter 进入 `/` 过滤（清词重新开始，与 env/text/config 一致）。
func (t *sshTab) enterFilter() {
	t.focusLeft = true
	t.filterBox.EnterFresh()
}

// handleFilterKeys 处理过滤输入态按键；返回 true 表示按键已被消费。
func (t *sshTab) handleFilterKeys(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc":
		t.filterBox.Clear()
		t.clampFocus()
		return true
	case "enter":
		t.filterBox.Confirm()
		return true
	case "backspace":
		t.filterBox.Backspace()
		t.clampFocus()
		return true
	}
	if isPrintable(msg) {
		t.filterBox.Append(msg.String())
		t.clampFocus()
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

// clampFocus 把两栏游标都收回可见范围。
func (t *sshTab) clampFocus() {
	n := len(t.visibleHosts())
	if t.hostIndex >= n {
		t.hostIndex = maxInt(n-1, 0)
	}
}

func (t *sshTab) SetSize(width, height int) {
	t.width, t.height = width, height
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
	if key, ok := msg.(tea.KeyMsg); ok && t.filterBox.Active() && t.form == nil && t.mode == sshModeNormal {
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
		if msg.keyName != "" {
			t.pendingKeyJump = msg.keyName
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
		t.mode = sshModeExportPreview
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
		if t.focusLeft && t.hostIndex > 0 {
			t.hostIndex--
		} else if !t.focusLeft && t.keyIndex > 0 {
			t.keyIndex--
		}
	case "down", "j":
		if t.focusLeft && t.hostIndex < len(t.visibleHosts())-1 {
			t.hostIndex++
		} else if !t.focusLeft && t.keyIndex < len(t.keyPairs)-1 {
			t.keyIndex++
		}
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "/":
		if t.focusLeft {
			t.enterFilter()
			return t, nil
		}
	case "ctrl+r":
		t.loaded = false
		return t, t.load()
	case "enter":
		return t, t.openDetail()
	case "n":
		if t.focusLeft {
			return t.enterHostForm(nil)
		}
		return t.enterImportKeyPair()
	case "i":
		if !t.focusLeft {
			return t.enterImportKeyPair()
		}
	case "r":
		// r=重命名全局唯一：keypair 重命名从 R 迁到 r（host 重命名走 e 表单）
		if !t.focusLeft {
			return t.enterRenameKeyPair()
		}
	case "m":
		if !t.focusLeft {
			return t.enterMaterialize()
		}
	case " ", "space":
		if t.focusLeft {
			if host, ok := t.currentHost(); ok {
				t.sel.Toggle(host.Alias)
			}
		}
	case "a":
		if t.focusLeft {
			keys := make([]string, 0, len(t.visibleHosts()))
			for _, h := range t.visibleHosts() {
				keys = append(keys, h.Alias)
			}
			t.sel.SelectVisible(keys)
		}
	case "x":
		if t.focusLeft && t.sel.SelectionCount() > 1 {
			return t.enterBatchHostExport()
		}
		return t.enterExport()
	case "d":
		if t.focusLeft && t.sel.SelectionCount() > 1 {
			return t.enterBatchDeleteHosts()
		}
		return t.enterDelete()
	case "e":
		if t.focusLeft && t.sel.SelectionCount() > 1 {
			return t, warnToast("multiple hosts selected: narrow to a single selection to edit")
		}
		if t.focusLeft {
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

// cursorForFocus / jumpFocus / focusListLen 支撑翻页与跳顶底。
func (t *sshTab) cursorForFocus() int {
	if t.focusLeft {
		return t.hostIndex
	}
	return t.keyIndex
}

func (t *sshTab) focusListLen() int {
	if t.focusLeft {
		// 游标语义 = 过滤可见列表上的位置（与 hostListLines/currentHost 一致）。
		return len(t.visibleHosts())
	}
	return len(t.keyPairs)
}

func (t *sshTab) jumpFocus(idx int) {
	n := t.focusListLen()
	if n == 0 || idx < 0 {
		idx = 0
	} else if idx > n-1 {
		idx = n - 1
	}
	if t.focusLeft {
		t.hostIndex = idx
	} else {
		t.keyIndex = idx
	}
}

// updateMode handles the delete/materialize/export confirmation modals.
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
	case sshModeDeleteKey:
		switch {
		case len(t.keyRefs) == 0 && (msg.String() == "enter" || msg.String() == "y"):
			return t.doDeleteKey(t.pendingKey, false)
		case len(t.keyRefs) > 0 && msg.String() == "F":
			return t.doDeleteKey(t.pendingKey, true)
		case msg.String() == "esc" || msg.String() == "n":
			t.cancelMode()
		}
	case sshModeMaterialize:
		switch msg.String() {
		case "enter", "y":
			return t.doMaterialize(t.pendingKey, t.pendingForce)
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
		}
	}
	return t, nil
}

// cancelMode clears every staged confirmation field.
func (t *sshTab) cancelMode() {
	t.mode = sshModeNormal
	t.pendingHost = ""
	t.pendingKey = ""
	t.pendingForce = false
	t.keyRefs = nil
	t.materializePath = ""
	t.exportLabel = ""
	t.exportContent = ""
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
			key: "dir", label: "output directory", kind: formPath, placeholder: "~/.ssh/config.d",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("output directory cannot be empty")
				}
				return nil
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
			content, err := mgr.Export(alias)
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
	if t.focusLeft {
		host, ok := t.currentHost()
		if !ok {
			return t, warnToast("no host to delete")
		}
		t.pendingHost = host.Alias
		t.mode = sshModeDeleteHost
		return t, nil
	}
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair to delete")
	}
	t.pendingKey = key.Name
	t.keyRefs = t.hostRefs(key.Name)
	t.mode = sshModeDeleteKey
	return t, nil
}

// enterMaterialize stages a materialize, pre-computing whether the target file
// already exists so an overwrite needs the extra confirmation.
func (t *sshTab) enterMaterialize() (Tab, tea.Cmd) {
	key, ok := t.currentKey()
	if !ok {
		return t, warnToast("no keypair to materialize")
	}
	path, err := ssh.MaterializePath(key.Name)
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
	t.mode = sshModeMaterialize
	return t, nil
}

// enterExport renders the selected host fragment (or every host when the
// keypair pane is focused) and shows it for preview before any write.
func (t *sshTab) enterExport() (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	alias := ""
	label := "all hosts"
	if t.focusLeft {
		host, ok := t.currentHost()
		if !ok {
			return t, warnToast("no host to export")
		}
		alias = host.Alias
		label = "host " + alias
	}
	return t, func() tea.Msg {
		content, err := mgr.Export(alias)
		return sshExportMsg{label: label, content: content, err: err}
	}
}

// hostRefs returns the aliases of hosts whose identityKey is name.
func (t *sshTab) hostRefs(name string) []string {
	var refs []string
	for _, host := range t.hosts {
		if host.IdentityKey == name {
			refs = append(refs, host.Alias)
		}
	}
	sort.Strings(refs)
	return refs
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
			key: "tags", label: "tags", kind: formText, value: strings.Join(base.Tags, ", "), placeholder: "prod, web",
		},
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
		Tags:        parseTagsText(values["tags"]),
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
			h.Tags = entry.Tags
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

// --- keypair write flows ---

func (t *sshTab) enterImportKeyPair() (Tab, tea.Cmd) {
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
	)
	t.openForm(f, func(values map[string]string) tea.Cmd {
		return t.doImportKeyPair(strings.TrimSpace(values["name"]), strings.TrimSpace(values["path"]))
	})
	return t, nil
}

func (t *sshTab) doImportKeyPair(name, path string) tea.Cmd {
	if path == "" {
		return warnToast("private key file path cannot be empty")
	}
	mgr := t.mgr.SSH
	mgrs := t.mgr
	return func() tea.Msg {
		if _, err := mgr.ImportKeyPair(name, expandHome(path), false); err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "import failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "import")
		return sshReloadMsg{toast: "imported keypair " + name, keyName: name}
	}
}

func (t *sshTab) enterRenameKeyPair() (Tab, tea.Cmd) {
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

func (t *sshTab) doRenameKeyPair(oldName, newName string) tea.Cmd {
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
		return sshReloadMsg{toast: toast, keyName: newName}
	}
}

func (t *sshTab) doDeleteKey(name string, force bool) (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	refs := append([]string(nil), t.keyRefs...)
	t.cancelMode()
	return t, func() tea.Msg {
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
		return sshReloadMsg{toast: toast}
	}
}

func (t *sshTab) doMaterialize(name string, force bool) (Tab, tea.Cmd) {
	mgr := t.mgr.SSH
	mgrs := t.mgr
	t.cancelMode()
	return t, func() tea.Msg {
		path, err := mgr.Materialize(name, force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, false, "materialize failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpSSHKey, "keypair:"+name, true, "materialize")
		// Only the落盘路径 is surfaced; the private key body never reaches the UI.
		return toastMsg{text: "written to " + path, level: toastSuccess}
	}
}

func (t *sshTab) enterExportPathForm(content string) (Tab, tea.Cmd) {
	f := newForm("export to file",
		formField{
			key: "path", label: "target file", kind: formPath, placeholder: "~/.ssh/config.d/senv",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("target path cannot be empty")
				}
				return nil
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
	if t.hostIndex < 0 || t.hostIndex >= len(t.hosts) {
		return storage.HostEntry{}, false
	}
	return t.hosts[t.hostIndex], true
}

func (t *sshTab) currentKey() (ssh.KeyPairSummary, bool) {
	if t.keyIndex < 0 || t.keyIndex >= len(t.keyPairs) {
		return ssh.KeyPairSummary{}, false
	}
	return t.keyPairs[t.keyIndex], true
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

// openDetail shows the full record for the focused entry (panes truncate).
func (t *sshTab) openDetail() tea.Cmd {
	if t.focusLeft {
		host, ok := t.currentHost()
		if !ok {
			return warnToast("no host selected")
		}
		t.detail = newDetailOverlay("Host "+host.Alias, t.hostDetailLines(host))
	} else {
		key, ok := t.currentKey()
		if !ok {
			return warnToast("no keypair selected")
		}
		t.detail = newDetailOverlay("KeyPair "+key.Name, keyPairDetailLines(key))
	}
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

// focusJump positions the cursor on a host alias (global search target).
func (t *sshTab) focusJump(group, alias string) {
	t.pendingJump = alias
	t.applyPendingJump()
}

// applyPendingJump resolves a pending host or keypair name once the data is
// available (global search target or a just-finished write). 按过滤契约
// （tui-ux-filter）先清过滤保证目标可见，再在可见列表上定位。
func (t *sshTab) applyPendingJump() {
	if t.pendingJump != "" {
		t.filterBox.Clear()
		for i, h := range t.visibleHosts() {
			if h.Alias == t.pendingJump {
				t.hostIndex = i
				t.focusLeft = true
				t.pendingJump = ""
				break
			}
		}
	}
	if t.pendingKeyJump != "" {
		for i, k := range t.keyPairs {
			if k.Name == t.pendingKeyJump {
				t.keyIndex = i
				t.focusLeft = false
				t.pendingKeyJump = ""
				break
			}
		}
	}
}

func (t *sshTab) clamp() {
	if t.hostIndex >= len(t.hosts) {
		t.hostIndex = len(t.hosts) - 1
	}
	if t.hostIndex < 0 {
		t.hostIndex = 0
	}
	if t.keyIndex >= len(t.keyPairs) {
		t.keyIndex = len(t.keyPairs) - 1
	}
	if t.keyIndex < 0 {
		t.keyIndex = 0
	}
}

// --- view ---

func (t *sshTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("SSH") + "\n" + truncateRunes("⚠ "+t.loadErr, maxInt(t.width, 1))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if t.loaded && len(t.hosts) == 0 && len(t.keyPairs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("SSH"),
			emptyStateStyle.Render("no SSH assets yet; press n to create a host or import a keypair, then Ctrl+R to refresh"))
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

func (t *sshTab) viewBaseAt(height int) string {
	leftW := t.width * 11 / 20
	if leftW < 20 {
		leftW = 20
	}
	if leftW > t.width-24 {
		leftW = t.width - 24
	}
	if leftW < 8 {
		leftW = 8
	}
	rightW := t.width - leftW - 5
	if rightW < 4 {
		rightW = 4
	}

	// 加载态（env 范式）：几何常驻、框内提示，避免装载期布局跳动或误显空态。
	if !t.loaded {
		left := emptyStateStyle.Render("loading SSH assets…")
		right := emptyStateStyle.Render("loading SSH assets…")
		if t.focusLeft {
			left = activePaneStyle.Width(leftW).Height(height).Render(left)
			right = paneStyle.Width(rightW).Height(height).Render(right)
		} else {
			left = paneStyle.Width(leftW).Height(height).Render(left)
			right = activePaneStyle.Width(rightW).Height(height).Render(right)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
	}

	hostLines := t.hostListLines(maxInt(leftW-4, 8))
	keyLines := t.keyPairListLines(maxInt(rightW-4, 8))

	leftTitle := fmt.Sprintf("Hosts (%d)", len(t.visibleHosts()))
	if t.filterBox.Active() {
		leftTitle += "  " + t.filterBox.Prompt()
	}
	leftTitle += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(t.visibleHostAliases()))
	left := windowedPane(leftTitle, hostLines, t.hostIndex, height, leftW)
	right := windowedPane(fmt.Sprintf("KeyPairs (%d) · private keys masked", len(t.keyPairs)), keyLines, t.keyIndex, height, rightW)

	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(height).Render(left)
		right = paneStyle.Width(rightW).Height(height).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(height).Render(left)
		right = activePaneStyle.Width(rightW).Height(height).Render(right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
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

func hostListLabel(host storage.HostEntry, keys map[string]ssh.KeyPairSummary) string {
	target := orDash(host.Hostname)
	if host.User != "" {
		target = host.User + "@" + target
	}
	if host.Port != 0 {
		target += ":" + strconv.Itoa(host.Port)
	}
	line := host.Alias + " → " + target
	if host.IdentityKey == "" {
		return line
	}
	summary, ok := keys[host.IdentityKey]
	if !ok {
		return line + "  key:" + host.IdentityKey + " ⚠missing"
	}
	if fp := shortFingerprint(summary.Fingerprint); fp != "" {
		return line + "  key:" + host.IdentityKey + "(" + fp + ")"
	}
	return line + "  key:" + host.IdentityKey
}

func (t *sshTab) keyPairListLines(width int) []string {
	lines := make([]string, 0, len(t.keyPairs))
	for i, key := range t.keyPairs {
		label := key.Fingerprint
		if label == "" {
			label = "pubkey: none"
		}
		line := truncateWidth(key.Name+" · "+label, width)
		lines = append(lines, cursorLine(line, i == t.keyIndex))
	}
	return lines
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
		return modalBox(fmt.Sprintf("delete %d hosts?", len(t.pendingBatchHosts)),
			strings.TrimRight(b.String(), "\n"), "enter/y delete all · esc/n cancel")
	case sshModeDeleteHost:
		body := "cannot be undone after deletion."
		if host, ok := t.hostByAlias(t.pendingHost); ok {
			body = "hostname: " + orDash(host.Hostname) + "\ncannot be undone after deletion."
		}
		return modalBox("delete host "+t.pendingHost+"?", body, "enter/y confirm · esc/n cancel")
	case sshModeDeleteKey:
		if len(t.keyRefs) == 0 {
			return modalBox("delete keypair "+t.pendingKey+"?", "the private key cannot be recovered after deletion.", "enter/y confirm · esc/n cancel")
		}
		var b strings.Builder
		b.WriteString("these hosts still reference the keypair:\n")
		for _, alias := range t.keyRefs {
			b.WriteString("  · " + alias + "\n")
		}
		b.WriteString("\ndeletion is refused by default. press F to force delete and clear identityKey on these hosts.")
		return modalBox("keypair "+t.pendingKey+" still referenced", b.String(), "F force delete · esc cancel")
	case sshModeMaterialize:
		body := "will write the private key in plaintext to:\n" + t.materializePath + " (0600)."
		if t.pendingForce {
			body += "\n⚠ target file exists and will be overwritten."
		}
		return modalBox("materialize keypair "+t.pendingKey, body, "enter/y confirm · esc/n cancel")
	case sshModeExportPreview:
		return modalBox("export OpenSSH snippet — "+t.exportLabel, t.exportContent, "w write file · esc cancel")
	}
	return ""
}
