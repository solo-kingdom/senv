package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// aiNewCredential is the credential-field sentinel: picking it switches the
// form to the masked own-credential input instead of referencing an entry.
const aiNewCredential = "[new own credential]"

// aiCredentialRefLimit caps how many existing vault entries the credential
// picker offers, so a large vault cannot make the form unusable.
const aiCredentialRefLimit = 200

// aiTab 浏览 LLM provider 档案与各 coding agent 当前指向，并在 Tab 内完成
// 档案增删改、凭据录入与切换/仅换模型。凭据明文只在 SwitchManager 内部与
// 遮蔽输入框中流动，永不进入 Tab 状态或渲染文本。
type aiTab struct {
	mgr           Managers
	width, height int
	loaded        bool
	loadErr       string

	providers []*storage.LLMProviderEntry
	rows      []llm.StatusRow
	warning   string
	// credentialRefs are the selectable existing vault entries (env:<g>/<k>,
	// text:<g>/<k>); the reserved llm-keys group is excluded because it holds
	// provider-owned credentials, not user-selected references.
	credentialRefs []string

	providerIndex int
	agentIndex    int
	focusLeft     bool
	filterBox     Filter // `/` 过滤左栏主列表（provider alias 标识）
	pendingJump   string
	detail        *detailOverlay

	// form 非 nil 时表示打开的 provider 表单；formSubmit 是提交动作。
	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode            aiMode
	pendingProvider string

	flow          aiFlow
	flowAgent     int    // rows 下标（右栏选中的 agent）
	flowProvider  string // 目标 provider alias
	flowOnlyModel bool   // true = m（仅换默认模型），false = s（完整切换）
	// flowCandidates 是本次向导的候选模型（保序）；flowSelected 是多选步骤的
	// 勾选状态；flowCursor 是当前步骤列表里的游标。
	flowCandidates []string
	flowSelected   map[string]bool
	flowCursor     int
}

// aiMode is the destructive confirmation state.
type aiMode int

const (
	aiModeNormal aiMode = iota
	aiModeDeleteProvider
)

// aiFlow is the switch/model-change wizard state.
type aiFlow int

const (
	aiFlowNone aiFlow = iota
	aiFlowSelectModel
	aiFlowSelectDefault
	aiFlowConfirm
)

type aiLoadedMsg struct {
	providers      []*storage.LLMProviderEntry
	rows           []llm.StatusRow
	warning        string
	credentialRefs []string
	err            error
}

// aiSwitchResultMsg carries one switch (or model-only change) result.
type aiSwitchResultMsg struct {
	out       *llm.SwitchOutput
	err       error
	onlyModel bool
}

// aiProviderReloadMsg reports a successful profile write; the tab reloads and
// parks the cursor on the named provider.
type aiProviderReloadMsg struct {
	toast         string
	providerAlias string
}

// aiFormReopenMsg re-opens a provider form after a failed submit, preserving
// every field value and attaching the error to the offending field.
type aiFormReopenMsg struct {
	form   *form
	submit func(values map[string]string) tea.Cmd
	values map[string]string
	field  string
	err    error
}

func newAITab(mgr Managers) *aiTab {
	return &aiTab{mgr: mgr, focusLeft: true}
}

func (t *aiTab) Title() string { return "AI" }

