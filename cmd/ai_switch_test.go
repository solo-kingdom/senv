package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

// setAISwitchFlags 模拟一次命令行解析结果：直接设置 flag 变量与 Changed
// 状态，测试结束还原（cmd 测试沿用「直接调用 RunE + 预置 flag」的既有风格）。
// models 为 nil 表示未传 --models；非 nil（含空切片）表示显式传入。
func setAISwitchFlags(t *testing.T, models []string, defaultModel string) {
	t.Helper()
	lookup := aiSwitchCmd.Flags().Lookup("models")
	oldModels, oldChanged, oldDefault := aiSwitchModels, lookup.Changed, aiSwitchDefaultModel
	aiSwitchModels, aiSwitchDefaultModel = models, defaultModel
	lookup.Changed = models != nil
	t.Cleanup(func() {
		aiSwitchModels, lookup.Changed, aiSwitchDefaultModel = oldModels, oldChanged, oldDefault
	})
}

// setAIRemovedModelFlag 模拟用户传入了已移除的 --model。
func setAIRemovedModelFlag(t *testing.T) {
	t.Helper()
	lookup := aiSwitchCmd.Flags().Lookup("model")
	old := lookup.Changed
	lookup.Changed = true
	t.Cleanup(func() { lookup.Changed = old })
}

// runAISwitchCmd 分别捕获 stdout/stderr（codex 的凭据指引走 stderr）。
func runAISwitchCmd(t *testing.T, cmd *cobra.Command, args []string) (string, string, error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	err := cmd.RunE(cmd, args)
	return outBuf.String(), errBuf.String(), err
}

func addAIProviderForSwitchTest(t *testing.T, alias string) {
	t.Helper()
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-secret-value")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddCatalog = "p1"
		providerAddDefault = "m1"
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{alias}); err != nil {
		t.Fatalf("add: %v", err)
	}
}

func TestAISwitchClaudeCodeEndToEnd(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(out, "Claude Code → main") || !strings.Contains(out, "默认模型 m1") ||
		!strings.Contains(out, "共 2 个模型") {
		t.Fatalf("switch output = %q", out)
	}

	// agent 配置已写入（凭据明文 + 模型）。
	settingsRaw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsRaw, &settings); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	if settings["model"] != "m1" {
		t.Fatalf("settings model = %v", settings["model"])
	}
	picker := settings["modelPicker"].(map[string]any)
	if picker["replaceBuiltInOptions"] != true {
		t.Fatalf("modelPicker = %v", picker)
	}
	options := picker["options"].([]any)
	if len(options) != 2 || options[0].(map[string]any)["model"] != "m1" || options[1].(map[string]any)["model"] != "m2" {
		t.Fatalf("modelPicker options = %v", options)
	}
	env := settings["env"].(map[string]any)
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-secret-value" {
		t.Fatalf("env token = %v", env["ANTHROPIC_AUTH_TOKEN"])
	}

	// 指针落盘且不含凭据。
	pointerRaw, err := os.ReadFile(agentPointerPath())
	if err != nil {
		t.Fatalf("read pointer file: %v", err)
	}
	if !strings.Contains(string(pointerRaw), `"provider": "main"`) {
		t.Fatalf("pointer file = %s", pointerRaw)
	}
	if strings.Contains(string(pointerRaw), "sk-secret-value") {
		t.Fatal("pointer file contains credential")
	}

	// status 显示已切换（无需 vault）。
	statusOut, _, err := runAISwitchCmd(t, aiStatusCmd, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"claude-code", "已切换", "main / m1（2 个模型）", "未切换"} {
		if !strings.Contains(statusOut, want) {
			t.Fatalf("status output missing %q:\n%s", want, statusOut)
		}
	}
	for _, absent := range []string{"cursor", "不支持"} {
		if strings.Contains(statusOut, absent) {
			t.Fatalf("status output must not contain %q:\n%s", absent, statusOut)
		}
	}
}

func TestAISwitchFailures(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"cursor", "main"}); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Fatalf("switch cursor error = %v", err)
	}
	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "missing"}); err == nil {
		t.Fatal("switch missing provider unexpectedly succeeded")
	}

	// 参数类错误必须在解锁与写盘之前失败，且不留下任何文件。
	cases := []struct {
		name    string
		prepare func(*testing.T)
		want    string
	}{
		{"--model removed", setAIRemovedModelFlag, "--model 已移除"},
		{"empty --models", func(t *testing.T) { setAISwitchFlags(t, []string{}, "") }, "--models 不能为空"},
		{"model not in profile", func(t *testing.T) { setAISwitchFlags(t, []string{"m1", "nope"}, "") }, "available"},
		{"missing default and ambiguous set", func(t *testing.T) { setAISwitchFlags(t, nil, "nope") }, "not in the selected model set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.prepare(t)
			if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"}); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("switch error = %v, want containing %q", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
				t.Fatalf("agent config written despite invalid flags: %v", err)
			}
			if _, err := os.Stat(agentPointerPath()); !os.IsNotExist(err) {
				t.Fatalf("pointer written despite invalid flags: %v", err)
			}
		})
	}
}

