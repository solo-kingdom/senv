package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/text"
)

// codex 凭据提示的 TUI 契约（ADR-0024 / llm-provider-tui 的「切换结果提示
// 完整性」要求）：名字取本次切换结果、全部 warning 都可见、非 codex 不提示。

func TestAITabSwitchCodexUsesReferencedEnvName(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	em := tab.mgr.Env
	if err := em.AddGroup("ai", "test"); err != nil {
		t.Fatal(err)
	}
	if err := em.Set("ai", "DEEPSEEK_API_KEY", "sk-env"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	if err := em.ActivateGroup("ai"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := tab.mgr.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "deepseek", BaseURL: "https://api.deepseek.com/v1",
		KeyRef: "env:ai/DEEPSEEK_API_KEY", Models: []string{"m1"}, DefaultModel: "m1",
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	runAITabLoad(t, tab)
	if len(tab.providers) == 0 || tab.providers[0].Alias != "deepseek" {
		t.Fatalf("providers = %v, want deepseek selected first", tab.providers)
	}

	msg := driveAISwitch(t, tab, 1, 0, "s") // codex + deepseek
	batch := switchOutcome(t, msg)
	result := batch.results[0]
	if result.err != nil {
		t.Fatalf("switch msg = %#v", msg)
	}
	if result.out.CredentialEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("CredentialEnv = %q", result.out.CredentialEnv)
	}
	notice := strings.Join(collectAIToasts(t, tab, batch), "; ")
	if !strings.Contains(notice, "reads credentials from env DEEPSEEK_API_KEY") {
		t.Fatalf("notice = %q, want the referenced env name", notice)
	}
	if strings.Contains(notice, "SENV_DEEPSEEK") {
		t.Fatalf("notice = %q, must not show the derived name", notice)
	}
}

func TestAITabSwitchCodexShowsEveryWarning(t *testing.T) {
	tab, _, store := newAITestTabWithStore(t)
	em := tab.mgr.Env
	if err := em.AddGroup("dev", "test"); err != nil {
		t.Fatal(err)
	}
	if err := em.Set("dev", "APP_KEY", "sk-dev"); err != nil {
		t.Fatalf("env set: %v", err)
	}
	res, err := tab.mgr.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "deepseek", BaseURL: "https://api.deepseek.com/v1",
		KeyRef: "env:dev/APP_KEY", Models: []string{"m1"}, DefaultModel: "m1",
		ModelReasoning:   map[string][]string{"m1": {"low", "high"}},
		DefaultReasoning: "high",
	})
	if err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	// 模拟旧档案：有推理档位但没有默认档，切换仍可用并给出补全提示。
	entry := res.Entry
	info := entry.ModelInfo["m1"]
	info.DefaultReasoning = ""
	entry.ModelInfo["m1"] = info
	if err := store.SaveLLMProvider("deepseek", entry, "test-password"); err != nil {
		t.Fatalf("SaveLLMProvider: %v", err)
	}
	runAITabLoad(t, tab)

	msg := driveAISwitch(t, tab, 1, 0, "s") // codex + deepseek
	batch := switchOutcome(t, msg)
	result := batch.results[0]
	if result.err != nil {
		t.Fatalf("switch msg = %#v", msg)
	}
	if len(result.out.Warnings) < 2 {
		t.Fatalf("warnings = %v, want both the group and metadata warning", result.out.Warnings)
	}
	notice := strings.Join(collectAIToasts(t, tab, batch), "; ")
	// 组未激活是第二条 warning，旧实现只展示第一条时本断言会失败。
	if !strings.Contains(notice, "senv env group activate dev") {
		t.Fatalf("notice = %q, missing the activation warning", notice)
	}
	if !strings.Contains(notice, "未声明默认推理档") {
		t.Fatalf("notice = %q, missing the metadata warning", notice)
	}
	if _, err := filepath.Glob(filepath.Join(tab.mgr.LLMHome, ".codex", "config.toml")); err != nil {
		t.Fatalf("codex config missing: %v", err)
	}
}

