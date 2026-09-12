package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// mcpTab 浏览 MCP Server 档案与各 Coding Agent 的导出状态，并在 Tab 内完成
// 档案增删改与导出/撤回。列表/详情/计划不渲染 env 字面量；$EDITOR 是唯一解密面。
type mcpTab struct {
	mgr           Managers
	width, height int
	loaded        bool
	loadErr       string
	warning       string

	servers []mcp.Server
	agents  []agentcfg.Target
	status  []mcpAgentStatus

	serverIndex int
	agentIndex  int
	focusLeft   bool
	filterBox   Filter // `/` 过滤左栏主列表（alias/command 标识）
	sel         List   // 仅承载档案多选集（alias 为稳定标识）
	pendingJump string
	detail      *detailOverlay

	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode           mcpMode
	pendingAlias   string
	pendingAgents  []string
	planKind       string // "export" | "unexport"
	exportPlan     *mcp.ExportPlan
	unexportPlan   *mcp.UnexportPlan
	planForce      bool
	changedIdx     int
	changedAllowed map[string]bool
}

type mcpMode int

const (
	mcpModeNormal mcpMode = iota
	mcpModeDelete
	mcpModePlan
	mcpModeChangedConfirm
)

type mcpAgentStatus struct {
	ID     string
	Name   string
	Path   string
	State  string // 未导出 / 已导出 / 漂移 / 错误
	Reason string
}

type mcpLoadedMsg struct {
	servers []mcp.Server
	status  []mcpAgentStatus
	warning string
	err     error
}

type mcpReloadMsg struct {
	toast string
	alias string
}

type mcpFormReopenMsg struct {
	form   *form
	submit func(values map[string]string) tea.Cmd
	values map[string]string
	field  string
	err    error
}

func newMCPTab(mgr Managers) *mcpTab {
	return &mcpTab{mgr: mgr, focusLeft: true, agents: agentcfg.Supported()}
}

func (t *mcpTab) Title() string { return "MCP" }

func (t *mcpTab) Bindings() []KeyAction {
	if t.form != nil {
		return []KeyAction{
			{[]string{"tab/↑↓"}, "switch field", grpForm},
			{[]string{"e"}, "edit multiline", grpForm},
			{[]string{"enter"}, "submit", grpForm},
			{[]string{"esc"}, "cancel", grpForm},
		}
	}
	switch t.mode {
	case mcpModeDelete:
		return []KeyAction{{[]string{"enter/y"}, "confirm", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	case mcpModePlan:
		return []KeyAction{
			{[]string{"enter/y"}, "confirm", grpConfirm},
			{[]string{"F"}, "force overwrite drift", grpConfirm},
			{[]string{"esc/n"}, "cancel", grpConfirm},
		}
	case mcpModeChangedConfirm:
		return []KeyAction{
			{[]string{"y"}, "delete this one", grpConfirm},
			{[]string{"n"}, "skip", grpConfirm},
			{[]string{"esc"}, "cancel entire revert", grpConfirm},
		}
	}
	return append([]KeyAction{actUp, actDown, actLeft, actRight, actDetail,
		actTop, actBottom, actPageUp, actPageDn},
		KeyAction{[]string{"n"}, "new profile", grpItem},
		KeyAction{[]string{"e"}, "edit profile", grpItem},
		KeyAction{[]string{"d"}, "delete profile", grpItem},
		KeyAction{[]string{"x/X"}, "export (current/all agents)", grpItem},
		KeyAction{[]string{"u/U"}, "revert (current/all agents)", grpItem},
		actFilter, actRefresh,
	)
}

func (t *mcpTab) InputMode() bool {
	return t.form != nil || t.mode != mcpModeNormal || t.filterBox.Active()
}

// visibleServers 返回过滤后的左栏可见列表（空词 = 全量）。
func (t *mcpTab) visibleServers() []mcp.Server {
	if t.filterBox.Term() == "" {
		return t.servers
	}
	out := make([]mcp.Server, 0, len(t.servers))
	for _, s := range t.servers {
		if t.filterBox.Matches(s.Alias + " " + s.Command) {
			out = append(out, s)
		}
	}
	return out
}

// visibleServerAliases 返回可见档案的别名（计数提示用）。
func (t *mcpTab) visibleServerAliases() []string {
	servers := t.visibleServers()
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.Alias)
	}
	return out
}

// clampLeft 把左栏游标收回可见范围。
func (t *mcpTab) clampLeft() {
	n := len(t.visibleServers())
	if t.serverIndex >= n {
		t.serverIndex = maxInt(n-1, 0)
	}
}

func (t *mcpTab) SetSize(width, height int) {
	t.width, t.height = width, height
	if t.form != nil {
		t.form.SetSize(width, height)
	}
	if t.detail != nil {
		t.detail.SetSize(width, height)
	}
}

func (t *mcpTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *mcpTab) Reload() tea.Cmd { return t.load() }

