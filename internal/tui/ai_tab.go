package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// aiNewCredential is the credential-field sentinel: picking it switches the
// form to the masked own-credential input instead of referencing an entry.
const aiNewCredential = "「新建自有凭据」"

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
	flowOnlyModel bool   // true = m（仅换模型），false = s（完整切换）
	modelIndex    int
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

func (t *aiTab) Help() string {
	if t.form != nil {
		return "tab/↑↓ 切换字段 · ←→ 选候选 · enter 提交 · esc 取消"
	}
	switch t.flow {
	case aiFlowSelectModel:
		return "↑↓/jk 选模型 · enter 下一步 · esc 取消"
	case aiFlowConfirm:
		return "enter/y 确认 · esc/n 取消"
	}
	if t.mode == aiModeDeleteProvider {
		return "enter/y 确认 · esc/n 取消"
	}
	return "↑↓/jk 移动 · ←→/hl 切换栏 · enter 详情 · n 新建 · e 编辑 · d 删除 · s 切换 · m 换模型 · r 刷新"
}

// InputMode reports that the tab owns the keyboard: forms, the switch wizard and
// destructive confirmations must not be interrupted by global shortcuts.
func (t *aiTab) InputMode() bool {
	return t.form != nil || t.flow != aiFlowNone || t.mode != aiModeNormal
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

func (t *aiTab) load() tea.Cmd {
	return func() tea.Msg {
		if t.mgr.LLM == nil {
			return aiLoadedMsg{}
		}
		providers, err := t.mgr.LLM.ListProviders()
		if err != nil {
			return aiLoadedMsg{err: err}
		}
		sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
		rows, warning, err := sm.Status()
		if err != nil {
			return aiLoadedMsg{err: err}
		}
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
		if groups, err := mgr.Env.ListGroups(); err == nil {
			for _, g := range groups {
				vars, err := mgr.Env.List(g.Name)
				if err != nil {
					continue
				}
				for key := range vars[g.Name] {
					refs = append(refs, "env:"+g.Name+"/"+key)
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
		return t, warnToast("已取消")
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
		notice := fmt.Sprintf("%s → %s（模型 %s）", out.AgentName, out.Provider, out.Model)
		if msg.onlyModel {
			notice = fmt.Sprintf("%s 仅换模型 → %s", out.AgentName, out.Model)
		}
		if out.CredentialEnv != "" {
			notice += fmt.Sprintf("；%s 从环境变量 %s 读取凭据", out.AgentName, out.CredentialEnv)
		}
		return t, tea.Batch(okToast(notice), t.load())

	case detailCloseMsg:
		t.detail = nil
		return t, nil

	case tea.KeyMsg:
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
		if t.focusLeft && t.providerIndex < len(t.providers)-1 {
			t.providerIndex++
		} else if !t.focusLeft && t.agentIndex < len(t.rows)-1 {
			t.agentIndex++
		}
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "r":
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
			return t, warnToast("没有选中的 provider")
		}
	case "d":
		if t.focusLeft {
			return t.enterDeleteProvider()
		}
	case "s":
		return t.startSwitch(false)
	case "m":
		return t.startSwitch(true)
	}
	return t, nil
}

func (t *aiTab) updateFlow(msg tea.KeyMsg) (Tab, tea.Cmd) {
	models := t.flowModels()
	switch t.flow {
	case aiFlowSelectModel:
		switch msg.String() {
		case "up", "k":
			if t.modelIndex > 0 {
				t.modelIndex--
			}
		case "down", "j":
			if t.modelIndex < len(models)-1 {
				t.modelIndex++
			}
		case "enter":
			if len(models) > 0 {
				t.flow = aiFlowConfirm
			}
		case "esc":
			t.cancelMode()
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
}

// startSwitch begins the switch (onlyModel=false) or model-change
// (onlyModel=true) wizard for the agent selected in the right pane.
func (t *aiTab) startSwitch(onlyModel bool) (Tab, tea.Cmd) {
	if len(t.rows) == 0 {
		return t, warnToast("没有可操作的 agent")
	}
	row := t.rows[clamp(t.agentIndex, 0, len(t.rows)-1)]
	if !row.Supported {
		return t, warnToast("agent " + row.AgentID + " 暂不支持切换")
	}
	var alias string
	if onlyModel {
		if row.Pointer == nil {
			return t, warnToast("请先按 s 为 " + row.AgentID + " 选择 provider")
		}
		alias = row.Pointer.Provider
	} else {
		if len(t.providers) == 0 {
			return t, warnToast("暂无 provider，按 n 新建")
		}
		alias = t.providers[clamp(t.providerIndex, 0, len(t.providers)-1)].Alias
	}
	entry := t.providerByAlias(alias)
	if entry == nil {
		return t, warnToast("provider " + alias + " 不存在")
	}
	if len(entry.Models) == 0 {
		return t, warnToast("provider " + alias + " 没有可用模型")
	}
	t.flowProvider = alias
	t.flowAgent = clamp(t.agentIndex, 0, len(t.rows)-1)
	t.flowOnlyModel = onlyModel
	t.modelIndex = modelIndexOf(entry, entry.DefaultModel)
	if onlyModel && row.Pointer.Model != "" {
		t.modelIndex = modelIndexOf(entry, row.Pointer.Model)
	}
	t.flow = aiFlowSelectModel
	return t, nil
}

func (t *aiTab) flowModels() []string {
	entry := t.providerByAlias(t.flowProvider)
	if entry == nil {
		return nil
	}
	return entry.Models
}

func (t *aiTab) switchCmd(onlyModel bool) tea.Cmd {
	models := t.flowModels()
	if len(models) == 0 || t.flowAgent < 0 || t.flowAgent >= len(t.rows) {
		return nil
	}
	agentID := t.rows[t.flowAgent].AgentID
	provider := t.flowProvider
	model := models[clamp(t.modelIndex, 0, len(models)-1)]
	sm := llm.NewSwitchManager(t.mgr.LLM, t.mgr.LLMPointer, t.mgr.LLMHome)
	return func() tea.Msg {
		out, err := sm.Switch(agentID, provider, model)
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
	allowHTTP := "否"
	if strings.HasPrefix(base.BaseURL, "http://") {
		allowHTTP = "是"
	}

	title := "新建 LLM Provider"
	fields := make([]formField, 0, 8)
	if create {
		fields = append(fields, formField{
			key: "alias", label: "别名", kind: formText, placeholder: "main",
		})
	} else {
		title = "编辑 Provider " + existing.Alias
	}
	fields = append(fields,
		formField{
			key: "base_url", label: "base_url", kind: formText, value: base.BaseURL,
			placeholder: "https://api.example.com/v1",
		},
		formField{
			key: "api_shape", label: "接入形态", kind: formEnum, value: base.APIShape,
			options: []string{
				storage.LLMAPIShapeOpenAIChat,
				storage.LLMAPIShapeOpenAIResponses,
				storage.LLMAPIShapeAnthropic,
			},
			optional: true,
		},
		formField{
			key: "catalog", label: "目录 provider", kind: formText, value: base.CatalogProvider,
			placeholder: "models.dev provider id（可选）",
		},
		formField{
			key: "models", label: "模型集", kind: formText, value: strings.Join(base.Models, ", "),
			placeholder: "m1, m2（逗号分隔）",
		},
		formField{
			key: "default_model", label: "默认模型", kind: formText, value: base.DefaultModel,
			placeholder: "m1（可选）",
		},
		formField{
			key: "credential", label: "凭据来源", kind: formRef, value: credentialValue,
			options: credentialOptions,
		},
		formField{
			key: "api_key", label: "自有凭据", kind: formSecret,
			placeholder: "仅「新建自有凭据」时填写",
		},
		formField{
			key: "allow_http", label: "允许 HTTP", kind: formEnum, value: allowHTTP,
			options: []string{"否", "是"},
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
			return reopen("alias", fmt.Errorf("别名不能为空"))
		}
		if err := storage.ValidateName(alias); err != nil {
			return reopen("alias", fmt.Errorf("非法别名"))
		}
		if t.providerByAlias(alias) != nil {
			return reopen("alias", fmt.Errorf("provider %s 已存在", alias))
		}
	} else {
		alias = existing.Alias
	}

	baseURL := strings.TrimSpace(values["base_url"])
	if baseURL == "" {
		return reopen("base_url", fmt.Errorf("base_url 不能为空"))
	}
	allowHTTP := strings.TrimSpace(values["allow_http"]) == "是"
	if err := storage.ValidateLLMProviderURL(baseURL, allowHTTP); err != nil {
		return reopen("base_url", err)
	}
	apiShape := strings.TrimSpace(values["api_shape"])
	if err := storage.ValidateLLMProviderAPIShape(apiShape); err != nil {
		return reopen("api_shape", err)
	}
	catalog := strings.TrimSpace(values["catalog"])
	models := parseModelList(values["models"])
	if catalog == "" && len(models) == 0 {
		return reopen("models", fmt.Errorf("模型集不能为空：填写模型或目录 provider"))
	}
	defaultModel := strings.TrimSpace(values["default_model"])
	if catalog == "" && defaultModel != "" && !containsString(models, defaultModel) {
		return reopen("default_model", fmt.Errorf("默认模型 %s 不在模型集中", defaultModel))
	}
	credential := strings.TrimSpace(values["credential"])
	apiKey := strings.TrimSpace(values["api_key"])
	switch {
	case credential == aiNewCredential && apiKey == "":
		return reopen("api_key", fmt.Errorf("新建自有凭据需要输入 API key"))
	case credential != aiNewCredential && apiKey != "":
		return reopen("api_key", fmt.Errorf("已选择既有条目时请清空自有凭据字段"))
	case credential == "" && create:
		return reopen("credential", fmt.Errorf("请选择凭据来源或新建自有凭据"))
	}

	mgr := t.mgr.LLM
	mgrs := t.mgr
	catalogPath := t.mgr.LLMCatalog

	if create {
		opts := llm.AddProviderOptions{
			Alias:           alias,
			BaseURL:         baseURL,
			AllowHTTP:       allowHTTP,
			CatalogPath:     catalogPath,
			CatalogProvider: catalog,
			Models:          models,
			DefaultModel:    defaultModel,
			APIShape:        apiShape,
		}
		if credential == aiNewCredential {
			opts.APIKey = apiKey
		} else {
			opts.KeyRef = credential
		}
		return func() tea.Msg {
			res, err := mgr.AddProvider(opts)
			if err != nil {
				recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "add 失败")
				return aiFormReopenMsg{form: f, submit: submit, values: values, field: providerErrorField(err), err: err}
			}
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "add")
			toast := fmt.Sprintf("已保存 provider %s（%d 个模型）", res.Entry.Alias, len(res.Entry.Models))
			if len(res.Warnings) > 0 {
				toast += "；" + truncateWidth(res.Warnings[0], 40)
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
	if !equalStrings(models, existing.Models) || catalog != existing.CatalogProvider {
		opts.Models = models
		opts.CatalogProvider = &catalog
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
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "edit 失败")
			return aiFormReopenMsg{form: f, submit: submit, values: values, field: providerErrorField(err), err: err}
		}
		recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "edit")
		toast := fmt.Sprintf("已更新 provider %s（%d 个模型）", res.Entry.Alias, len(res.Entry.Models))
		if len(res.Warnings) > 0 {
			toast += "；" + truncateWidth(res.Warnings[0], 40)
		}
		return aiProviderReloadMsg{toast: toast, providerAlias: res.Entry.Alias}
	}
}

func (t *aiTab) enterDeleteProvider() (Tab, tea.Cmd) {
	p := t.currentProvider()
	if p == nil {
		return t, warnToast("没有可删除的 provider")
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
			recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, false, "remove 失败")
			return errMsg{err: err}
		}
		recordAudit(mgrs, session.AuditOpLLMProvider, "provider:"+alias, true, "remove")
		toast := "已删除 provider " + alias
		if res.CredentialRemoved {
			toast += "（含自有凭据）"
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
			return warnToast("没有选中的 provider")
		}
		t.detail = newDetailOverlay("Provider "+p.Alias, t.providerDetailLines(p))
	} else {
		if len(t.rows) == 0 {
			return warnToast("没有选中的 agent")
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
		lines = append(lines, "  "+m)
	}
	lines = append(lines, "", "被指向的 agent:")
	used := false
	for _, r := range t.rows {
		if r.Pointer != nil && r.Pointer.Provider == p.Alias {
			lines = append(lines, "  "+r.AgentID+" · "+r.Pointer.Model)
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
	state := "未切换"
	switch {
	case !row.Supported:
		state = "不支持"
	case row.Pointer != nil:
		state = row.Pointer.Provider + " / " + row.Pointer.Model
	}
	lines := []string{
		"agent:       " + row.AgentID,
		"name:        " + row.AgentName,
		"supported:   " + fmt.Sprintf("%t", row.Supported),
		"pointer:     " + state,
		"config:      " + orDash(row.ConfigPath),
	}
	if !row.Supported {
		lines = append(lines, "senv 无法写回该 agent 的配置（无公开 schema）")
	}
	return lines
}

// --- lookups ---

func (t *aiTab) currentProvider() *storage.LLMProviderEntry {
	if t.providerIndex < 0 || t.providerIndex >= len(t.providers) {
		return nil
	}
	return t.providers[t.providerIndex]
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

// modelIndexOf returns the index of model within entry's model set, or 0.
func modelIndexOf(entry *storage.LLMProviderEntry, model string) int {
	if model != "" {
		for i, m := range entry.Models {
			if m == model {
				return i
			}
		}
	}
	return 0
}

// --- view ---

func (t *aiTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("AI") + "\n" + truncateRunes("⚠ "+t.loadErr, max(t.width, 1))
	}
	if t.detail != nil {
		return t.detail.View()
	}
	if len(t.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			paneTitleStyle.Render("AI"),
			emptyStateStyle.Render("暂无 LLM Provider 档案；执行 senv ai provider add 或按 n 新建后按 r 刷新"))
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

	providerLines := t.providerLines(max(leftW-4, 8))
	agentLines := t.agentLines(max(rightW-4, 8))

	left := windowedPane(fmt.Sprintf("Providers (%d)", len(t.providers)), providerLines, t.providerIndex, height, leftW)
	right := windowedPane("Agents · 当前指向", agentLines, t.agentIndex, height, rightW)

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
	lines := make([]string, 0, len(t.providers))
	for i, p := range t.providers {
		def := p.DefaultModel
		if def == "" {
			def = "-"
		}
		line := fmt.Sprintf("%s · 默认 %s · %d 模型", p.Alias, def, len(p.Models))
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
		state := "未切换"
		switch {
		case !r.Supported:
			state = "不支持"
		case r.Pointer != nil:
			state = r.Pointer.Provider + " / " + r.Pointer.Model
		}
		line := fmt.Sprintf("%s · %s", r.AgentID, state)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.agentIndex))
	}
	return lines
}

func (t *aiTab) renderModal() string {
	switch t.mode {
	case aiModeDeleteProvider:
		return modalBox("删除 provider "+t.pendingProvider+"？",
			"自有凭据会一并删除；外部引用保留。", "enter/y 确认 · esc/n 取消")
	}
	return ""
}

// renderFlow renders the model picker and the final confirmation. The full
// model list is windowed so a large model set cannot push the modal off-screen.
func (t *aiTab) renderFlow() string {
	models := t.flowModels()
	agent := "agent"
	if t.flowAgent >= 0 && t.flowAgent < len(t.rows) {
		agent = t.rows[t.flowAgent].AgentID
	}
	if t.flow == aiFlowConfirm {
		model := "-"
		if len(models) > 0 {
			model = models[clamp(t.modelIndex, 0, len(models)-1)]
		}
		action := "切换"
		if t.flowOnlyModel {
			action = "仅换模型"
		}
		return modalBox("确认"+action,
			fmt.Sprintf("%s → %s / %s", agent, t.flowProvider, model), "enter/y 确认 · esc/n 取消")
	}
	lines := make([]string, 0, len(models))
	for i, m := range models {
		label := truncateWidth(m, max(t.width-10, 12))
		lines = append(lines, cursorLine(label, i == clamp(t.modelIndex, 0, len(models)-1)))
	}
	title := "选择模型 — " + agent + " → " + t.flowProvider
	return modalBox(title, strings.Join(lines, "\n"), "↑↓/jk 选择 · enter 下一步 · esc 取消")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