func (t *aiTab) Bindings() []KeyAction {
	if t.form != nil {
		return []KeyAction{
			{[]string{"tab/↑↓"}, "switch field", grpForm},
			{[]string{"←→"}, "pick candidate", grpForm},
			{[]string{"enter"}, "submit", grpForm},
			{[]string{"esc"}, "cancel", grpForm},
		}
	}
	switch t.flow {
	case aiFlowSelectModel:
		return []KeyAction{
			{[]string{"space"}, "toggle", grpWizard},
			actUp, actDown,
			{[]string{"enter"}, "next", grpWizard},
			{[]string{"esc"}, "cancel", grpWizard},
		}
	case aiFlowSelectDefault:
		return []KeyAction{actUp, actDown, {[]string{"enter"}, "next", grpWizard}, {[]string{"esc"}, "back", grpWizard}}
	case aiFlowConfirm:
		return []KeyAction{{[]string{"enter/y"}, "confirm", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	}
	if t.mode == aiModeDeleteProvider {
		return []KeyAction{{[]string{"enter/y"}, "confirm", grpConfirm}, {[]string{"esc/n"}, "cancel", grpConfirm}}
	}
	return append([]KeyAction{actUp, actDown, actLeft, actRight, actDetail,
		actTop, actBottom, actPageUp, actPageDn},
		KeyAction{[]string{"n"}, "new provider", grpItem},
		KeyAction{[]string{"e"}, "edit provider", grpItem},
		KeyAction{[]string{"d"}, "delete provider", grpItem},
		KeyAction{[]string{"s"}, "switch (model set + default)", grpItem},
		KeyAction{[]string{"M"}, "default model only", grpItem},
		actFilter, actRefresh,
	)
}

// InputMode reports that the tab owns the keyboard: forms, the switch wizard and
// destructive confirmations must not be interrupted by global shortcuts.
func (t *aiTab) InputMode() bool {
	return t.form != nil || t.flow != aiFlowNone || t.mode != aiModeNormal || t.filterBox.Active()
}

// visibleProviders 返回过滤后的左栏可见列表（空词 = 全量）。
func (t *aiTab) visibleProviders() []*storage.LLMProviderEntry {
	if t.filterBox.Term() == "" {
		return t.providers
	}
	out := make([]*storage.LLMProviderEntry, 0, len(t.providers))
	for _, p := range t.providers {
		if t.filterBox.Matches(p.Alias) {
			out = append(out, p)
		}
	}
	return out
}

// clampLeft 把左栏游标收回可见范围。
func (t *aiTab) clampLeft() {
	n := len(t.visibleProviders())
	if t.providerIndex >= n {
		t.providerIndex = maxInt(n-1, 0)
	}
}

func (t *aiTab) SetSize(width, height int) {
	t.width, t.height = width, height
}

func (t *aiTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// Reload drops cached data and reloads; the top level calls it after a
// background sync applies remote changes.
func (t *aiTab) Reload() tea.Cmd {
	// stale-while-revalidate：后台重载期间旧数据保持可见，完成后静默替换。
	return t.load()
}

func (t *aiTab) load() tea.Cmd {
	return func() tea.Msg {
		st := perflog.Start("tui.load-ai")
		if t.mgr.LLM == nil {
			st.End(true)
			return aiLoadedMsg{}
		}
		providers, err := t.mgr.LLM.ListProviders()
		if err != nil {
			st.End(false)
			return aiLoadedMsg{err: err}
		}
		sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
		rows, warning, err := sm.Status()
		if err != nil {
			st.End(false)
			return aiLoadedMsg{err: err}
		}
		st.With("providers", len(providers)).End(true)
		return aiLoadedMsg{
			providers:      providers,
			rows:           rows,
			warning:        warning,
			credentialRefs: gatherCredentialRefs(t.mgr),
		}
	}
}

// gatherCredentialRefs collects selectable credential references from the
// vault. Only keys are read — values are never touched on this path.
func gatherCredentialRefs(mgr Managers) []string {
	var refs []string
	if mgr.Env != nil {
		if vars, _, err := envSnapshot(mgr); err == nil {
			for g, keys := range vars {
				for key := range keys {
					refs = append(refs, "env:"+g+"/"+key)
				}
			}
		}
	}
	if mgr.Text != nil {
		if groups, err := mgr.Text.ListGroups(); err == nil {
			for _, g := range groups {
				if g.Name == llm.LLMKeysGroup {
					continue
				}
				infos, err := mgr.Text.List(g.Name)
				if err != nil {
					continue
				}
				for _, info := range infos {
					refs = append(refs, "text:"+g.Name+"/"+info.Key)
				}
			}
		}
	}
	sort.Strings(refs)
	if len(refs) > aiCredentialRefLimit {
		refs = refs[:aiCredentialRefLimit]
	}
	return refs
}

func (t *aiTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	// Form results and failed-submit reopen must be handled before routing
	// further messages into a (possibly absent) form.
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
	case aiFormReopenMsg:
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
	case aiLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.providers = msg.providers
		t.rows = msg.rows
		t.warning = msg.warning
		t.credentialRefs = msg.credentialRefs
		t.clamp()
		t.applyPendingJump()
		return t, nil

	case aiProviderReloadMsg:
		t.cancelMode()
		if msg.providerAlias != "" {
			t.pendingJump = msg.providerAlias
		}
		cmd := t.load()
		if msg.toast != "" {
			return t, tea.Batch(okToast(msg.toast), cmd)
		}
		return t, cmd

	case aiSwitchResultMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		out := msg.out
		notice := fmt.Sprintf("%s → %s (default %s, %d models)",
			out.AgentName, out.Provider, out.DefaultModel, len(out.Models))
		if msg.onlyModel {
			notice = fmt.Sprintf("%s default model only → %s", out.AgentName, out.DefaultModel)
		}
		if out.CredentialEnv != "" {
			notice += fmt.Sprintf("; %s reads credentials from env %s", out.AgentName, out.CredentialEnv)
		}
		if len(out.Warnings) > 0 {
			notice += "; " + out.Warnings[0]
		}
		return t, tea.Batch(okToast(notice), t.load())

	case detailCloseMsg:
		t.detail = nil
		return t, nil

	case tea.KeyMsg:
		if t.filterBox.Active() && t.form == nil && t.flow == aiFlowNone && t.mode == aiModeNormal {
			switch msg.String() {
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
			if isPrintable(msg) {
				t.filterBox.Append(msg.String())
				t.clampLeft()
			}
			// 过滤输入态吞掉其余按键（audit 范式）：导航/实体动作不得在
			// 编辑过滤词时透传触发。
			return t, nil
		}
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		if t.flow != aiFlowNone {
			return t.updateFlow(msg)
		}
		if t.mode != aiModeNormal {
			return t.updateMode(msg)
		}
		return t.updateKey(msg)
	}
	return t, nil
}

func (t *aiTab) updateKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if t.focusLeft && t.providerIndex > 0 {
			t.providerIndex--
		} else if !t.focusLeft && t.agentIndex > 0 {
			t.agentIndex--
		}
	case "down", "j":
		if t.focusLeft && t.providerIndex < len(t.visibleProviders())-1 {
			t.providerIndex++
		} else if !t.focusLeft && t.agentIndex < len(t.rows)-1 {
			t.agentIndex++
		}
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "/":
		t.focusLeft = true
		t.filterBox.EnterFresh()
		return t, nil
	case "ctrl+r":
		t.loaded = false
		return t, t.load()
	case "enter":
		return t, t.openDetail()
	case "n":
		if t.focusLeft {
			return t.enterProviderForm(nil)
		}
	case "e":
		if t.focusLeft {
			if p := t.currentProvider(); p != nil {
				return t.enterProviderForm(p)
			}
			return t, warnToast("no provider selected")
		}
	case "d":
		if t.focusLeft {
			return t.enterDeleteProvider()
		}
	case "s":
		return t.startSwitch(false)
	case "M":
		// M=仅换默认模型（与 s 成对，大写为变体；grill D7）
		return t.startSwitch(true)
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
func (t *aiTab) cursorForFocus() int {
	if t.focusLeft {
		return t.providerIndex
	}
	return t.agentIndex
}

func (t *aiTab) focusListLen() int {
	if t.focusLeft {
		return len(t.visibleProviders())
	}
	return len(t.rows)
}

func (t *aiTab) jumpFocus(idx int) {
	n := t.focusListLen()
	if n == 0 || idx < 0 {
		idx = 0
	} else if idx > n-1 {
		idx = n - 1
	}
	if t.focusLeft {
		t.providerIndex = idx
	} else {
		t.agentIndex = idx
	}
}

func (t *aiTab) updateFlow(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch t.flow {
	case aiFlowSelectModel:
		switch msg.String() {
		case "up", "k":
			if t.flowCursor > 0 {
				t.flowCursor--
			}
		case "down", "j":
			if t.flowCursor < len(t.flowCandidates)-1 {
				t.flowCursor++
			}
		case " ":
			if model, ok := t.cursorCandidate(); ok {
				if t.flowSelected[model] {
					delete(t.flowSelected, model)
				} else {
					t.flowSelected[model] = true
				}
			}
		case "enter":
			// 空集在提交前拦截：不进入下一步，也不触碰 SwitchManager。
			if len(t.flowSelectedModels()) == 0 {
				return t, warnToast("agent model set cannot be empty: select at least one model")
			}
			t.flowCursor = t.defaultModelCursor()
			t.flow = aiFlowSelectDefault
		case "esc":
			t.cancelMode()
		}
	case aiFlowSelectDefault:
		models := t.flowSelectedModels()
		switch msg.String() {
		case "up", "k":
			if t.flowCursor > 0 {
				t.flowCursor--
			}
		case "down", "j":
			if t.flowCursor < len(models)-1 {
				t.flowCursor++
			}
		case "enter":
			if len(models) > 0 {
				t.flow = aiFlowConfirm
			}
		case "esc":
			if t.flowOnlyModel {
				t.cancelMode()
				break
			}
			// 回到多选步骤重挑 Agent 模型集。
			t.flow = aiFlowSelectModel
			t.flowCursor = 0
		}
	case aiFlowConfirm:
		switch msg.String() {
		case "enter", "y":
			onlyModel := t.flowOnlyModel
			t.flow = aiFlowNone
			return t, t.switchCmd(onlyModel)
		case "esc", "n":
			t.cancelMode()
		}
	}
	return t, nil
}

func (t *aiTab) updateMode(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch t.mode {
	case aiModeDeleteProvider:
		switch msg.String() {
		case "enter", "y":
			alias := t.pendingProvider
			return t.doDeleteProvider(alias)
		case "esc", "n":
			t.cancelMode()
		}
	}
	return t, nil
}

// cancelMode clears every staged wizard/confirmation field.
func (t *aiTab) cancelMode() {
	t.mode = aiModeNormal
	t.pendingProvider = ""
	t.flow = aiFlowNone
	t.flowProvider = ""
	t.flowOnlyModel = false
	t.flowCandidates = nil
	t.flowSelected = nil
	t.flowCursor = 0
}

// startSwitch begins the switch (onlyModel=false) or default-model-change
// (onlyModel=true) wizard for the agent selected in the right pane.
func (t *aiTab) startSwitch(onlyModel bool) (Tab, tea.Cmd) {
	if len(t.rows) == 0 {
		return t, warnToast("no agent to operate on")
	}
	row := t.rows[clamp(t.agentIndex, 0, len(t.rows)-1)]
	if !row.Supported {
		return t, warnToast("agent " + row.AgentID + " switching not supported yet")
	}
	var alias string
	if onlyModel {
		if row.Pointer == nil {
			return t, warnToast("press s first for " + row.AgentID + ": choose a provider")
		}
		alias = row.Pointer.Provider
	} else {
		// 游标是过滤可见列表上的位置：一律经 currentProvider 定位。
		provider := t.currentProvider()
		if provider == nil {
			return t, warnToast("no provider selected")
		}
		alias = provider.Alias
	}
	entry := t.providerByAlias(alias)
	if entry == nil {
		return t, warnToast("provider " + alias + " does not exist")
	}
	if len(entry.Models) == 0 {
		return t, warnToast("provider " + alias + " has no usable models")
	}
	t.flowProvider = alias
	t.flowAgent = clamp(t.agentIndex, 0, len(t.rows)-1)
	t.flowOnlyModel = onlyModel
	if onlyModel {
		// m 只在已写入该 agent 的 Agent 模型集内换默认模型，不动模型集。
		candidates := append([]string(nil), row.Pointer.Models...)
		if len(candidates) == 0 {
			return t, warnToast("pointer has no agent model set recorded, press s to switch again first")
		}
		t.flowCandidates = candidates
		t.flowSelected = allModelsSelected(candidates)
		t.flowCursor = t.defaultModelCursor()
		t.flow = aiFlowSelectDefault
		return t, nil
	}
	t.flowCandidates = append([]string(nil), entry.Models...)
	t.flowSelected = allModelsSelected(entry.Models)
	t.flowCursor = 0
	t.flow = aiFlowSelectModel
	return t, nil
}

// allModelsSelected 返回候选集合的「默认全选」状态（grill D3）。
func allModelsSelected(models []string) map[string]bool {
	selected := make(map[string]bool, len(models))
	for _, model := range models {
		selected[model] = true
	}
	return selected
}

// cursorCandidate 返回多选步骤游标下的候选模型。
func (t *aiTab) cursorCandidate() (string, bool) {
	if t.flowCursor < 0 || t.flowCursor >= len(t.flowCandidates) {
		return "", false
	}
	return t.flowCandidates[t.flowCursor], true
}

// flowRow 返回本次向导选中的 agent 行。
func (t *aiTab) flowRow() *llm.StatusRow {
	if t.flowAgent < 0 || t.flowAgent >= len(t.rows) {
		return nil
	}
	return &t.rows[t.flowAgent]
}

// flowSelectedModels 返回当前勾选的 Agent 模型集（按候选顺序，保序）。
func (t *aiTab) flowSelectedModels() []string {
	models := make([]string, 0, len(t.flowCandidates))
	for _, model := range t.flowCandidates {
		if t.flowSelected[model] {
			models = append(models, model)
		}
	}
	return models
}

// defaultModelCursor 返回默认模型步骤的初始游标：优先档案默认模型（m 时优先
// 当前默认模型），不在已勾选集合内时落首项。
func (t *aiTab) defaultModelCursor() int {
	models := t.flowSelectedModels()
	if len(models) == 0 {
		return 0
	}
	preferred := ""
	if entry := t.providerByAlias(t.flowProvider); entry != nil {
		preferred = entry.DefaultModel
	}
	if t.flowOnlyModel {
		if row := t.flowRow(); row != nil && row.Pointer != nil && row.Pointer.DefaultModel != "" {
			preferred = row.Pointer.DefaultModel
		}
	}
	for i, model := range models {
		if model == preferred {
			return i
		}
	}
	return 0
}

func (t *aiTab) switchCmd(onlyModel bool) tea.Cmd {
	models := t.flowSelectedModels()
	row := t.flowRow()
	if len(models) == 0 || row == nil {
		return nil
	}
	agentID := row.AgentID
	provider := t.flowProvider
	defaultModel := models[clamp(t.flowCursor, 0, len(models)-1)]
	sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
	return func() tea.Msg {
		out, err := sm.Switch(agentID, provider, models, defaultModel)
		return aiSwitchResultMsg{out: out, err: err, onlyModel: onlyModel}
	}
}

// --- provider write flows ---

func (t *aiTab) openForm(f *form, onSubmit func(values map[string]string) tea.Cmd) {
	f.SetSize(t.width, t.height)
	t.form = f
	t.formSubmit = onSubmit
}

// enterProviderForm opens the create (existing == nil) or edit form. In edit
// mode the alias is shown read-only in the title, never as an editable field.
func (t *aiTab) enterProviderForm(existing *storage.LLMProviderEntry) (Tab, tea.Cmd) {
	create := existing == nil
	base := storage.LLMProviderEntry{}
	if existing != nil {
		base = *existing
	}

	credentialOptions := []string{aiNewCredential}
	credentialOptions = append(credentialOptions, t.credentialRefs...)
	if base.CredentialRef != "" && !containsString(credentialOptions, base.CredentialRef) {
		credentialOptions = append(credentialOptions, base.CredentialRef)
	}
	credentialValue := base.CredentialRef
	if credentialValue == "" {
		credentialValue = aiNewCredential
	}
	allowHTTP := "no"
	if strings.HasPrefix(base.BaseURL, "http://") {
		allowHTTP = "yes"
	}

	title := "new LLM provider"
	fields := make([]formField, 0, 9)
	if create {
		fields = append(fields, formField{
			key: "alias", label: "alias", kind: formText, placeholder: "main",
		})
	} else {
		title = "edit provider " + existing.Alias
	}
	fields = append(fields,
		formField{
			key: "base_url", label: "base_url", kind: formText, value: base.BaseURL,
			placeholder: "https://api.example.com/v1",
		},
		formField{
			key: "api_shape", label: "api shape", kind: formEnum, value: base.APIShape,
			options: []string{
				storage.LLMAPIShapeOpenAIChat,
				storage.LLMAPIShapeOpenAIResponses,
				storage.LLMAPIShapeAnthropic,
			},
			optional: true,
		},
		formField{
			key: "catalog", label: "catalog provider", kind: formText, value: base.CatalogProvider,
			placeholder: "models.dev provider id (optional)",
		},
		formField{
			key: "models", label: "model set", kind: formText, value: strings.Join(base.Models, ", "),
			placeholder: "m1, m2 (comma separated)",
		},
		formField{
			key: "model_contexts", label: "model contexts", kind: formText,
			value:       formatModelContexts(base.Models, base.ModelInfo),
			placeholder: "m1=1000000 (required for custom models without catalog metadata)",
		},
		formField{
			key: "model_outputs", label: "model outputs", kind: formText,
			value:       formatModelOutputs(base.Models, base.ModelInfo),
			placeholder: "m1=32000 (output cap, optional)",
		},
		formField{
			key: "model_reasoning", label: "model reasoning", kind: formText,
			value:       formatModelReasoning(base.Models, base.ModelInfo),
			placeholder: "m1=low;high (reasoning effort, optional)",
		},
		formField{
			key: "model_default_reasoning", label: "default reasoning", kind: formText,
			value:       formatModelDefaultReasoning(base.Models, base.ModelInfo),
			placeholder: "m1=high or set-level high (required when efforts exist)",
		},
		formField{
			key: "model_modalities", label: "input modalities", kind: formText,
			value:       formatModelModalities(base.Models, base.ModelInfo),
			placeholder: "m1=text,image (optional)",
		},
		formField{
			key: "default_model", label: "default model", kind: formText, value: base.DefaultModel,
			placeholder: "m1 (optional)",
		},
		formField{
			key: "credential", label: "credential source", kind: formRef, value: credentialValue,
			options: credentialOptions,
		},
		formField{
			key: "api_key", label: "own credential", kind: formSecret,
			placeholder: "only when picking [new own credential]",
		},
		formField{
			key: "allow_http", label: "allow HTTP", kind: formEnum, value: allowHTTP,
			options: []string{"no", "yes"},
		},
	)

	f := newForm(title, fields...)
	var submit func(values map[string]string) tea.Cmd
	submit = func(values map[string]string) tea.Cmd {
		return t.doSubmitProvider(existing, values, f, submit)
	}
	t.openForm(f, submit)
	return t, nil
}

// doSubmitProvider validates locally (re-opening the form with an inline error)
// and then writes through AddProvider/EditProvider.
func (t *aiTab) doSubmitProvider(existing *storage.LLMProviderEntry, values map[string]string, f *form, submit func(map[string]string) tea.Cmd) tea.Cmd {
	create := existing == nil
	reopen := func(field string, err error) tea.Cmd {
		return func() tea.Msg {
			return aiFormReopenMsg{form: f, submit: submit, values: values, field: field, err: err}
		}
	}

	alias := ""
	if create {
		alias = strings.TrimSpace(values["alias"])
		if alias == "" {
			return reopen("alias", fmt.Errorf("alias cannot be empty"))
		}
		if err := storage.ValidateName(alias); err != nil {
			return reopen("alias", fmt.Errorf("invalid alias"))
		}
		if t.providerByAlias(alias) != nil {
			return reopen("alias", fmt.Errorf("provider %s already exists", alias))
		}
	} else {
		alias = existing.Alias
	}

	baseURL := strings.TrimSpace(values["base_url"])
	if baseURL == "" {
		return reopen("base_url", fmt.Errorf("base_url cannot be empty"))
	}
	allowHTTP := strings.TrimSpace(values["allow_http"]) == "yes"
	if err := storage.ValidateLLMProviderURL(baseURL, allowHTTP); err != nil {
		return reopen("base_url", err)
	}
	apiShape := strings.TrimSpace(values["api_shape"])
	if err := storage.ValidateLLMProviderAPIShape(apiShape); err != nil {
		return reopen("api_shape", err)
	}
	catalog := strings.TrimSpace(values["catalog"])
	models := parseModelList(values["models"])
	modelContexts, err := llm.ParseModelContexts(parseModelList(values["model_contexts"]))
	if err != nil {
		return reopen("model_contexts", err)
	}
	modelOutputs, err := llm.ParseModelOutputs(parseModelList(values["model_outputs"]))
	if err != nil {
		return reopen("model_outputs", err)
	}
	modelReasoning, err := llm.ParseModelReasoning(parseModelList(values["model_reasoning"]))
	if err != nil {
		return reopen("model_reasoning", err)
	}
	modelDefaultReasoning, collectionDefault, err := parseDefaultReasoningField(values["model_default_reasoning"])
	if err != nil {
		return reopen("model_default_reasoning", err)
	}
	modelModalities, err := llm.ParseModelModalities(parseModalityList(values["model_modalities"]))
	if err != nil {
		return reopen("model_modalities", err)
	}
	if catalog == "" && len(models) == 0 {
		return reopen("models", fmt.Errorf("model set cannot be empty: fill in models or a catalog provider"))
	}
	defaultModel := strings.TrimSpace(values["default_model"])
	if catalog == "" && defaultModel != "" && !containsString(models, defaultModel) {
		return reopen("default_model", fmt.Errorf("default model %s not in model set", defaultModel))
	}
	credential := strings.TrimSpace(values["credential"])
	apiKey := strings.TrimSpace(values["api_key"])
	switch {
	case credential == aiNewCredential && apiKey == "":
		return reopen("api_key", fmt.Errorf("[new own credential] requires an API key"))
	case credential != aiNewCredential && apiKey != "":
		return reopen("api_key", fmt.Errorf("clear the own-credential field when an existing entry is selected"))
	case credential == "" && create:
		return reopen("credential", fmt.Errorf("choose a credential source or [new own credential]"))
	}

	mgr := t.mgr.LLM
	mgrs := t.mgr
	catalogPath := t.mgr.LLMCatalog

	if create {
		opts := llm.AddProviderOptions{
			Alias:                 alias,
			BaseURL:               baseURL,
			AllowHTTP:             allowHTTP,
			CatalogPath:           catalogPath,
			CatalogProvider:       catalog,
			Models:                models,
			ModelContexts:         modelContexts,
			ModelOutputs:          modelOutputs,
			ModelReasoning:        modelReasoning,
			ModelDefaultReasoning: modelDefaultReasoning,
			DefaultReasoning:      collectionDefault,
			ModelModalities:       modelModalities,
			RequireModelMetadata:  true,
			DefaultModel:          defaultModel,
			APIShape:              apiShape,
		}
		if credential == aiNewCredential {
			opts.APIKey = apiKey
		} else {
			opts.KeyRef = credential
		}
		return func() tea.Msg {
			res, err := mgr.AddProvider(opts)
			if err != nil {
				recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "add failed")
				return aiFormReopenMsg{form: f, submit: submit, values: values, field: providerErrorField(err), err: err}
			}
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "add")
			toast := fmt.Sprintf("saved provider %s (%d models)", res.Entry.Alias, len(res.Entry.Models))
			if len(res.Warnings) > 0 {
				toast += "; " + truncateWidth(res.Warnings[0], 40)
			}
			return aiProviderReloadMsg{toast: toast, providerAlias: res.Entry.Alias}
		}
	}

	opts := llm.EditProviderOptions{
		Alias:        alias,
		AllowHTTP:    allowHTTP,
		CatalogPath:  catalogPath,
		BaseURL:      &baseURL,
		APIShape:     &apiShape,
		DefaultModel: &defaultModel,
	}
	contextsChanged := strings.TrimSpace(values["model_contexts"]) != strings.TrimSpace(formatModelContexts(existing.Models, existing.ModelInfo))
	outputsChanged := strings.TrimSpace(values["model_outputs"]) != strings.TrimSpace(formatModelOutputs(existing.Models, existing.ModelInfo))
	reasoningChanged := strings.TrimSpace(values["model_reasoning"]) != strings.TrimSpace(formatModelReasoning(existing.Models, existing.ModelInfo))
	defaultReasoningChanged := strings.TrimSpace(values["model_default_reasoning"]) != strings.TrimSpace(formatModelDefaultReasoning(existing.Models, existing.ModelInfo))
	modalitiesChanged := strings.TrimSpace(values["model_modalities"]) != strings.TrimSpace(formatModelModalities(existing.Models, existing.ModelInfo))
	if !equalStrings(models, existing.Models) || catalog != existing.CatalogProvider || contextsChanged ||
		reasoningChanged || defaultReasoningChanged {
		opts.Models = models
		opts.CatalogProvider = &catalog
		opts.RequireModelMetadata = true
	}
	// 变更字段才进 opts；nil 解析结果按「显式清空」传递（ClearingMap），
	// 否则编辑入口的清空意图会被档案旧值回填吞掉。未变更字段 MUST NOT
	// 进 opts，避免空哨兵误清未展示的目录元数据。
	if contextsChanged {
		opts.ModelContexts = llm.ClearingMap(modelContexts)
	}
	if outputsChanged {
		opts.ModelOutputs = llm.ClearingMap(modelOutputs)
	}
	if reasoningChanged {
		opts.ModelReasoning = llm.ClearingMap(modelReasoning)
	}
	if defaultReasoningChanged {
		opts.ModelDefaultReasoning = llm.ClearingMap(modelDefaultReasoning)
		opts.DefaultReasoning = &collectionDefault
	}
	if modalitiesChanged {
		opts.ModelModalities = llm.ClearingMap(modelModalities)
	}
	if credential == aiNewCredential {
		opts.APIKey = apiKey
	} else if credential != existing.CredentialRef {
		ref := credential
		opts.KeyRef = &ref
	}
	return func() tea.Msg {
		res, err := mgr.EditProvider(opts)
		if err != nil {
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "edit failed")
			return aiFormReopenMsg{form: f, submit: submit, values: values, field: providerErrorField(err), err: err}
		}
		recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "edit")
		toast := fmt.Sprintf("updated provider %s (%d models)", res.Entry.Alias, len(res.Entry.Models))
		if len(res.Warnings) > 0 {
			toast += "; " + truncateWidth(res.Warnings[0], 40)
		}
		return aiProviderReloadMsg{toast: toast, providerAlias: res.Entry.Alias}
	}
}