func (t *mcpTab) load() tea.Cmd {
	return func() tea.Msg {
		st := perflog.Start("tui.load-mcp")
		if t.mgr.MCP == nil {
			st.End(true)
			return mcpLoadedMsg{}
		}
		servers, err := t.mgr.MCP.List()
		if err != nil {
			st.End(false)
			return mcpLoadedMsg{err: err}
		}
		status, warning := t.statusFor(servers)
		st.With("servers", len(servers)).End(true)
		return mcpLoadedMsg{servers: servers, status: status, warning: warning}
	}
}

func (t *mcpTab) statusFor(servers []mcp.Server) ([]mcpAgentStatus, string) {
	out := make([]mcpAgentStatus, 0, len(t.agents))
	for _, agent := range t.agents {
		out = append(out, mcpAgentStatus{ID: agent.ID, Name: agent.Name, State: "not exported"})
	}
	alias := ""
	idx := t.serverIndex
	if idx >= 0 && idx < len(servers) {
		alias = servers[idx].Alias
	} else if len(servers) > 0 {
		alias = servers[0].Alias
	}
	if alias == "" {
		return out, ""
	}
	exporter, err := t.exporter(false)
	if err != nil {
		return out, err.Error()
	}
	plan, err := exporter.Plan(t.agents, []string{alias})
	if err != nil {
		return out, err.Error()
	}
	byAgent := map[string]mcp.ExportItem{}
	for _, item := range plan.Items {
		byAgent[item.Agent] = item
	}
	for i := range out {
		item, ok := byAgent[out[i].ID]
		if !ok {
			continue
		}
		out[i].Path = item.Path
		out[i].Reason = item.Reason
		switch item.Action {
		case mcp.ActionSkip, mcp.ActionUpdate:
			out[i].State = "exported"
		case mcp.ActionDrift:
			out[i].State = "drift"
		case mcp.ActionError:
			out[i].State = "error"
		default:
			out[i].State = "not exported"
		}
	}
	warn := strings.Join(exporter.Ledger().Warnings(), "; ")
	return out, warn
}

func (t *mcpTab) exporter(force bool) (*mcp.Exporter, error) {
	if t.mgr.MCP == nil {
		return nil, fmt.Errorf("MCP manager unavailable")
	}
	opts := mcp.ExporterOptions{
		Home:       t.mgr.MCPHome,
		Scope:      "user",
		Force:      force,
		LedgerPath: t.mgr.MCPLedger,
	}
	if t.mgr.Env != nil && t.mgr.Text != nil {
		getter := tuiGetter{envMgr: t.mgr.Env, textMgr: t.mgr.Text}
		opts.Resolve = func(value string) (string, error) {
			return ref.Resolve(value, getter, ref.ResolveOptions{})
		}
	}
	return t.mgr.MCP.NewExporter(opts)
}