// newAITestTabWithStore 与 newAITestTab 同构，但把 storage manager 交回调用方，
// 便于构造「直接写档案」的旧档案场景。
func newAITestTabWithStore(t *testing.T) (*aiTab, string, *storage.Manager) {
	t.Helper()
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	home := t.TempDir()
	mgr := Managers{
		Env:         env.NewManager(store, "test-password"),
		Text:        text.NewManager(store, "test-password"),
		LLM:         llm.NewProviderManager(store, "test-password"),
		LLMPointer:  llm.DefaultPointerPath(home),
		LLMHome:     home,
		LLMCatalog:  filepath.Join(base, "cache", "models-dev.json"),
		AuditWriter: &fakeAuditWriter{},
	}
	tab := newAITab(mgr)
	tab.SetSize(100, 24)
	return tab, home, store
}

func TestAITabSwitchPiUsesCredentialEnv(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)

	msg := driveAISwitch(t, tab, 3, 0, "s") // pi + main
	batch := switchOutcome(t, msg)
	result := batch.results[0]
	if result.err != nil {
		t.Fatalf("switch msg = %#v", msg)
	}
	// pi 支持 "$NAME" 环境变量插值（ADR-0031），auto 模式走 CredentialEnvVar，
	// CredentialEnv 非空。
	if result.out.CredentialEnv == "" {
		t.Fatal("CredentialEnv = \"\", want an env var name for pi under auto mode")
	}
	notice := strings.Join(collectAIToasts(t, tab, batch), "; ")
	if !strings.Contains(notice, "reads credentials from env "+result.out.CredentialEnv) {
		t.Fatalf("notice = %q, want the env credential hint", notice)
	}
}

// TestAITabSwitchCredentialModeStep 覆盖 ADR-0031 的 TUI 凭据模式步骤：选中
// inline 时配置写明文，选中 env 时配置只写引用。
func TestAITabSwitchCredentialModeStep(t *testing.T) {
	t.Run("inline writes plaintext", func(t *testing.T) {
		tab, home, _ := newAITestTab(t)
		runAITabLoad(t, tab)
		tab.focusLeft = false
		tab.agentIndex = rowIndexOf(t, tab, "pi")

		tab.Update(runeKey("s"))
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // 模型集 → 默认模型
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → 凭据模式
		if tab.flow != aiFlowSelectCredentialMode {
			t.Fatalf("flow = %v, want selectCredentialMode", tab.flow)
		}
		// 游标移到 inline（下标 2）并确认。
		tab.Update(runeKey("j"))
		tab.Update(runeKey("j"))
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if tab.flow != aiFlowConfirm {
			t.Fatalf("flow = %v, want confirm", tab.flow)
		}
		_, cmd := tab.Update(runeKey("y"))
		if cmd == nil {
			t.Fatal("confirm did not return a switch command")
		}
		batch := switchOutcome(t, cmd())
		if batch.results[0].err != nil {
			t.Fatalf("switch error: %v", batch.results[0].err)
		}
		if batch.results[0].out.CredentialEnv != "" {
			t.Fatalf("CredentialEnv = %q, want empty under inline", batch.results[0].out.CredentialEnv)
		}
		raw, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "sk-tui-secret") {
			t.Fatalf("inline mode must write plaintext:\n%s", raw)
		}
	})

	t.Run("env writes reference", func(t *testing.T) {
		tab, home, _ := newAITestTab(t)
		runAITabLoad(t, tab)
		tab.focusLeft = false
		tab.agentIndex = rowIndexOf(t, tab, "pi")

		tab.Update(runeKey("s"))
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → 凭据模式
		if tab.flow != aiFlowSelectCredentialMode {
			t.Fatalf("flow = %v, want selectCredentialMode", tab.flow)
		}
		// 游标移到 env（下标 1）并确认。
		tab.Update(runeKey("j"))
		tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if tab.flow != aiFlowConfirm {
			t.Fatalf("flow = %v, want confirm", tab.flow)
		}
		_, cmd := tab.Update(runeKey("y"))
		if cmd == nil {
			t.Fatal("confirm did not return a switch command")
		}
		batch := switchOutcome(t, cmd())
		if batch.results[0].err != nil {
			t.Fatalf("switch error: %v", batch.results[0].err)
		}
		if batch.results[0].out.CredentialEnv == "" {
			t.Fatal("CredentialEnv = \"\", want an env var name under env mode")
		}
		raw, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "sk-tui-secret") {
			t.Fatalf("env mode must not write plaintext:\n%s", raw)
		}
		wantRef := "$" + batch.results[0].out.CredentialEnv
		if !strings.Contains(string(raw), wantRef) {
			t.Fatalf("pi config missing %q:\n%s", wantRef, raw)
		}
	})
}