func (t *aiTab) enterDeleteProvider() (Tab, tea.Cmd) {
	p := t.currentProvider()
	if p == nil {
		return t, warnToast("no provider to delete")
	}
	t.pendingProvider = p.Alias
	t.mode = aiModeDeleteProvider
	return t, nil
}

func (t *aiTab) doDeleteProvider(alias string) (Tab, tea.Cmd) {
	mgr := t.mgr.LLM
	mgrs := t.mgr
	t.cancelMode()
	return t, func() tea.Msg {
		res, err := mgr.RemoveProvider(alias)
		if err != nil {
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "remove failed")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, true, "remove")
		toast := "removed provider " + alias
		if res.CredentialRemoved {
			toast += " (including own credential)"
		}
		return aiProviderReloadMsg{toast: toast}
	}
}

// providerErrorField maps a backend error to the form field that should show it
// inline, so a failed provider write keeps the user's input.
func providerErrorField(err error) string {
	if err == nil {
		return "alias"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "base url"):
		return "base_url"
	case strings.Contains(msg, "api_shape"):
		return "api_shape"
	case strings.Contains(msg, "default model"):
		return "default_model"
	case strings.Contains(msg, "credential"), strings.Contains(msg, "api key"), strings.Contains(msg, "key"):
		return "api_key"
	case strings.Contains(msg, "context"):
		return "model_contexts"
	case strings.Contains(msg, "output"):
		return "model_outputs"
	case strings.Contains(msg, "default reasoning"):
		return "model_default_reasoning"
	case strings.Contains(msg, "modalit"):
		return "model_modalities"
	case strings.Contains(msg, "reasoning"):
		return "model_reasoning"
	case strings.Contains(msg, "model"):
		return "models"
	case strings.Contains(msg, "catalog"):
		return "catalog"
	}
	return "alias"
}