func TestAISwitchCodexNoSecretAndGuidance(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, errOut, err := runAISwitchCmd(t, aiSwitchCmd, []string{"codex", "main"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	// 名字走 stdout 信息行（不再要求用户手工设置）。
	if !strings.Contains(stdout, "凭据环境变量：SENV_MAIN_API_KEY") {
		t.Fatalf("stdout missing credential env name: %q", stdout)
	}
	// 自有凭据场景的兑底写入在 stderr 告知（vault 变更可见）。
	if !strings.Contains(errOut, "SENV_MAIN_API_KEY") || !strings.Contains(errOut, "senv env export") {
		t.Fatalf("stderr missing seed notice: %q", errOut)
	}
	cfg, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	if strings.Contains(string(cfg), "sk-secret-value") {
		t.Fatal("codex config contains plaintext key")
	}
	var config map[string]any
	if err := toml.Unmarshal(cfg, &config); err != nil {
		t.Fatalf("parse codex TOML: %v", err)
	}
	if config["model"] != "m1" || config["model_provider"] != "senv-main" {
		t.Fatalf("codex top-level = %v", config)
	}
	provider := config["model_providers"].(map[string]any)["senv-main"].(map[string]any)
	if provider["env_key"] != "SENV_MAIN_API_KEY" {
		t.Fatalf("codex provider = %v", provider)
	}
}

func TestAIStatusEmpty(t *testing.T) {
	newAuditTestProject(t)
	t.Setenv("HOME", t.TempDir())
	out, _, err := runAISwitchCmd(t, aiStatusCmd, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "opencode") || !strings.Contains(out, "未切换") {
		t.Fatalf("status output = %q", out)
	}
}

// TestAISwitchModelSetFlagsAndAudit 覆盖任务 1.1/2.1/2.2：显式模型集保序、
// --default-model 只覆盖本次、输出含条数、审计含默认模型与条数且不含凭据。
func TestAISwitchModelSetFlagsAndAudit(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)

	setAISwitchFlags(t, []string{"m2", "m1"}, "m2")
	out, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(out, "默认模型 m2") || !strings.Contains(out, "共 2 个模型") {
		t.Fatalf("switch output = %q", out)
	}

	var settings map[string]any
	if err := json.Unmarshal(mustReadFile(t, filepath.Join(home, ".claude", "settings.json")), &settings); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	if settings["model"] != "m2" {
		t.Fatalf("settings model = %v", settings["model"])
	}
	options := settings["modelPicker"].(map[string]any)["options"].([]any)
	if len(options) != 2 || options[0].(map[string]any)["model"] != "m2" || options[1].(map[string]any)["model"] != "m1" {
		t.Fatalf("modelPicker order = %v, want explicit order m2,m1", options)
	}

	var pointer struct {
		Agents map[string]struct {
			Provider     string   `json:"provider"`
			Models       []string `json:"models"`
			DefaultModel string   `json:"default_model"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(mustReadFile(t, agentPointerPath()), &pointer); err != nil {
		t.Fatalf("parse pointer file: %v", err)
	}
	got := pointer.Agents["claude-code"]
	if got.Provider != "main" || got.DefaultModel != "m2" || strings.Join(got.Models, ",") != "m2,m1" {
		t.Fatalf("pointer = %+v", got)
	}

	// --default-model 不回写档案。
	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("provider manager: %v", err)
	}
	entry, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if entry.DefaultModel != "m1" {
		t.Fatalf("profile default model changed to %q", entry.DefaultModel)
	}

	log := readAuditLogForTest(t)
	if !strings.Contains(log, `default:m2 models:2`) {
		t.Fatalf("audit log missing detail:\n%s", log)
	}
	if strings.Contains(log, "sk-secret-value") {
		t.Fatal("audit log leaked credential material")
	}
}

// TestAISwitchHintsWhenModelSetLarge 覆盖 D3：模型集超过阈值时提示可用
// --models 缩小。
func TestAISwitchHintsWhenModelSetLarge(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-secret-value")
	models := make([]string, 0, aiSwitchModelSetHint+1)
	modelCtx := make([]string, 0, aiSwitchModelSetHint+1)
	for i := 0; i <= aiSwitchModelSetHint; i++ {
		model := fmt.Sprintf("mm-%02d", i)
		models = append(models, model)
		modelCtx = append(modelCtx, model+"=128000")
	}
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = models
		providerAddModelCtx = modelCtx
		providerAddDefault = models[0]
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"big"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	t.Setenv("HOME", t.TempDir())

	out, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "big"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(out, "共 21 个模型") || !strings.Contains(out, "--models") {
		t.Fatalf("switch output should hint a smaller set:\n%s", out)
	}
}

// TestAIStatusDriftAgainstProfile 覆盖任务 3.1/3.2：status 展示默认模型与
// 条数；档案缩集后出现漂移提示；档案不可得时只省略提示、照常展示指针。
func TestAIStatusDriftAgainstProfile(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	t.Setenv("HOME", t.TempDir())

	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"}); err != nil {
		t.Fatalf("switch: %v", err)
	}
	out, _, err := runAISwitchCmd(t, aiStatusCmd, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "main / m1（2 个模型）") {
		t.Fatalf("status output = %q", out)
	}
	if strings.Contains(out, "已不含模型") {
		t.Fatalf("status reported drift while profile matches:\n%s", out)
	}

	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("provider manager: %v", err)
	}
	// 缩集必须同时清掉目录来源，否则模型集会被目录重新并回全集。
	noCatalog := ""
	if _, err := mgr.EditProvider(llm.EditProviderOptions{
		Alias:           "main",
		CatalogPath:     catalogCachePath(),
		CatalogProvider: &noCatalog,
		Models:          []string{"m1"},
	}); err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	out, _, err = runAISwitchCmd(t, aiStatusCmd, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "已不含模型 m2") {
		t.Fatalf("status should flag drift:\n%s", out)
	}

	// 档案不可得：省略漂移提示，指针照常展示，不报错。
	clearAuthMemo()
	out, _, err = runAISwitchCmd(t, aiStatusCmd, nil)
	if err != nil {
		t.Fatalf("status without vault: %v", err)
	}
	if strings.Contains(out, "已不含模型") {
		t.Fatalf("status should omit drift without a profile:\n%s", out)
	}
	if !strings.Contains(out, "已切换") {
		t.Fatalf("status should still show the pointer:\n%s", out)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// addAIProviderWithKeyRefForSwitchTest 建一个以外部引用（env:/text:）取凭据的
// 档案，复用 provider add 的既有测试脚手架。
func addAIProviderWithKeyRefForSwitchTest(t *testing.T, alias, keyRef string) {
	t.Helper()
	writeAIProviderTestCatalog(t)
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddCatalog = "p1"
		providerAddDefault = "m1"
		providerAddKeyRef = keyRef
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{alias}); err != nil {
		t.Fatalf("add: %v", err)
	}
}

// codexEnvKeyOf 读取 codex 配置里 senv provider 的 env_key。
func codexEnvKeyOf(t *testing.T, configPath, alias string) string {
	t.Helper()
	var config map[string]any
	if err := toml.Unmarshal(mustReadFile(t, configPath), &config); err != nil {
		t.Fatalf("parse codex TOML: %v", err)
	}
	providers, _ := config["model_providers"].(map[string]any)
	provider, _ := providers["senv-"+alias].(map[string]any)
	key, _ := provider["env_key"].(string)
	return key
}

// TestAISwitchCodexReusesReferencedEnvName 覆盖 ADR-0024 的主路径：env: 引用
// 复用被引用 key 名，切换不写 vault、不产生 warning，`senv env export` 已提供。
func TestAISwitchCodexReusesReferencedEnvName(t *testing.T) {
	cfg, data := newAuditTestProject(t)
	em := env.NewManager(storage.NewManager(cfg, data), "audit-password")
	if err := em.Set("ai", "DEEPSEEK_API_KEY", "sk-env-value"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	if err := em.ActivateGroup("ai"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	addAIProviderWithKeyRefForSwitchTest(t, "deepseek", "env:ai/DEEPSEEK_API_KEY")

	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, errOut, err := runAISwitchCmd(t, aiSwitchCmd, []string{"codex", "deepseek"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(stdout, "凭据环境变量：DEEPSEEK_API_KEY") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.TrimSpace(errOut) != "" {
		t.Fatalf("unexpected warning: %q", errOut)
	}
	if got := codexEnvKeyOf(t, filepath.Join(home, ".codex", "config.toml"), "deepseek"); got != "DEEPSEEK_API_KEY" {
		t.Fatalf("env_key = %q", got)
	}
	vars, _, err := em.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, ok := vars["default"]["DEEPSEEK_API_KEY"]; ok {
		t.Fatal("switch added an env entry although the referenced name is already exported")
	}
}

// TestAISwitchCodexWarnsOnInactiveCredentialGroup：引用组未激活时给出可操作的
// 激活命令，而不是含糊提示。
func TestAISwitchCodexWarnsOnInactiveCredentialGroup(t *testing.T) {
	cfg, data := newAuditTestProject(t)
	em := env.NewManager(storage.NewManager(cfg, data), "audit-password")
	if err := em.Set("dev", "APP_KEY", "sk-dev"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	addAIProviderWithKeyRefForSwitchTest(t, "stag", "env:dev/APP_KEY")

	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, errOut, err := runAISwitchCmd(t, aiSwitchCmd, []string{"codex", "stag"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(stdout, "凭据环境变量：APP_KEY") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(errOut, "senv env group activate dev") {
		t.Fatalf("stderr = %q, want the activation command", errOut)
	}
	if got := codexEnvKeyOf(t, filepath.Join(home, ".codex", "config.toml"), "stag"); got != "APP_KEY" {
		t.Fatalf("env_key = %q", got)
	}
}