func (t *mcpTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && t.filterBox.Active() && t.form == nil && t.mode == mcpModeNormal {
		switch key.String() {
		case "esc":
			t.filterBox.Clear()
			t.clampLeft()
			return t, nil
		case "enter":
			t.filterBox.Confirm()
			return t, nil
		case "backspace":
			t.filterBox.Backspace()
			t.clampLeft()
			return t, nil
		}
		if isPrintable(key) {
			t.filterBox.Append(key.String())
			t.clampLeft()
			return t, nil
		}
	}
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
	case mcpFormReopenMsg:
		msg.form.SetSize(t.width, t.height)
		for key, value := range msg.values {
			msg.form.SetValue(key, value)
		}
		if index := msg.form.fieldIndex(msg.field); index >= 0 {
			msg.form.errs[index] = msg.err.Error()
		}
		t.form = msg.form
		t.formSubmit = msg.submit
		return t, nil
	}
	if t.form != nil {
		next, cmd := t.form.Update(msg)
		t.form = next
		return t, cmd
	}

	switch msg := msg.(type) {
	case mcpLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.servers = msg.servers
		t.status = msg.status
		t.warning = msg.warning
		t.clamp()
		t.reconcileSelection()
		t.applyPendingJump()
		return t, nil
	case mcpReloadMsg:
		t.cancelMode()
		if msg.alias != "" {
			t.pendingJump = msg.alias
		}
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd
	case detailCloseMsg:
		t.detail = nil
		return t, nil
	case tea.KeyMsg:
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		if t.mode != mcpModeNormal {
			return t.updateMode(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

func (t *mcpTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if t.focusLeft && t.serverIndex > 0 {
			t.serverIndex--
			t.syncStatus()
		} else if !t.focusLeft && t.agentIndex > 0 {
			t.agentIndex--
		}
	case "down", "j":
		if t.focusLeft && t.serverIndex < len(t.visibleServers())-1 {
			t.serverIndex++
			t.syncStatus()
		} else if !t.focusLeft && t.agentIndex < len(t.agents)-1 {
			t.agentIndex++
		}
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "/":
		// 与 ssh 同范式：过滤只作用于左栏档案列表，右栏不进入过滤态。
		if t.focusLeft {
			t.filterBox.EnterFresh()
			return t, nil
		}
	case "enter":
		return t, t.openDetail()
	case "n":
		return t.enterForm(nil)
	case "e":
		if t.sel.SelectionCount() > 1 {
			return t, warnToast("multiple profiles selected: narrow to a single selection to edit")
		}
		if t.selectionDivergesFromCursor() {
			return t, warnToast("selected profile differs from cursor: clear the selection or align the cursor to edit")
		}
		srv := t.currentServer()
		if srv == nil {
			return t, warnToast("no profile selected")
		}
		entry, err := t.mgr.MCP.Get(srv.Alias)
		if err != nil {
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		return t.enterForm(entry)
	case " ", "space":
		if t.focusLeft {
			if srv := t.currentServer(); srv != nil {
				t.sel.Toggle(srv.Alias)
			}
		}
	case "a":
		if t.focusLeft {
			keys := make([]string, 0, len(t.visibleServers()))
			for _, s := range t.visibleServers() {
				keys = append(keys, s.Alias)
			}
			t.sel.SelectVisible(keys)
		}
	case "d":
		if t.sel.SelectionCount() > 1 {
			return t, warnToast("multiple profiles selected: narrow to a single selection to delete")
		}
		if t.selectionDivergesFromCursor() {
			return t, warnToast("selected profile differs from cursor: clear the selection or align the cursor to delete")
		}
		return t.enterDelete()
	case "x":
		return t.startExport(false)
	case "X":
		return t.startExport(true)
	case "u":
		return t.startUnexport(false)
	case "U":
		return t.startUnexport(true)
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
func (t *mcpTab) cursorForFocus() int {
	if t.focusLeft {
		return t.serverIndex
	}
	return t.agentIndex
}

func (t *mcpTab) focusListLen() int {
	if t.focusLeft {
		return len(t.visibleServers())
	}
	return len(t.agents)
}

func (t *mcpTab) jumpFocus(idx int) {
	n := t.focusListLen()
	if n == 0 || idx < 0 {
		idx = 0
	} else if idx > n-1 {
		idx = n - 1
	}
	if t.focusLeft {
		t.serverIndex = idx
	} else {
		t.agentIndex = idx
	}
}

func (t *mcpTab) syncStatus() {
	// 游标是过滤可见列表上的位置；状态右栏必须按同一列表定位选中档案，
	// 否则过滤态下会把状态算到不可见档案上。
	t.status, t.warning = t.statusFor(t.visibleServers())
}

func (t *mcpTab) updateMode(msg tea.KeyMsg) (Tab, tea.Cmd) {
	key := msg.String()
	switch t.mode {
	case mcpModeDelete:
		if key == "y" || key == "enter" {
			alias := t.pendingAlias
			t.cancelMode()
			return t, t.doDelete(alias)
		}
		if key == "esc" || key == "n" {
			t.cancelMode()
			return t, warnToast("cancelled")
		}
		// 其余按键忽略（grill D7：确认页收紧）
	case mcpModeChangedConfirm:
		// 帮助文案承诺 esc 取消整个撤回：这里必须整体退出，已答 y 的
		// 条目同样不生效，不得把 esc 当作「跳过本条」继续执行。
		if key == "esc" {
			t.cancelMode()
			return t, warnToast("unexport cancelled")
		}
		items := t.changedItems()
		item := items[t.changedIdx]
		t.changedAllowed[item.Agent+"/"+item.Alias] = key == "y"
		t.changedIdx++
		if t.changedIdx < len(items) {
			return t, nil
		}
		plan := t.unexportPlan
		force := t.planForce
		allowed := t.changedAllowed
		alias := t.pendingAlias
		t.cancelMode()
		return t, t.executeUnexport(plan, force, alias, allowed)
	case mcpModePlan:
		switch key {
		case "F":
			return t.replanForce()
		case "y", "enter":
			if t.planKind == "unexport" && len(t.changedItems()) > 0 {
				t.changedAllowed = map[string]bool{}
				t.changedIdx = 0
				t.mode = mcpModeChangedConfirm
				return t, nil
			}
			return t.confirmPlan()
		case "esc", "n":
			t.cancelMode()
			return t, warnToast("cancelled")
		}
		// 其余按键忽略：计划确认页不把未知键解释为取消或放行（grill D7）
	}
	return t, nil
}

func (t *mcpTab) cancelMode() {
	t.mode = mcpModeNormal
	t.pendingAlias = ""
	t.pendingAgents = nil
	t.planKind = ""
	t.exportPlan = nil
	t.unexportPlan = nil
	t.planForce = false
	t.changedIdx = 0
	t.changedAllowed = nil
}

func (t *mcpTab) currentServer() *mcp.Server {
	servers := t.visibleServers()
	if t.serverIndex < 0 || t.serverIndex >= len(servers) {
		return nil
	}
	return &servers[t.serverIndex]
}

func (t *mcpTab) currentAgent() agentcfg.Target {
	if t.agentIndex < 0 || t.agentIndex >= len(t.agents) {
		return agentcfg.Target{}
	}
	return t.agents[t.agentIndex]
}

func (t *mcpTab) clamp() {
	// 游标语义 = 过滤可见列表上的位置（与 currentServer/serverLines 一致）。
	n := len(t.visibleServers())
	if t.serverIndex >= n {
		t.serverIndex = n - 1
	}
	if t.serverIndex < 0 {
		t.serverIndex = 0
	}
	if t.agentIndex >= len(t.agents) {
		t.agentIndex = len(t.agents) - 1
	}
	if t.agentIndex < 0 {
		t.agentIndex = 0
	}
}

// reconcileSelection 丢弃多选集中已不存在的档案别名（删除/改名后 reload 时
// 防止幽灵勾选）。
func (t *mcpTab) reconcileSelection() {
	live := make(map[string]bool, len(t.servers))
	for _, s := range t.servers {
		live[s.Alias] = true
	}
	for _, alias := range t.sel.Selected() {
		if !live[alias] {
			t.sel.Toggle(alias)
		}
	}
}

func (t *mcpTab) focusJump(_, alias string) {
	t.pendingJump = alias
	t.applyPendingJump()
}

func (t *mcpTab) applyPendingJump() {
	if t.pendingJump == "" {
		return
	}
	t.filterBox.Clear() // 跳转前清过滤，保证目标可见
	for i, srv := range t.servers {
		if srv.Alias == t.pendingJump {
			t.serverIndex = i
			t.focusLeft = true
			t.syncStatus()
			t.pendingJump = ""
			return
		}
	}
}

func (t *mcpTab) openDetail() tea.Cmd {
	srv := t.currentServer()
	if srv == nil {
		return warnToast("no profile selected")
	}
	entry, err := t.mgr.MCP.Get(srv.Alias)
	if err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	t.detail = newDetailOverlay("MCP "+entry.Alias, mcpDetailLines(entry))
	t.detail.SetSize(t.width, t.height)
	return nil
}

func mcpDetailLines(entry *storage.MCPServerEntry) []string {
	if entry.URL != "" {
		// Remote profiles: only the url origin and header key names are
		// rendered — query strings and header values carry credentials.
		lines := []string{
			"alias:       " + entry.Alias,
			"transport:   " + entry.Transport,
			"url:         " + mcp.URLOrigin(entry.URL),
			"description: " + orDash(entry.Description),
			"headers:",
		}
		keys := make([]string, 0, len(entry.Headers))
		for key := range entry.Headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			lines = append(lines, "  (none)")
		} else {
			for _, key := range keys {
				lines = append(lines, "  "+key)
			}
		}
		return lines
	}
	lines := []string{
		"alias:       " + entry.Alias,
		"transport:   " + entry.Transport,
		"command:     " + entry.Command,
		"description: " + orDash(entry.Description),
		"args:",
	}
	if len(entry.Args) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, arg := range entry.Args {
			lines = append(lines, "  "+arg)
		}
	}
	lines = append(lines, "env:")
	keys := make([]string, 0, len(entry.Env))
	for key := range entry.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, key := range keys {
			lines = append(lines, "  "+key+"="+maskEnvValue(entry.Env[key]))
		}
	}
	return lines
}

func maskEnvValue(value string) string {
	if isRefTemplate(value) {
		return value
	}
	if value == "" {
		return ""
	}
	return "***"
}

func isRefTemplate(value string) bool {
	return strings.Contains(value, "{{env:") || strings.Contains(value, "{{text:")
}

// isRemoteTransport reports whether a form transport value addresses a remote
// server (http/sse) rather than a local stdio command.
func isRemoteTransport(transport string) bool {
	return transport == storage.MCPTransportHTTP || transport == storage.MCPTransportSSE
}

func (t *mcpTab) enterForm(existing *storage.MCPServerEntry) (Tab, tea.Cmd) {
	create := existing == nil
	title := "new MCP server profile"
	alias := ""
	transport := storage.MCPTransportStdio
	command := ""
	args := ""
	envText := ""
	url := ""
	headersText := ""
	desc := ""
	if !create {
		title = "edit " + existing.Alias
		alias = existing.Alias
		transport = existing.Transport
		command = existing.Command
		args = strings.Join(existing.Args, "\n")
		envText = formatEnvLines(existing.Env)
		url = existing.URL
		headersText = formatEnvLines(existing.Headers)
		desc = existing.Description
	}
	aliasField := formField{key: "alias", label: "alias", kind: formText, value: alias, validate: requiredAlias}
	if !create {
		aliasField.kind = formEnum
		aliasField.options = []string{alias}
	}
	// stdio 字段只在 stdio 传输下出现；remote 字段只在 http/sse 下出现。
	stdioOnly := func(values map[string]string) bool { return !isRemoteTransport(values["transport"]) }
	remoteOnly := func(values map[string]string) bool { return isRemoteTransport(values["transport"]) }
	fields := []formField{
		aliasField,
		{key: "transport", label: "transport", kind: formEnum, value: transport, options: []string{
			storage.MCPTransportStdio, storage.MCPTransportHTTP, storage.MCPTransportSSE,
		}},
		{key: "command", label: "command", kind: formText, value: command, validate: requiredCommand, visible: stdioOnly},
		{key: "args", label: "args", kind: formEditor, value: args, visible: stdioOnly},
		{key: "env", label: "env", kind: formEditor, value: envText, preview: mcpEnvPreview, validate: validateEnvLines, visible: stdioOnly},
		{key: "url", label: "url", kind: formText, value: url, validate: requiredURL, visible: remoteOnly},
		{key: "headers", label: "headers", kind: formEditor, value: headersText, preview: mcpHeadersPreview, validate: validateHeaderLines, visible: remoteOnly},
		{key: "description", label: "description", kind: formText, value: desc},
	}
	f := newForm(title, fields...)
	f.editExternal = func(index int, current string) tea.Cmd {
		return externalEditorCmd(index, "senv-mcp-*", current)
	}
	t.form = f
	t.form.SetSize(t.width, t.height)
	t.formSubmit = t.submitForm(create, f)
	return t, nil
}

func (t *mcpTab) submitForm(create bool, f *form) func(map[string]string) tea.Cmd {
	return func(values map[string]string) tea.Cmd {
		return t.doSubmit(create, f, values)
	}
}

func (t *mcpTab) doSubmit(create bool, f *form, values map[string]string) tea.Cmd {
	mgrs := t.mgr
	submit := t.submitForm(create, f)
	reopen := func(field string, err error) tea.Msg {
		return mcpFormReopenMsg{form: f, submit: submit, values: values, field: field, err: err}
	}
	return func() tea.Msg {
		alias := strings.TrimSpace(values["alias"])
		transport := values["transport"]
		description := strings.TrimSpace(values["description"])
		entry := &storage.MCPServerEntry{Alias: alias, Transport: transport, Description: description}
		if isRemoteTransport(transport) {
			// remote：只落 url/headers；值按模板原样存储。
			entry.URL = strings.TrimSpace(values["url"])
			headers, err := parseHeaderLines(values["headers"])
			if err != nil {
				return reopen("headers", err)
			}
			entry.Headers = headers
		} else {
			entry.Command = strings.TrimSpace(values["command"])
			entry.Args = parseArgLines(values["args"])
			envMap, err := parseEnvLines(values["env"])
			if err != nil {
				return reopen("env", err)
			}
			entry.Env = envMap
		}
		if create {
			if err := mgrs.MCP.Add(entry); err != nil {
				field := "alias"
				if !errors.Is(err, mcp.ErrExists) {
					field = "command"
					if isRemoteTransport(transport) {
						field = "url"
					}
				}
				recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, false, "add failed")
				return reopen(field, err)
			}
			recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, true, "add")
			return mcpReloadMsg{toast: "saved " + alias, alias: alias}
		}
		err := mgrs.MCP.Update(alias, func(existing *storage.MCPServerEntry) error {
			// 传输切换整体替换字段集：切到 remote 清空 stdio 字段，反之亦然，
			// 这样目标传输的字段校验不会读到另一侧的残留值。CreatedAt 是
			// 档案身份的一部分，整体替换时必须保留。
			createdAt := existing.CreatedAt
			*existing = *entry
			existing.CreatedAt = createdAt
			return nil
		})
		if err != nil {
			recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, false, "edit failed")
			field := "command"
			if isRemoteTransport(transport) {
				field = "url"
			}
			return reopen(field, err)
		}
		recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, true, "edit")
		return mcpReloadMsg{toast: "updated " + alias, alias: alias}
	}
}