// --- detail ---

func (t *aiTab) openDetail() tea.Cmd {
	if t.focusLeft {
		p := t.currentProvider()
		if p == nil {
			return warnToast("no provider selected")
		}
		t.detail = newDetailOverlay("Provider "+p.Alias, t.providerDetailLines(p))
	} else {
		if len(t.rows) == 0 {
			return warnToast("no agent selected")
		}
		row := t.rows[clamp(t.agentIndex, 0, len(t.rows)-1)]
		t.detail = newDetailOverlay("Agent "+row.AgentID, agentDetailLines(row))
	}
	t.detail.SetSize(t.width, t.height)
	return nil
}

func (t *aiTab) providerDetailLines(p *storage.LLMProviderEntry) []string {
	lines := []string{
		"alias:          " + p.Alias,
		"base_url:       " + p.BaseURL,
		"api_shape:      " + orDash(p.APIShape),
		"credential_ref: " + p.CredentialRef,
		"catalog:        " + orDash(p.CatalogProvider),
		"default_model:  " + orDash(p.DefaultModel),
		fmt.Sprintf("models (%d):", len(p.Models)),
	}
	if len(p.Models) == 0 {
		lines = append(lines, "  -")
	}
	for _, m := range p.Models {
		line := "  " + m
		if info, ok := p.ModelInfo[m]; ok {
			if info.ContextWindow > 0 {
				line += fmt.Sprintf("  context=%d", info.ContextWindow)
			}
			if info.OutputLimit > 0 {
				line += fmt.Sprintf("  output=%d", info.OutputLimit)
			}
			if len(info.ReasoningEfforts) > 0 {
				line += "  reasoning=" + strings.Join(info.ReasoningEfforts, ";")
			}
			if info.DefaultReasoning != "" {
				line += "  default_reasoning=" + info.DefaultReasoning
			}
			if len(info.InputModalities) > 0 {
				line += "  modalities=" + strings.Join(info.InputModalities, ",")
			}
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "pointed-to agent:")
	used := false
	for _, r := range t.rows {
		if r.Pointer != nil && r.Pointer.Provider == p.Alias {
			lines = append(lines, "  "+r.AgentID+" · "+r.Pointer.DefaultModel)
			used = true
		}
	}
	if !used {
		lines = append(lines, "  -")
	}
	if t.warning != "" {
		lines = append(lines, "", "⚠ "+t.warning)
	}
	return lines
}

func agentDetailLines(row llm.StatusRow) []string {
	state := "not switched"
	switch {
	case !row.Supported:
		state = "unsupported"
	case row.Pointer != nil:
		state = row.Pointer.Provider + " / " + row.Pointer.DefaultModel
	}
	lines := []string{
		"agent:       " + row.AgentID,
		"name:        " + row.AgentName,
		"supported:   " + fmt.Sprintf("%t", row.Supported),
		"pointer:     " + state,
		"config:      " + orDash(row.ConfigPath),
	}
	if !row.Supported {
		lines = append(lines, "senv cannot write back this agent's config (no public schema)")
	}
	return lines
}

// --- lookups ---

func (t *aiTab) currentProvider() *storage.LLMProviderEntry {
	providers := t.visibleProviders()
	if t.providerIndex < 0 || t.providerIndex >= len(providers) {
		return nil
	}
	return providers[t.providerIndex]
}

func (t *aiTab) providerByAlias(alias string) *storage.LLMProviderEntry {
	for _, p := range t.providers {
		if p.Alias == alias {
			return p
		}
	}
	return nil
}

func (t *aiTab) clamp() {
	if t.providerIndex >= len(t.providers) {
		t.providerIndex = len(t.providers) - 1
	}
	if t.providerIndex < 0 {
		t.providerIndex = 0
	}
	if t.agentIndex >= len(t.rows) {
		t.agentIndex = len(t.rows) - 1
	}
	if t.agentIndex < 0 {
		t.agentIndex = 0
	}
}

// focusJump positions the cursor on a provider alias (global search target).
func (t *aiTab) focusJump(group, alias string) {
	t.pendingJump = alias
	t.applyPendingJump()
}

func (t *aiTab) applyPendingJump() {
	if t.pendingJump == "" {
		return
	}
	t.filterBox.Clear() // 跳转前清过滤，保证目标可见
	for i, p := range t.providers {
		if p.Alias == t.pendingJump {
			t.providerIndex = i
			t.focusLeft = true
			t.pendingJump = ""
			return
		}
	}
}

// --- helpers ---

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// parseModelList splits a comma-separated model field, dropping blanks.
func parseModelList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && !containsString(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// parseModalityList 保留模态列表里的逗号：`s1=text,image` 不能被 parseModelList
// 拆成非法的 `image` 片段。
func parseModalityList(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "=") || len(out) == 0 {
			out = append(out, p)
			continue
		}
		out[len(out)-1] += "," + p
	}
	return out
}

