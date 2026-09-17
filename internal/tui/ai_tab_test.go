package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func newAITestTab(t *testing.T) (*aiTab, string, *fakeAuditWriter) {
	t.Helper()
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	pm := llm.NewProviderManager(store, "test-password")
	if _, err := pm.AddProvider(llm.AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-tui-secret", Models: []string{"m1", "m2"}, DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	home := t.TempDir()
	w := &fakeAuditWriter{}
	mgr := Managers{
		Env:  env.NewManager(store, "test-password"),
		Text: text.NewManager(store, "test-password"),
		LLM:  pm, LLMPointer: llm.DefaultPointerPath(home), LLMHome: home,
		LLMCatalog:  filepath.Join(base, "cache", "models-dev.json"),
		AuditWriter: w,
	}
	tab := newAITab(mgr)
	tab.SetSize(100, 24)
	return tab, home, w
}

func runAITabLoad(t *testing.T, tab *aiTab) {
	t.Helper()
	cmd := tab.load()
	msg := cmd()
	next, _ := tab.Update(msg)
	if next.(*aiTab) != tab {
		t.Fatal("Update returned a different tab pointer")
	}
	if tab.loadErr != "" {
		t.Fatalf("load error: %s", tab.loadErr)
	}
}

// submitAIForm fills a form's fields by key and presses enter, returning the
// settled tab (the form stays open when validation fails).
func submitAIForm(t *testing.T, tab *aiTab, values map[string]string) *aiTab {
	t.Helper()
	if tab.form == nil {
		t.Fatal("expected an open form")
	}
	for key, value := range values {
		tab.form.SetValue(key, value)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return flushTab(out, cmd).(*aiTab)
}

func TestAITabRegistration(t *testing.T) {
	if tabs := New(Managers{}); len(tabs.tabs) != 3 {
		t.Fatalf("base tab count = %d, want 3 (no AI without LLM)", len(tabs.tabs))
	}
	m := New(Managers{LLM: &llm.ProviderManager{}})
	last := m.tabs[len(m.tabs)-1]
	if last.Title() != "AI" {
		t.Fatalf("last tab = %q, want AI", last.Title())
	}
}

func TestAITabBrowseNoSecretLeak(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	view := tab.View()
	for _, want := range []string{"main", "not switched", "current target"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, absent := range []string{"unsupported", "zcode", "cursor"} {
		if strings.Contains(view, absent) {
			t.Fatalf("view must not contain %q:\n%s", absent, view)
		}
	}
	if strings.Contains(view, "sk-tui-secret") {
		t.Fatal("view leaked credential plaintext")
	}
	if !strings.Contains(view, "▸ main") {
		t.Fatalf("selected provider missing marker:\n%s", view)
	}
	// Provider details (base_url, credential ref) live in the `enter` overlay so
	// long values can never wrap the browse panes.
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail := tab.View()
	for _, want := range []string{"https://api.example.com/v1", "text:llm-keys/main", "api_shape"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail overlay missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "sk-tui-secret") {
		t.Fatal("detail overlay leaked credential plaintext")
	}
}

func TestAITabFocusSwitchesPanes(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)

	tab.focusLeft = true
	tab.providerIndex = 0
	tab.agentIndex = 0
	out, _ := tab.Update(runeKey("l"))
	tab = out.(*aiTab)
	if tab.focusLeft {
		t.Fatal("l should move focus to the agent pane")
	}
	out, _ = tab.Update(runeKey("j"))
	tab = out.(*aiTab)
	if tab.agentIndex != 1 {
		t.Fatalf("agentIndex = %d, want 1 after j", tab.agentIndex)
	}
	if tab.providerIndex != 0 {
		t.Fatalf("provider selection changed while the agent pane was focused: %d", tab.providerIndex)
	}
	out, _ = tab.Update(runeKey("h"))
	tab = out.(*aiTab)
	if !tab.focusLeft {
		t.Fatal("h should move focus back to the provider pane")
	}
}

// driveAISwitch walks the model picker: focus the agent pane, pick the agent,
// press the trigger key, move to the wanted model and confirm.
func driveAISwitch(t *testing.T, tab *aiTab, agentSteps, modelSteps int, trigger string) tea.Msg {
	t.Helper()
	tab.focusLeft = false
	for i := 0; i < agentSteps; i++ {
		tab.Update(runeKey("j"))
	}
	if _, cmd := tab.Update(runeKey(trigger)); cmd != nil {
		t.Fatal("flow start unexpectedly returned a command")
	}
	// s 先进入多选步骤（默认全选），m 直接从默认模型步骤开始。
	if trigger == "s" {
		if tab.flow != aiFlowSelectModel {
			t.Fatalf("flow = %v, want selectModel", tab.flow)
		}
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if tab.flow != aiFlowSelectDefault {
		t.Fatalf("flow = %v, want selectDefault", tab.flow)
	}
	for i := 0; i < modelSteps; i++ {
		tab.Update(runeKey("j"))
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tab.flow != aiFlowConfirm {
		t.Fatalf("flow = %v, want confirm", tab.flow)
	}
	_, cmd := tab.Update(runeKey("y"))
	if cmd == nil {
		t.Fatal("confirm did not return switch command")
	}
	return cmd()
}

// collectAIToasts applies a result message and returns the toasts it produced.
func collectAIToasts(t *testing.T, tab *aiTab, msg tea.Msg) []string {
	t.Helper()
	next, cmd := tab.Update(msg)
	tab = next.(*aiTab)
	var notices []string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			notices = append(notices, tm.text)
			continue
		}
		next, _ = tab.Update(m)
		tab = next.(*aiTab)
	}
	return notices
}

func TestAITabSwitchSuccess(t *testing.T) {
	tab, home, _ := newAITestTab(t)
	runAITabLoad(t, tab)

	msg := driveAISwitch(t, tab, 0, 0, "s") // claude-code + m1
	result, ok := msg.(aiSwitchResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("switch msg = %#v", msg)
	}
	if notices := collectAIToasts(t, tab, result); !strings.Contains(strings.Join(notices, ";"), "Claude Code → main") {
		t.Fatalf("notice = %q", notices)
	}
	if !strings.Contains(tab.View(), "claude-code") || !strings.Contains(tab.View(), "main / m1") {
		t.Fatalf("pointer not refreshed:\n%s", tab.View())
	}

	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(raw), "sk-tui-secret") {
		t.Fatalf("settings.json = %s, %v", raw, err)
	}
}

func TestAITabSwitchFailureBanner(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	// 指针路径挂在普通文件下使保存必败。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab.mgr.LLMPointer = filepath.Join(blocker, "pointers.json")

	msg := driveAISwitch(t, tab, 0, 0, "s")
	result := msg.(aiSwitchResultMsg)
	if result.err == nil {
		t.Fatal("switch unexpectedly succeeded")
	}
	_, cmd := tab.Update(result)
	if cmd == nil {
		t.Fatal("failure did not produce error banner command")
	}
	if _, ok := cmd().(errMsg); !ok {
		t.Fatalf("banner msg = %#v", cmd())
	}
}

func TestAITabSwitchCodexGuidance(t *testing.T) {
	tab, home, _ := newAITestTab(t)
	runAITabLoad(t, tab)

	msg := driveAISwitch(t, tab, 1, 1, "s") // codex + m2
	result := msg.(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("switch error: %v", result.err)
	}
	if notices := collectAIToasts(t, tab, result); !strings.Contains(strings.Join(notices, ";"), "SENV_MAIN_API_KEY") {
		t.Fatalf("codex guidance missing: %q", notices)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-tui-secret") {
		t.Fatal("codex config leaked credential")
	}
}

func TestAITabModelOnlyChange(t *testing.T) {
	tab, home, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	_ = home

	// 先切换到 main/m1。
	first := driveAISwitch(t, tab, 0, 0, "s").(aiSwitchResultMsg)
	if first.err != nil {
		t.Fatalf("initial switch: %#v", first)
	}
	collectAIToasts(t, tab, first)
	if !strings.Contains(tab.View(), "main / m1") {
		t.Fatalf("pointer not set:\n%s", tab.View())
	}

	// M：仅换模型到 m2（grill D7：model-only 键 m→M）。
	msg := driveAISwitch(t, tab, 0, 1, "M")
	result := msg.(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("model-only change error: %v", result.err)
	}
	if !result.onlyModel || result.out.Provider != "main" || result.out.DefaultModel != "m2" {
		t.Fatalf("model-only result = %+v", result.out)
	}
	if notices := collectAIToasts(t, tab, result); !strings.Contains(strings.Join(notices, ";"), "default model only") {
		t.Fatalf("notice = %q", notices)
	}
	if !strings.Contains(tab.View(), "main / m2") {
		t.Fatalf("pointer not refreshed:\n%s", tab.View())
	}
}

func TestAITabModelOnlyRequiresPointer(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.agentIndex = 0
	_, cmd := tab.Update(runeKey("M"))
	if tab.flow != aiFlowNone {
		t.Fatal("m without a pointer must not start a flow")
	}
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v", msgs)
	}
	tm, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(tm.text, "press s first") {
		t.Fatalf("expected guidance toast, got %#v", msgs)
	}
}