func (t *mcpTab) enterDelete() (Tab, tea.Cmd) {
	srv := t.currentServer()
	if srv == nil {
		return t, warnToast("no profile to delete")
	}
	t.mode = mcpModeDelete
	t.pendingAlias = srv.Alias
	t.pendingAgents = t.exportedAgents(srv.Alias)
	return t, nil
}

func (t *mcpTab) exportedAgents(alias string) []string {
	exporter, err := t.exporter(false)
	if err != nil {
		return nil
	}
	var out []string
	for _, agent := range t.agents {
		if _, ok := exporter.Ledger().Get(agent.ID, alias); ok {
			out = append(out, agent.ID)
		}
	}
	return out
}

func (t *mcpTab) doDelete(alias string) tea.Cmd {
	mgr := t.mgr.MCP
	mgrs := t.mgr
	return func() tea.Msg {
		if err := mgr.Delete(alias); err != nil {
			recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, false, "delete failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpMCPServer, "mcp:"+alias, true, "delete")
		return mcpReloadMsg{toast: "deleted " + alias}
	}
}

func (t *mcpTab) startExport(allAgents bool) (Tab, tea.Cmd) {
	aliases := t.planAliases()
	if len(aliases) == 0 {
		return t, warnToast("no profile to export")
	}
	targets := t.exportTargets(allAgents)
	if len(targets) == 0 {
		return t, warnToast("no target agent to export")
	}
	exporter, err := t.exporter(false)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	plan, err := exporter.Plan(targets, aliases)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.mode = mcpModePlan
	t.planKind = "export"
	t.exportPlan = plan
	t.planForce = false
	t.pendingAlias = aliases[0]
	if len(aliases) > 1 {
		t.pendingAlias = "" // 多档案：完成后整表刷新
	}
	return t, nil
}

func (t *mcpTab) startUnexport(allAgents bool) (Tab, tea.Cmd) {
	aliases := t.planAliases()
	if len(aliases) == 0 {
		return t, warnToast("no profile to unexport")
	}
	targets := t.exportTargets(allAgents)
	if len(targets) == 0 {
		return t, warnToast("no target agent to unexport")
	}
	exporter, err := t.exporter(false)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	plan, err := exporter.PlanUnexport(targets, aliases)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.mode = mcpModePlan
	t.planKind = "unexport"
	t.unexportPlan = plan
	t.planForce = false
	t.pendingAlias = aliases[0]
	if len(aliases) > 1 {
		t.pendingAlias = ""
	}
	return t, nil
}

// planAliases 多选集非空时返回选择集别名，否则返回当前选中档案。
func (t *mcpTab) planAliases() []string {
	if t.sel.SelectionCount() > 0 {
		var out []string
		for _, s := range t.visibleServers() {
			if t.sel.IsSelected(s.Alias) {
				out = append(out, s.Alias)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if srv := t.currentServer(); srv != nil {
		return []string{srv.Alias}
	}
	return nil
}

func (t *mcpTab) exportTargets(all bool) []agentcfg.Target {
	if all {
		return t.agents
	}
	cur := t.currentAgent()
	if cur.ID == "" {
		return nil
	}
	return []agentcfg.Target{cur}
}

// selectionDivergesFromCursor 报告「恰好选中 1 条且不是游标所在档案」——
// 此时单实体/删除动作不应静默作用于游标项。
func (t *mcpTab) selectionDivergesFromCursor() bool {
	if t.sel.SelectionCount() != 1 {
		return false
	}
	srv := t.currentServer()
	if srv == nil {
		return false
	}
	return !t.sel.IsSelected(srv.Alias)
}

func (t *mcpTab) replanForce() (Tab, tea.Cmd) {
	if t.planKind != "export" || t.exportPlan == nil {
		return t, warnToast("current plan cannot force overwrite")
	}
	// 多选计划的 pendingAlias 为空：别名从计划条目推导，与计划本身对齐。
	aliases := planAliases(t.exportPlan)
	if len(aliases) == 0 {
		return t, warnToast("current plan cannot force overwrite")
	}
	targets := make([]agentcfg.Target, 0, len(t.exportPlan.Items))
	seen := map[string]bool{}
	for _, item := range t.exportPlan.Items {
		if seen[item.Agent] {
			continue
		}
		seen[item.Agent] = true
		if target, ok := agentcfg.Find(item.Agent); ok {
			targets = append(targets, target)
		}
	}
	exporter, err := t.exporter(true)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	plan, err := exporter.Plan(targets, aliases)
	if err != nil {
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.exportPlan = plan
	t.planForce = true
	return t, nil
}

// planAliases 返回导出计划涉及的档案别名（保序去重）。
func planAliases(plan *mcp.ExportPlan) []string {
	if plan == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range plan.Items {
		if seen[item.Alias] {
			continue
		}
		seen[item.Alias] = true
		out = append(out, item.Alias)
	}
	return out
}

func (t *mcpTab) confirmPlan() (Tab, tea.Cmd) {
	kind := t.planKind
	exportPlan := t.exportPlan
	unexportPlan := t.unexportPlan
	force := t.planForce
	alias := t.pendingAlias
	allowed := t.changedAllowed
	t.cancelMode()
	t.sel.ClearSelection() // 批量操作提交后清空多选集（grill D9）
	if kind == "unexport" {
		return t, t.executeUnexport(unexportPlan, force, alias, allowed)
	}
	return t, t.executeExport(exportPlan, force, alias)
}

func (t *mcpTab) executeExport(plan *mcp.ExportPlan, force bool, alias string) tea.Cmd {
	if plan == nil {
		return warnToast("no plan to execute")
	}
	if !plan.NeedsWrite() {
		return warnToast("nothing to write")
	}
	mgrs := t.mgr
	if alias == "" {
		alias = t.currentAlias(plan)
	}
	return func() tea.Msg {
		exporter, err := t.exporter(force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpMCPExport, mcpExportAuditTarget(plan), false, "export failed")
			return errMsg{err: err}
		}
		report, err := exporter.Execute(plan)
		ok := err == nil && report.Failures == 0
		recordAudit(mgrs, session.AuditOpMCPExport, mcpExportAuditTarget(plan), ok, fmt.Sprintf("export %d items", len(report.Items)))
		if err != nil {
			return errMsg{err: err}
		}
		toast := "exported"
		if report.Failures > 0 {
			toast = fmt.Sprintf("export finished, %d failed", report.Failures)
		}
		return mcpReloadMsg{toast: toast, alias: alias}
	}
}

func (t *mcpTab) executeUnexport(plan *mcp.UnexportPlan, force bool, alias string, allowed map[string]bool) tea.Cmd {
	if plan == nil {
		return warnToast("no plan to execute")
	}
	// 与 executeExport 对齐：没有可移除条目时不执行、不记成功审计。
	if !plan.NeedsWrite() {
		return warnToast("nothing to write")
	}
	if allowed == nil {
		allowed = map[string]bool{}
	}
	mgrs := t.mgr
	return func() tea.Msg {
		exporter, err := t.exporter(force)
		if err != nil {
			recordAudit(mgrs, session.AuditOpMCPExport, mcpUnexportAuditTarget(plan, alias), false, "unexport failed")
			return errMsg{err: err}
		}
		report, err := exporter.ExecuteUnexport(plan, func(item mcp.UnexportItem) bool {
			return allowed[item.Agent+"/"+item.Alias]
		})
		ok := err == nil && report.Failures == 0
		recordAudit(mgrs, session.AuditOpMCPExport, mcpUnexportAuditTarget(plan, alias), ok, fmt.Sprintf("unexport %d items", len(report.Items)))
		if err != nil {
			return errMsg{err: err}
		}
		toast := "unexported"
		if report.Failures > 0 {
			toast = fmt.Sprintf("unexport finished, %d failed", report.Failures)
		}
		return mcpReloadMsg{toast: toast, alias: alias}
	}
}

func (t *mcpTab) currentAlias(plan *mcp.ExportPlan) string {
	if t.pendingAlias != "" {
		return t.pendingAlias
	}
	if plan != nil && len(plan.Items) > 0 {
		return plan.Items[0].Alias
	}
	if srv := t.currentServer(); srv != nil {
		return srv.Alias
	}
	return ""
}

func (t *mcpTab) changedItems() []mcp.UnexportItem {
	if t.unexportPlan == nil {
		return nil
	}
	var out []mcp.UnexportItem
	for _, item := range t.unexportPlan.Items {
		if item.Action == mcp.UnexportChanged {
			out = append(out, item)
		}
	}
	return out
}

func mcpExportAuditTarget(plan *mcp.ExportPlan) string {
	if plan == nil || len(plan.Items) == 0 {
		return "mcp:"
	}
	agents := make([]string, 0, len(plan.Items))
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if seen[item.Agent] {
			continue
		}
		seen[item.Agent] = true
		agents = append(agents, item.Agent)
	}
	return "mcp:" + plan.Items[0].Alias + " agents:" + strings.Join(agents, ",")
}

func mcpUnexportAuditTarget(plan *mcp.UnexportPlan, alias string) string {
	if plan == nil || len(plan.Items) == 0 {
		if alias == "" {
			return "mcp:"
		}
		return "mcp:" + alias
	}
	agents := make([]string, 0, len(plan.Items))
	seen := map[string]bool{}
	for _, item := range plan.Items {
		if seen[item.Agent] {
			continue
		}
		seen[item.Agent] = true
		agents = append(agents, item.Agent)
	}
	name := plan.Items[0].Alias
	if name == "" {
		name = alias
	}
	return "mcp:" + name + " agents:" + strings.Join(agents, ",")
}

func (t *mcpTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("MCP") + "\n" + truncateRunes("⚠ "+t.loadErr, maxInt(t.width, 1))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if t.mode == mcpModePlan || t.mode == mcpModeChangedConfirm {
		return clipLines(t.renderPlan(), paneBudget(t.height))
	}
	if t.loaded && len(t.servers) == 0 && t.mode == mcpModeNormal && t.form == nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("MCP"),
			emptyStateStyle.Render("no MCP server profiles yet; press n to create one"))
	}
	overlay := ""
	switch {
	case t.form != nil:
		overlay = t.form.View()
	case t.mode == mcpModeDelete:
		overlay = t.renderDelete()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *mcpTab) viewBaseAt(height int) string {
	leftW := t.width * 2 / 5
	if leftW < 24 {
		leftW = 24
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
		left := emptyStateStyle.Render("loading MCP profiles…")
		right := emptyStateStyle.Render("loading MCP profiles…")
		if t.focusLeft {
			left = activePaneStyle.Width(leftW).Height(height).Render(left)
			right = paneStyle.Width(rightW).Height(height).Render(right)
		} else {
			left = paneStyle.Width(leftW).Height(height).Render(left)
			right = activePaneStyle.Width(rightW).Height(height).Render(right)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
	}
	leftTitle := fmt.Sprintf("profiles (%d)", len(t.visibleServers()))
	if t.filterBox.Active() {
		leftTitle += "  " + t.filterBox.Prompt()
	}
	leftTitle += t.sel.SelectionHint(t.sel.SelectionCount() - t.sel.SelectedIn(t.visibleServerAliases()))
	left := windowedPane(leftTitle, t.serverLines(maxInt(leftW-4, 8)), t.serverIndex, height, leftW)
	right := windowedPane("Agents · export status", t.agentLines(maxInt(rightW-4, 8)), t.agentIndex, height, rightW)
	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(height).Render(left)
		right = paneStyle.Width(rightW).Height(height).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(height).Render(left)
		right = activePaneStyle.Width(rightW).Height(height).Render(right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
}

func (t *mcpTab) serverLines(width int) []string {
	servers := t.visibleServers()
	lines := make([]string, 0, len(servers))
	for i, srv := range servers {
		aliasLabel := srv.Alias
		if t.sel.IsSelected(srv.Alias) {
			aliasLabel = "[x] " + aliasLabel
		}
		line := fmt.Sprintf("%s · %s · %d env", aliasLabel, srv.Target(), len(srv.EnvKeys))
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.serverIndex))
	}
	return lines
}

func (t *mcpTab) agentLines(width int) []string {
	lines := make([]string, 0, len(t.status)+1)
	if t.warning != "" {
		lines = append(lines, mutedStyle().Render(truncateWidth("⚠ "+t.warning, width)))
	}
	for i, row := range t.status {
		line := fmt.Sprintf("%s · %s", row.ID, row.State)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.agentIndex))
	}
	return lines
}