func formatModelContexts(models []string, info map[string]storage.LLMModelInfo) string {
	parts := make([]string, 0, len(models))
	for _, model := range models {
		if meta, ok := info[model]; ok && meta.ContextWindow > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", model, meta.ContextWindow))
		}
	}
	return strings.Join(parts, ", ")
}

// formatModelOutputs 把档案里的输出上限渲染回表单字段（与 model_contexts 同风格）。
func formatModelOutputs(models []string, info map[string]storage.LLMModelInfo) string {
	parts := make([]string, 0, len(models))
	for _, model := range models {
		if meta, ok := info[model]; ok && meta.OutputLimit > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", model, meta.OutputLimit))
		}
	}
	return strings.Join(parts, ", ")
}

// formatModelReasoning 把档案里的推理档位渲染回表单字段；档位用分号分隔，
// 避免与字段级逗号分隔符冲突。
func formatModelReasoning(models []string, info map[string]storage.LLMModelInfo) string {
	parts := make([]string, 0, len(models))
	for _, model := range models {
		if meta, ok := info[model]; ok && len(meta.ReasoningEfforts) > 0 {
			parts = append(parts, model+"="+strings.Join(meta.ReasoningEfforts, ";"))
		}
	}
	return strings.Join(parts, ", ")
}

func formatModelDefaultReasoning(models []string, info map[string]storage.LLMModelInfo) string {
	parts := make([]string, 0, len(models))
	for _, model := range models {
		if meta, ok := info[model]; ok && meta.DefaultReasoning != "" {
			parts = append(parts, model+"="+meta.DefaultReasoning)
		}
	}
	return strings.Join(parts, ", ")
}