func TestAITabFlowEscape(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.Update(runeKey("s"))
	if !tab.InputMode() {
		t.Fatal("flow should enable InputMode")
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tab.flow != aiFlowNone || tab.InputMode() {
		t.Fatalf("esc did not cancel flow: %v", tab.flow)
	}
}

func TestAITabCreateProviderViaForm(t *testing.T) {
	tab, _, w := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = true

	out, _ := tab.Update(runeKey("n"))
	tab = out.(*aiTab)
	if tab.form == nil {
		t.Fatal("n should open the provider create form")
	}
	if !tab.InputMode() {
		t.Error("open form must report InputMode")
	}
	if tab.form.fieldIndex("alias") == -1 {
		t.Fatal("create form must expose the alias field")
	}
	tab = submitAIForm(t, tab, map[string]string{
		"alias":                   "second",
		"base_url":                "https://second.example.com",
		"api_shape":               string(llm.APIShapeAnthropic),
		"models":                  "s1, s2",
		"model_contexts":          "s1=128000, s2=200000",
		"model_outputs":           "s1=32000",
		"model_reasoning":         "s1=low;high",
		"model_default_reasoning": "s1=high",
		"model_modalities":        "s1=text,image",
		"default_model":           "s2",
		"credential":              aiNewCredential,
		"api_key":                 "sk-second-secret",
	})
	if tab.form != nil {
		t.Fatalf("form should close after a successful create: %#v", tab.form.errs)
	}
	p := tab.providerByAlias("second")
	if p == nil {
		t.Fatalf("provider not created: %#v", tab.providers)
	}
	if p.APIShape != string(llm.APIShapeAnthropic) || p.DefaultModel != "s2" || len(p.Models) != 2 {
		t.Fatalf("unexpected provider: %+v", p)
	}
	if got := p.ModelInfo["s1"]; got.OutputLimit != 32000 || strings.Join(got.ReasoningEfforts, ";") != "low;high" {
		t.Fatalf("s1 model info = %+v, want output/reasoning from form", got)
	}
	if got := p.ModelInfo["s1"]; got.DefaultReasoning != "high" || strings.Join(got.InputModalities, ",") != "text,image" {
		t.Fatalf("s1 default/modalities = %+v", got)
	}
	if p.BaseURL != "https://second.example.com/v1" {
		t.Fatalf("BaseURL = %q", p.BaseURL)
	}
	if p.CredentialRef != llm.OwnedCredentialRef("second") {
		t.Fatalf("CredentialRef = %q", p.CredentialRef)
	}
	if view := tab.View(); strings.Contains(view, "sk-second-secret") {
		t.Fatalf("view leaked the new credential:\n%s", view)
	}
	var sawAdd bool
	for _, c := range w.calls {
		if c.target == "provider:second" && c.detail == "add" && c.success {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("create not audited: %#v", w.calls)
	}
}

func TestAITabCreateFormRequiresCredential(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	out, _ := tab.Update(runeKey("n"))
	tab = out.(*aiTab)
	tab = submitAIForm(t, tab, map[string]string{
		"alias": "second", "base_url": "https://second.example.com",
		"models": "s1", "credential": aiNewCredential,
	})
	if tab.form == nil {
		t.Fatal("missing own credential must keep the form open")
	}
	index := tab.form.fieldIndex("api_key")
	if index < 0 || !strings.Contains(tab.form.errs[index], "API key") {
		t.Fatalf("inline error missing: %#v", tab.form.errs)
	}
	if tab.providerByAlias("second") != nil {
		t.Fatal("invalid form wrote a provider")
	}
}

func TestAITabCreateFormRequiresDefaultReasoning(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	out, _ := tab.Update(runeKey("n"))
	tab = out.(*aiTab)
	tab = submitAIForm(t, tab, map[string]string{
		"alias": "second", "base_url": "https://second.example.com",
		"models": "s1", "model_contexts": "s1=128000",
		"model_reasoning": "s1=low;high",
		"credential":      aiNewCredential, "api_key": "sk-second",
	})
	if tab.form == nil {
		t.Fatal("missing default reasoning must keep the form open")
	}
	index := tab.form.fieldIndex("model_default_reasoning")
	if index < 0 || !strings.Contains(strings.ToLower(tab.form.errs[index]), "default reasoning") {
		t.Fatalf("inline error missing: %#v", tab.form.errs)
	}
	if tab.providerByAlias("second") != nil {
		t.Fatal("invalid form wrote a provider")
	}
}

func TestAITabDetailShowsDefaultReasoningAndModalities(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = true
	out, _ := tab.Update(runeKey("n"))
	tab = out.(*aiTab)
	tab = submitAIForm(t, tab, map[string]string{
		"alias": "vision", "base_url": "https://vision.example.com",
		"models": "s1", "model_contexts": "s1=128000",
		"model_reasoning":         "s1=low;high",
		"model_default_reasoning": "s1=high",
		"model_modalities":        "s1=text,image",
		"credential":              aiNewCredential, "api_key": "sk-vision",
	})
	if tab.form != nil {
		t.Fatalf("form should close: %#v", tab.form.errs)
	}
	for i, p := range tab.providers {
		if p.Alias == "vision" {
			tab.providerIndex = i
			break
		}
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail := tab.View()
	for _, want := range []string{"default_reasoning=high", "modalities=text,image"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}
}

func TestAITabEditProviderFormKeepsAliasAndCredential(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = true
	tab.providerIndex = 0

	out, _ := tab.Update(runeKey("e"))
	tab = out.(*aiTab)
	if tab.form == nil {
		t.Fatal("e should open the provider edit form")
	}
	if tab.form.fieldIndex("alias") != -1 {
		t.Fatal("edit form must not expose the alias field")
	}
	values := tab.form.Values()
	if values["base_url"] != "https://api.example.com/v1" {
		t.Fatalf("base_url prefill = %q", values["base_url"])
	}
	if values["models"] != "m1, m2" || values["default_model"] != "m1" {
		t.Fatalf("model prefill = %#v", values)
	}

	tab = submitAIForm(t, tab, map[string]string{
		"api_shape":     string(llm.APIShapeOpenAIResponses),
		"default_model": "m2",
	})
	if tab.form != nil {
		t.Fatalf("form should close after a successful edit: %#v", tab.form.errs)
	}
	p := tab.providerByAlias("main")
	if p == nil || p.Alias != "main" {
		t.Fatalf("provider lost: %#v", p)
	}
	if p.APIShape != string(llm.APIShapeOpenAIResponses) || p.DefaultModel != "m2" {
		t.Fatalf("edit not applied: %+v", p)
	}
	// 未改凭据字段时保留原自有凭据引用。
	if p.CredentialRef != llm.OwnedCredentialRef("main") {
		t.Fatalf("credential ref changed: %q", p.CredentialRef)
	}
}

func TestAITabEditFormReopensOnBackendError(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = true
	tab.providerIndex = 0
	out, _ := tab.Update(runeKey("e"))
	tab = out.(*aiTab)

	// 非法 api_shape 绕过枚举（模拟粘贴/旧数据）时后端拒绝，表单重开并内联报错。
	tab = submitAIForm(t, tab, map[string]string{"api_shape": "openai"})
	if tab.form == nil {
		t.Fatal("backend rejection must reopen the form")
	}
	index := tab.form.fieldIndex("api_shape")
	if index < 0 || !strings.Contains(tab.form.errs[index], "api_shape") {
		t.Fatalf("inline error missing: %#v", tab.form.errs)
	}
	if tab.form.Values()["api_shape"] != "openai" {
		t.Fatalf("form input lost on reopen: %#v", tab.form.Values())
	}
	p := tab.providerByAlias("main")
	if p.APIShape != "" {
		t.Fatalf("invalid edit wrote api_shape %q", p.APIShape)
	}
}

func TestAITabCredentialPickerListsExistingEntries(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	if err := tab.mgr.Env.AddGroup("llm", "test"); err != nil {
		t.Fatal(err)
	}
	if err := tab.mgr.Env.Set("llm", "KEY", "env-secret"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	if err := tab.mgr.Text.AddGroup("notes", "test"); err != nil {
		t.Fatal(err)
	}
	if err := tab.mgr.Text.Set("notes", "TOKEN", "text-secret"); err != nil {
		t.Fatalf("text set: %v", err)
	}
	runAITabLoad(t, tab)

	out, _ := tab.Update(runeKey("n"))
	tab = out.(*aiTab)
	index := tab.form.fieldIndex("credential")
	if index < 0 {
		t.Fatal("credential field missing")
	}
	options := tab.form.fields[index].enumOptions()
	for _, want := range []string{aiNewCredential, "env:llm/KEY", "text:notes/TOKEN"} {
		if !containsString(options, want) {
			t.Fatalf("credential options missing %q: %#v", want, options)
		}
	}
	// 聚焦凭据字段后视图渲染候选列表，但不含任何值明文。
	tab.form.index = index
	tab.form.syncInput()
	view := tab.View()
	if !strings.Contains(view, "env:llm/KEY") {
		t.Fatalf("candidate list not rendered:\n%s", view)
	}
	for _, secret := range []string{"env-secret", "text-secret"} {
		if strings.Contains(view, secret) {
			t.Fatalf("credential picker leaked %q:\n%s", secret, view)
		}
	}
}

func TestAITabDeleteProviderConfirmAndAudit(t *testing.T) {
	tab, _, w := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = true
	tab.providerIndex = 0

	out, _ := tab.Update(runeKey("d"))
	tab = out.(*aiTab)
	if tab.mode != aiModeDeleteProvider {
		t.Fatalf("d should stage a delete, mode=%v", tab.mode)
	}
	if view := tab.View(); !strings.Contains(view, "delete provider main") {
		t.Fatalf("confirm modal missing:\n%s", view)
	}
	// esc 取消不删除。
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*aiTab)
	if tab.providerByAlias("main") == nil {
		t.Fatal("esc should not delete the provider")
	}

	out, _ = tab.Update(runeKey("d"))
	tab = out.(*aiTab)
	out, cmd := tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*aiTab)
	if tab.providerByAlias("main") != nil {
		t.Fatalf("provider still present: %#v", tab.providers)
	}
	var sawRemove bool
	for _, c := range w.calls {
		if c.target == "provider:main" && c.detail == "remove" && c.success {
			sawRemove = true
		}
	}
	if !sawRemove {
		t.Fatalf("delete not audited: %#v", w.calls)
	}
}

func TestAITabRenameProvider(t *testing.T) {
	tab, _, w := newAITestTab(t)
	runAITabLoad(t, tab)
	if _, err := tab.mgr.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "taken", BaseURL: "https://api.example.com",
		APIKey: "sk-taken", Models: []string{"m1"}, DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("seed taken: %v", err)
	}
	runAITabLoad(t, tab)
	tab.focusLeft = true
	tab.providerIndex = 0
	for i, p := range tab.visibleProviders() {
		if p.Alias == "main" {
			tab.providerIndex = i
			break
		}
	}

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*aiTab)
	if tab.form == nil || !strings.Contains(tab.form.title, "rename provider main") {
		t.Fatalf("r should open rename form, form=%v", tab.form)
	}

	// 冲突：表单内联报错，档案不变。
	tab = submitAIForm(t, tab, map[string]string{"alias": "taken"})
	if tab.form == nil {
		t.Fatal("conflict should keep the form open")
	}
	if errs := tab.form.errs; len(errs) == 0 || !strings.Contains(errs[0], "already exists") {
		t.Fatalf("inline error missing: %v", errs)
	}
	if tab.providerByAlias("main") == nil || tab.providerByAlias("taken") == nil {
		t.Fatalf("conflict mutated providers: %#v", tab.providers)
	}

	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*aiTab)
	out, _ = tab.Update(runeKey("r"))
	tab = out.(*aiTab)
	tab = submitAIForm(t, tab, map[string]string{"alias": "main-prod"})
	if tab.form != nil {
		t.Fatalf("successful rename should close form: %v", tab.form.errs)
	}
	if tab.providerByAlias("main") != nil || tab.providerByAlias("main-prod") == nil {
		t.Fatalf("providers after rename: %#v", tab.providers)
	}
	entry, err := tab.mgr.LLM.GetProvider("main-prod")
	if err != nil || entry.CredentialRef != "text:llm-keys/main-prod" {
		t.Fatalf("renamed entry = %+v, %v", entry, err)
	}
	var sawRename bool
	for _, c := range w.calls {
		if c.target == "provider:main-prod" && c.detail == "rename main" && c.success {
			sawRename = true
		}
	}
	if !sawRename {
		t.Fatalf("rename not audited: %#v", w.calls)
	}
}