func (t *mcpTab) renderDelete() string {
	body := "only deletes the profile in the vault, not exported entries."
	if len(t.pendingAgents) > 0 {
		body += "\nexported to:" + strings.Join(t.pendingAgents, ", ") + "\npress u to unexport."
	}
	return modalBox(t.width, t.height, "delete "+t.pendingAlias+"?", body, "enter/y confirm · esc/n cancel")
}

func (t *mcpTab) renderPlan() string {
	if t.mode == mcpModeChangedConfirm {
		items := t.changedItems()
		if t.changedIdx >= 0 && t.changedIdx < len(items) {
			item := items[t.changedIdx]
			return modalBox(t.width, t.height, "entry was modified locally",
				fmt.Sprintf("%s / %s\n%s", item.Agent, item.Alias, item.Path),
				"y delete · n skip · esc cancel")
		}
	}
	title := "export plan"
	if t.planKind == "unexport" {
		title = "unexport plan"
	}
	if t.planForce {
		title += " (force overwrite)"
	}
	var b strings.Builder
	if t.planKind == "unexport" && t.unexportPlan != nil {
		for _, item := range t.unexportPlan.Items {
			fmt.Fprintf(&b, "%-16s %-8s %s", item.Agent, item.Action, item.Path)
			if item.Reason != "" {
				fmt.Fprintf(&b, "  — %s", item.Reason)
			}
			b.WriteByte('\n')
		}
	} else if t.exportPlan != nil {
		for _, item := range t.exportPlan.Items {
			fmt.Fprintf(&b, "%-16s %-8s %s", item.Agent, item.Action, item.Path)
			if item.Plaintext && (item.Action == mcp.ActionCreate || item.Action == mcp.ActionUpdate) {
				b.WriteString("  [plaintext]")
			}
			if item.Reason != "" {
				fmt.Fprintf(&b, "  — %s", item.Reason)
			}
			b.WriteByte('\n')
		}
	}
	return modalBox(t.width, t.height, title, strings.TrimRight(b.String(), "\n"),
		"enter/y confirm · F overwrite drift · esc/n cancel")
}