func formatModelModalities(models []string, info map[string]storage.LLMModelInfo) string {
	parts := make([]string, 0, len(models))
	for _, model := range models {
		if meta, ok := info[model]; ok && len(meta.InputModalities) > 0 {
			parts = append(parts, model+"="+strings.Join(meta.InputModalities, ","))
		}
	}
	return strings.Join(parts, ", ")
}

// parseDefaultReasoningField 接受 per-model `m1=high`（逗号分隔）或集合级单一档位。
func parseDefaultReasoningField(raw string) (map[string]string, string, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return nil, "", nil
	}
	if strings.Contains(spec, "=") {
		perModel, err := llm.ParseModelDefaultReasoning(parseModelList(spec))
		return perModel, "", err
	}
	return nil, spec, nil
}

// --- view ---

func (t *aiTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("AI") + "\n" + truncateRunes("⚠ "+t.loadErr, maxInt(t.width, 1))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if t.loaded && len(t.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("AI"),
			emptyStateStyle.Render("no LLM provider profiles yet; run senv ai provider add or press n to create one, then r to refresh"))
	}
	overlay := ""
	switch {
	case t.form != nil:
		overlay = t.form.View()
	case t.flow != aiFlowNone:
		overlay = t.renderFlow()
	case t.mode != aiModeNormal:
		overlay = t.renderModal()
	}
	if t.width > 0 && overlay != "" {
		overlay = lipgloss.NewStyle().MaxWidth(t.width).Render(overlay)
	}
	return stackWithOverlay(t.height, overlay, t.viewBaseAt)
}