func TestAITabExcludesUnsupportedAgents(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	for _, row := range tab.rows {
		if row.AgentID == "cursor" || row.AgentID == "zcode" {
			t.Fatalf("unsupported agent %s must not be listed: %+v", row.AgentID, tab.rows)
		}
	}
}

func TestAITabEmptyState(t *testing.T) {
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	tab := newAITab(Managers{LLM: llm.NewProviderManager(store, "test-password"), LLMHome: t.TempDir()})
	tab.SetSize(80, 20)
	runAITabLoad(t, tab)
	if view := tab.View(); !strings.Contains(view, "senv ai provider add") {
		t.Fatalf("empty state hint missing:\n%s", view)
	}
}

func TestAITabLoadError(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	next, cmd := tab.Update(aiLoadedMsg{err: errors.New("boom")})
	tab = next.(*aiTab)
	if cmd != nil || tab.loadErr == "" {
		t.Fatalf("loadErr = %q, cmd = %v", tab.loadErr, cmd)
	}
	if !strings.Contains(tab.View(), "boom") {
		t.Fatalf("view missing error:\n%s", tab.View())
	}
}

// TestAITabLongValuesDoNotWrap 覆盖长模型集/长 base_url 不折行、不撑高。
func TestAITabLongValuesDoNotWrap(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	models := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		models = append(models, fmt.Sprintf("model-%02d-%s", i, strings.Repeat("x", 40)))
	}
	if _, err := tab.mgr.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "wide", BaseURL: "https://api.example.com/a/really/long/base/path",
		APIKey: "k", Models: models,
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	runAITabLoad(t, tab)
	tab.SetSize(80, 20)
	view := tab.View()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("line %d width %d > 80: %q", i, w, line)
		}
	}
}

