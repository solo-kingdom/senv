package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

func setAIModelFlag(t *testing.T, v string) {
	t.Helper()
	old := aiSwitchModel
	aiSwitchModel = v
	t.Cleanup(func() { aiSwitchModel = old })
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
	if !strings.Contains(out, "Claude Code → main") || !strings.Contains(out, "模型 m1") {
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
	for _, want := range []string{"claude-code", "已切换", "main / m1", "cursor", "不支持", "未切换"} {
		if !strings.Contains(statusOut, want) {
			t.Fatalf("status output missing %q:\n%s", want, statusOut)
		}
	}
}

func TestAISwitchFailures(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	t.Setenv("HOME", t.TempDir())

	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"cursor", "main"}); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Fatalf("switch cursor error = %v", err)
	}
	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "missing"}); err == nil {
		t.Fatal("switch missing provider unexpectedly succeeded")
	}
	setAIModelFlag(t, "nope")
	if _, _, err := runAISwitchCmd(t, aiSwitchCmd, []string{"claude-code", "main"}); err == nil ||
		!strings.Contains(err.Error(), "available") {
		t.Fatalf("switch bad model error = %v", err)
	}
}

func TestAISwitchCodexNoSecretAndGuidance(t *testing.T) {
	newAuditTestProject(t)
	addAIProviderForSwitchTest(t, "main")
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, errOut, err := runAISwitchCmd(t, aiSwitchCmd, []string{"codex", "main"})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(errOut, "SENV_MAIN_API_KEY") {
		t.Fatalf("stderr guidance missing env var name: %q", errOut)
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