func (t *aiTab) viewBaseAt(height int) string {
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
		left := emptyStateStyle.Render("loading providers…")
		right := emptyStateStyle.Render("loading providers…")
		if t.focusLeft {
			left = activePaneStyle.Width(leftW).Height(height).Render(left)
			right = paneStyle.Width(rightW).Height(height).Render(right)
		} else {
			left = paneStyle.Width(leftW).Height(height).Render(left)
			right = activePaneStyle.Width(rightW).Height(height).Render(right)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
	}

	providerLines := t.providerLines(maxInt(leftW-4, 8))
	agentLines := t.agentLines(maxInt(rightW-4, 8))

	leftTitle := fmt.Sprintf("Providers (%d)", len(t.visibleProviders()))
	if t.filterBox.Active() {
		leftTitle += "  " + t.filterBox.Prompt()
	}
	left := windowedPane(leftTitle, providerLines, t.providerIndex, height, leftW)
	right := windowedPane("Agents · current target", agentLines, t.agentIndex, height, rightW)

	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(height).Render(left)
		right = paneStyle.Width(rightW).Height(height).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(height).Render(left)
		right = activePaneStyle.Width(rightW).Height(height).Render(right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
}

func (t *aiTab) providerLines(width int) []string {
	pointed := make(map[string]bool, len(t.rows))
	for _, r := range t.rows {
		if r.Pointer != nil {
			pointed[r.Pointer.Provider] = true
		}
	}
	providers := t.visibleProviders()
	lines := make([]string, 0, len(providers))
	for i, p := range providers {
		def := p.DefaultModel
		if def == "" {
			def = "-"
		}
		line := fmt.Sprintf("%s · default %s · %d models", p.Alias, def, len(p.Models))
		if pointed[p.Alias] {
			line += " ●"
		}
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.providerIndex))
	}
	return lines
}