func requiredAlias(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("alias cannot be empty")
	}
	return storage.ValidateName(strings.TrimSpace(v))
}

func requiredCommand(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

func requiredURL(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func validateEnvLines(v string) error {
	_, err := parseEnvLines(v)
	return err
}

func validateHeaderLines(v string) error {
	_, err := parseHeaderLines(v)
	return err
}

// parseHeaderLines parses one "Name: Value" header per line. Values may
// contain ':'; only the first separator is significant.
func parseHeaderLines(raw string) (map[string]string, error) {
	headers := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("header must be \"Name: Value\", got %q", line)
		}
		if _, dup := headers[key]; dup {
			return nil, fmt.Errorf("duplicate header %q", key)
		}
		headers[key] = strings.TrimSpace(value)
	}
	if len(headers) == 0 {
		return nil, nil
	}
	return headers, nil
}

func parseArgLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseEnvLines(raw string) (map[string]string, error) {
	env := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("env must be KEY=VALUE, got %q", line)
		}
		if _, dup := env[key]; dup {
			return nil, fmt.Errorf("duplicate env key %q", key)
		}
		env[key] = value
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

func formatEnvLines(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+env[key])
	}
	return strings.Join(lines, "\n")
}

func mcpEnvPreview(raw string) string {
	keys := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, found := strings.Cut(line, "=")
		if found && key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return "(empty, press e to edit in $EDITOR)"
	}
	return strings.Join(keys, ", ") + "(press e to edit)"
}

// mcpHeadersPreview mirrors mcpEnvPreview for the headers editor field: only
// header names are shown, never values.
func mcpHeadersPreview(raw string) string {
	keys := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(key) != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	if len(keys) == 0 {
		return "(empty, press e to edit in $EDITOR)"
	}
	return strings.Join(keys, ", ") + "(press e to edit)"
}

var _ Tab = (*mcpTab)(nil)