// rowIndexOf 返回右栏里指定 agent 的行下标。
func rowIndexOf(t *testing.T, tab *aiTab, agentID string) int {
	t.Helper()
	for i, row := range tab.rows {
		if row.AgentID == agentID {
			return i
		}
	}
	t.Fatalf("agent %s not found in rows", agentID)
	return -1
}

// TestAITabSwitchSelectionSubset 覆盖任务 1.1/1.3：进入多选默认全选，取消勾选
// 后只把子集写入 agent 配置，成功提示含条数。
func TestAITabSwitchSelectionSubset(t *testing.T) {
	tab, home, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.agentIndex = rowIndexOf(t, tab, "claude-code")

	tab.Update(runeKey("s"))
	if tab.flow != aiFlowSelectModel {
		t.Fatalf("flow = %v, want selectModel", tab.flow)
	}
	if len(tab.flowSelectedModels()) != 2 {
		t.Fatalf("multi-select must default to the full provider set, got %v", tab.flowSelectedModels())
	}
	tab.Update(runeKey("j"))                   // 游标到 m2
	tab.Update(tea.KeyMsg{Type: tea.KeySpace}) // 取消勾选
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // 进入默认模型步骤
	if tab.flow != aiFlowSelectDefault {
		t.Fatalf("flow = %v, want selectDefault", tab.flow)
	}
	if got := strings.Join(tab.flowSelectedModels(), ","); got != "m1" {
		t.Fatalf("selected = %q, want m1 only", got)
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := tab.Update(runeKey("y"))
	if cmd == nil {
		t.Fatal("confirm did not return a switch command")
	}
	result := cmd().(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("switch error: %v", result.err)
	}
	if got := strings.Join(result.out.Models, ","); got != "m1" {
		t.Fatalf("switched models = %q, want the selected subset", got)
	}
	if notices := collectAIToasts(t, tab, result); !strings.Contains(strings.Join(notices, ";"), "1 models") {
		t.Fatalf("notice should include the model count: %q", notices)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"m1"`) || strings.Contains(string(raw), `"m2"`) {
		t.Fatalf("settings.json should only carry the subset:\n%s", raw)
	}
}

// TestAITabSwitchEmptySelectionBlocked 覆盖任务 1.2：空模型集在提交前拦截，
// 不进入确认步骤，也不调用 SwitchManager。
func TestAITabSwitchEmptySelectionBlocked(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.agentIndex = rowIndexOf(t, tab, "claude-code")

	tab.Update(runeKey("s"))
	for i := 0; i < len(tab.flowCandidates); i++ {
		tab.Update(tea.KeyMsg{Type: tea.KeySpace}) // 逐个取消勾选
		if i < len(tab.flowCandidates)-1 {
			tab.Update(runeKey("j"))
		}
	}
	if len(tab.flowSelectedModels()) != 0 {
		t.Fatalf("expected an empty selection, got %v", tab.flowSelectedModels())
	}
	_, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tab.flow != aiFlowSelectModel {
		t.Fatalf("empty selection advanced the flow to %v", tab.flow)
	}
	msgs := runCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v, want a single warning toast", msgs)
	}
	tm, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(tm.text, "cannot be empty") {
		t.Fatalf("expected an empty-set toast, got %#v", msgs)
	}
	if _, err := os.Stat(tab.mgr.LLMPointer); !os.IsNotExist(err) {
		t.Fatalf("pointer written despite the blocked switch: %v", err)
	}
}

// TestAITabDefaultCursorFallsToFirstSelected 覆盖任务 2.2 的一半：档案默认模型
// 不在勾选集合内时，默认模型游标落集合首项。
func TestAITabDefaultCursorFallsToFirstSelected(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.agentIndex = rowIndexOf(t, tab, "claude-code")

	tab.Update(runeKey("s"))
	tab.Update(tea.KeyMsg{Type: tea.KeySpace}) // 取消勾选档案默认模型 m1
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	models := tab.flowSelectedModels()
	if strings.Join(models, ",") != "m2" {
		t.Fatalf("selected = %v", models)
	}
	if got := models[tab.flowCursor]; got != "m2" {
		t.Fatalf("default cursor = %q, want the first selected model", got)
	}
}

// TestAITabModelOnlyCandidatesLimitedToPointer 覆盖任务 2.1/2.2：m 的候选只能是
// 指针里的 Agent 模型集，提交时模型集不变。
func TestAITabModelOnlyCandidatesLimitedToPointer(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	// 指针记录的模型集比档案小（档案有 m1、m2），m 必须只用指针里的集合。
	pf := &llm.PointerFile{Version: 1, Agents: map[string]llm.AgentPointer{
		"claude-code": {
			Provider: "main", Models: []string{"m2"}, DefaultModel: "m2",
			SwitchedAt: time.Now().Format(time.RFC3339),
		},
	}}
	if err := llm.SavePointers(tab.mgr.LLMPointer, pf); err != nil {
		t.Fatalf("SavePointers: %v", err)
	}
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.agentIndex = rowIndexOf(t, tab, "claude-code")

	tab.Update(runeKey("M"))
	if tab.flow != aiFlowSelectDefault {
		t.Fatalf("flow = %v, want selectDefault directly", tab.flow)
	}
	if got := strings.Join(tab.flowCandidates, ","); got != "m2" {
		t.Fatalf("candidates = %q, want only the pointer's Agent model set", got)
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := tab.Update(runeKey("y"))
	if cmd == nil {
		t.Fatal("confirm did not return a switch command")
	}
	result := cmd().(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("model-only change error: %v", result.err)
	}
	if !result.onlyModel || strings.Join(result.out.Models, ",") != "m2" || result.out.DefaultModel != "m2" {
		t.Fatalf("model-only result = %+v, want the pointer set unchanged", result.out)
	}
}

// TestAITabAgentRowShowsModelCountAndDrift 覆盖任务 3.1：agent 行与 status 口径
// 一致（默认模型 + 条数），档案缩集时附漂移标记，且不渲染凭据。
func TestAITabAgentRowShowsModelCountAndDrift(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	result := driveAISwitch(t, tab, 0, 0, "s").(aiSwitchResultMsg)
	if result.err != nil {
		t.Fatalf("switch error: %v", result.err)
	}
	collectAIToasts(t, tab, result)
	view := tab.View()
	if !strings.Contains(view, "main / m1 (2 models)") {
		t.Fatalf("agent row missing the model count:\n%s", view)
	}
	if strings.Contains(view, "sk-tui-secret") {
		t.Fatal("credential rendered into the AI tab")
	}

	if _, err := tab.mgr.LLM.EditProvider(llm.EditProviderOptions{
		Alias: "main", Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	runAITabLoad(t, tab)
	if view := tab.View(); !strings.Contains(view, "main / m1 (2 models) ⚠") {
		t.Fatalf("drift marker missing:\n%s", view)
	}
}

// TestAITabSwitchHelpDocumentsMultiSelect 覆盖 keymap 注册表的流内键位。
func TestAITabSwitchHelpDocumentsMultiSelect(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.focusLeft = false
	tab.Update(runeKey("s"))
	if !bindingsContain(tab.Bindings(), "space", "toggle") {
		t.Fatalf("multi-select bindings missing space: %v", tab.Bindings())
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !bindingsContain(tab.Bindings(), "enter", "next") {
		t.Fatalf("default-model bindings missing enter: %v", tab.Bindings())
	}
}

// bindingsContain 报告键位列表里是否存在指定键与说明的条目。
func bindingsContain(bs []KeyAction, key, descPart string) bool {
	for _, b := range bs {
		for _, k := range b.Keys {
			if k == key && strings.Contains(b.Desc, descPart) {
				return true
			}
		}
	}
	return false
}

// TestAITabReloadKeepsDataVisibleAndReloads 验证 stale-while-revalidate：
// Reload 保持旧数据可见，经 aiLoadedMsg 回灌后静默替换。
func TestAITabReloadKeepsDataVisibleAndReloads(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	if !tab.loaded {
		t.Fatal("ai tab should be loaded after load")
	}
	cmd := tab.Reload()
	if !tab.loaded {
		t.Fatal("Reload must keep the loaded flag (stale data stays visible)")
	}
	if cmd == nil {
		t.Fatal("expected a load command from Reload")
	}
	runAITabLoad(t, tab)
	if !tab.loaded {
		t.Fatal("ai tab should be loaded after the reload lands")
	}
	if len(tab.providers) != 1 {
		t.Fatalf("providers = %d after reload, want 1", len(tab.providers))
	}
}

func TestAITabEditProviderClearsMetadataFields(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	// 先给档案补上 context window 与全套元数据，编辑入口才有可清空的值。
	if _, err := tab.mgr.LLM.EditProvider(llm.EditProviderOptions{
		Alias:                 "main",
		ModelContexts:         map[string]int{"m1": 200_000, "m2": 100_000},
		ModelOutputs:          map[string]int{"m1": 32_000},
		ModelReasoning:        map[string][]string{"m1": {"low", "high"}},
		ModelDefaultReasoning: map[string]string{"m1": "high"},
		ModelModalities:       map[string][]string{"m1": {"text", "image"}},
		RequireModelMetadata:  true,
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	runAITabLoad(t, tab)
	tab.focusLeft = true
	tab.providerIndex = 0

	out, _ := tab.Update(runeKey("e"))
	tab = out.(*aiTab)
	values := tab.form.Values()
	if values["model_outputs"] == "" || values["model_default_reasoning"] == "" || values["model_modalities"] == "" {
		t.Fatalf("metadata prefill missing: %#v", values)
	}

	// 清空输出上限、默认推理档与输入模态后提交。
	tab = submitAIForm(t, tab, map[string]string{
		"model_outputs":           "",
		"model_default_reasoning": "",
		"model_modalities":        "",
	})
	if tab.form != nil {
		t.Fatalf("form should close after clearing metadata: %#v", tab.form.errs)
	}
	p := tab.providerByAlias("main")
	if p == nil {
		t.Fatal("provider lost")
	}
	info := p.ModelInfo["m1"]
	if info.OutputLimit != 0 || info.DefaultReasoning != "" || len(info.InputModalities) != 0 {
		t.Fatalf("metadata not cleared: %+v", info)
	}
	if strings.Join(info.ReasoningEfforts, ",") != "low,high" {
		t.Fatalf("reasoning efforts = %v, want preserved", info.ReasoningEfforts)
	}
	if got := info.ContextWindow; got != 200_000 {
		t.Fatalf("context window = %d, want preserved", got)
	}

	// 重开编辑表单：清空过的字段不再回填旧值，保留字段照常预填。
	out, _ = tab.Update(runeKey("e"))
	tab = out.(*aiTab)
	values = tab.form.Values()
	if values["model_outputs"] != "" || values["model_default_reasoning"] != "" || values["model_modalities"] != "" {
		t.Fatalf("reopen shows stale metadata: %#v", values)
	}
	if !strings.Contains(values["model_reasoning"], "low") {
		t.Fatalf("reasoning prefill lost: %#v", values)
	}
}

// TestAITabLoadingStateBeforeLoad 校验加载态范式：装载完成前渲染常驻双栏几何
// 并内嵌加载提示，不得误显带操作指引的空态文案（tui-tab-consistency-render）。
func TestAITabLoadingStateBeforeLoad(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	if tab.loaded {
		t.Fatal("fresh tab should not be loaded")
	}
	out := tab.View()
	if !strings.Contains(out, "loading providers…") {
		t.Fatalf("loading hint missing before load: %q", clipRunesT(out, 120))
	}
	if strings.Contains(out, "no LLM provider profiles yet") {
		t.Fatal("empty-state guidance shown while loading")
	}
	if w, h := lipgloss.Width(out), lipgloss.Height(out); w != 100 || h != 26 {
		t.Fatalf("loading view size = %dx%d, want 100x26 (persistent two-pane geometry)", w, h)
	}

	runAITabLoad(t, tab)
	out = tab.View()
	if strings.Contains(out, "loading providers…") {
		t.Fatal("loading hint still shown after load")
	}
	if !strings.Contains(out, "Providers (1)") {
		t.Fatalf("provider list missing after load: %q", clipRunesT(out, 120))
	}
}