func (t *aiTab) agentLines(width int) []string {
	lines := make([]string, 0, len(t.rows)+1)
	if t.warning != "" {
		lines = append(lines, mutedStyle().Render(truncateWidth("⚠ "+t.warning, width)))
	}
	for i, r := range t.rows {
		state := "not switched"
		switch {
		case !r.Supported:
			state = "unsupported"
		case r.Pointer != nil:
			state = fmt.Sprintf("%s / %s (%d models)",
				r.Pointer.Provider, r.Pointer.DefaultModel, len(r.Pointer.Models))
			if r.Drift != "" {
				state += " ⚠"
			}
		}
		line := fmt.Sprintf("%s · %s", r.AgentID, state)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.agentIndex))
	}
	return lines
}

func (t *aiTab) renderModal() string {
	switch t.mode {
	case aiModeDeleteProvider:
		return modalBox("delete provider "+t.pendingProvider+"?",
			"own credential is deleted too; external references are kept.", "enter/y confirm · esc/n cancel")
	}
	return ""
}

// renderFlow renders the multi-select model set, the default-model picker and
// the final confirmation. The full model list is windowed so a large model set
// cannot push the modal off-screen.
func (t *aiTab) renderFlow() string {
	agent := "agent"
	if row := t.flowRow(); row != nil {
		agent = row.AgentID
	}
	switch t.flow {
	case aiFlowConfirm:
		models := t.flowSelectedModels()
		model := "-"
		if len(models) > 0 {
			model = models[clamp(t.flowCursor, 0, len(models)-1)]
		}
		action := "switch"
		if t.flowOnlyModel {
			action = "default model only"
		}
		return modalBox("confirm"+action,
			fmt.Sprintf("%s → %s / %s (%d models)", agent, t.flowProvider, model, len(models)),
			"enter/y confirm · esc/n cancel")
	case aiFlowSelectDefault:
		models := t.flowSelectedModels()
		lines := make([]string, 0, len(models))
		for i, m := range models {
			label := truncateWidth(m, maxInt(t.width-10, 12))
			lines = append(lines, cursorLine(label, i == clamp(t.flowCursor, 0, len(models)-1)))
		}
		title := fmt.Sprintf("choose default model (%d models) — %s → %s", len(models), agent, t.flowProvider)
		return modalBox(title, strings.Join(lines, "\n"), "↑↓/jk select · enter next · esc back")
	}
	lines := make([]string, 0, len(t.flowCandidates))
	for i, m := range t.flowCandidates {
		mark := "[ ]"
		if t.flowSelected[m] {
			mark = "[x]"
		}
		label := mark + " " + truncateWidth(m, maxInt(t.width-14, 12))
		lines = append(lines, cursorLine(label, i == clamp(t.flowCursor, 0, len(t.flowCandidates)-1)))
	}
	title := fmt.Sprintf("choose agent model set (selected %d/%d) — %s → %s",
		len(t.flowSelectedModels()), len(t.flowCandidates), agent, t.flowProvider)
	return modalBox(title, strings.Join(lines, "\n"), "space toggle · ↑↓/jk move · enter next · esc cancel")
}
