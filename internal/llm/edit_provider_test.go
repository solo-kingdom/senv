package llm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/text"
)

func ptr(s string) *string { return &s }

func addTestProvider(t *testing.T, mgr *ProviderManager, opts AddProviderOptions) *AddProviderResult {
	t.Helper()
	res, err := mgr.AddProvider(opts)
	if err != nil {
		t.Fatalf("AddProvider(%s): %v", opts.Alias, err)
	}
	return res
}

func TestEditProviderUpdatesFields(t *testing.T) {
	mgr, store, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, catalogPayload, time.Now())
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com",
		APIKey: "sk-secret", Models: []string{"m1", "m2"}, DefaultModel: "m1",
	})

	res, err := mgr.EditProvider(EditProviderOptions{
		Alias:           "main",
		BaseURL:         ptr("https://new.example.com"),
		APIShape:        ptr(string(APIShapeOpenAIChat)),
		CatalogProvider: ptr("p1"),
		Models:          []string{"custom-1"},
		DefaultModel:    ptr("custom-1"),
		CatalogPath:     catalogPath,
	})
	if err != nil {
		t.Fatalf("EditProvider: %v", err)
	}
	// base URL 归一为 OpenAI 兼容形态。
	if res.Entry.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("BaseURL = %q", res.Entry.BaseURL)
	}
	if res.Entry.APIShape != string(APIShapeOpenAIChat) {
		t.Fatalf("APIShape = %q", res.Entry.APIShape)
	}
	if got := strings.Join(res.Entry.Models, ","); got != "custom-1,m1,m2" {
		t.Fatalf("Models = %q", got)
	}
	if res.Entry.DefaultModel != "custom-1" {
		t.Fatalf("DefaultModel = %q", res.Entry.DefaultModel)
	}
	// 未提供凭据来源时保留旧自有凭据。
	value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
	if err != nil || value != "sk-secret" {
		t.Fatalf("credential = %q, %v; want preserved", value, err)
	}
	if res.Entry.CredentialRef != OwnedCredentialRef("main") {
		t.Fatalf("credential ref = %q", res.Entry.CredentialRef)
	}

	// 再次编辑：不带模型相关字段时保留模型集。
	res2, err := mgr.EditProvider(EditProviderOptions{Alias: "main", DefaultModel: ptr("m2")})
	if err != nil {
		t.Fatalf("EditProvider(second): %v", err)
	}
	if got := strings.Join(res2.Entry.Models, ","); got != "custom-1,m1,m2" {
		t.Fatalf("models changed by unrelated edit: %q", got)
	}
	if res2.Entry.DefaultModel != "m2" {
		t.Fatalf("DefaultModel = %q, want m2", res2.Entry.DefaultModel)
	}
}

func TestEditProviderCredentialRotation(t *testing.T) {
	t.Run("rotate owned credential", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		addTestProvider(t, mgr, AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"},
		})
		if _, err := mgr.EditProvider(EditProviderOptions{Alias: "main", APIKey: "new"}); err != nil {
			t.Fatalf("EditProvider: %v", err)
		}
		value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
		if err != nil || value != "new" {
			t.Fatalf("credential = %q, %v; want new", value, err)
		}
	})

	t.Run("external ref deletes owned credential", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		addTestProvider(t, mgr, AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"},
		})
		res, err := mgr.EditProvider(EditProviderOptions{Alias: "main", KeyRef: ptr("env:llm/KEY")})
		if err != nil {
			t.Fatalf("EditProvider: %v", err)
		}
		if res.Entry.CredentialRef != "env:llm/KEY" {
			t.Fatalf("credential ref = %q", res.Entry.CredentialRef)
		}
		if _, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned credential not removed: %v", err)
		}
	})

	t.Run("mutually exclusive inputs", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		addTestProvider(t, mgr, AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"},
		})
		_, err := mgr.EditProvider(EditProviderOptions{Alias: "main", APIKey: "x", KeyRef: ptr("env:llm/KEY")})
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestEditProviderRejectsWithoutPartialUpdate(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"}, DefaultModel: "m1",
	})

	cases := []struct {
		name string
		opts EditProviderOptions
		want string
	}{
		{"default not in final model set",
			EditProviderOptions{Alias: "main", Models: []string{"m2"}}, "final model set"},
		{"invalid api shape",
			EditProviderOptions{Alias: "main", APIShape: ptr("openai")}, "api_shape"},
		{"bad key ref",
			EditProviderOptions{Alias: "main", KeyRef: ptr("vault:llm/KEY")}, "credential ref"},
		{"missing provider",
			EditProviderOptions{Alias: "nope", BaseURL: ptr("https://b")}, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.EditProvider(tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want contains %q", err, tc.want)
			}
		})
	}

	// 全部失败都不留下部分更新。
	entry, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if entry.BaseURL != "https://a/v1" || entry.DefaultModel != "m1" || len(entry.Models) != 1 {
		t.Fatalf("provider mutated by failed edits: %+v", entry)
	}
	value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
	if err != nil || value != "old" {
		t.Fatalf("credential mutated by failed edits: %q, %v", value, err)
	}
}

func TestEditProviderClearsAPIShape(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m1"},
		APIShape: string(APIShapeAnthropic),
	})
	entry, err := mgr.GetProvider("main")
	if err != nil || entry.APIShape != string(APIShapeAnthropic) {
		t.Fatalf("APIShape = %q, %v", entry.APIShape, err)
	}
	res, err := mgr.EditProvider(EditProviderOptions{Alias: "main", APIShape: ptr("")})
	if err != nil {
		t.Fatalf("EditProvider(clear): %v", err)
	}
	if res.Entry.APIShape != "" {
		t.Fatalf("APIShape = %q, want cleared", res.Entry.APIShape)
	}
}

func TestAddProviderRejectsInvalidAPIShape(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	_, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m1"}, APIShape: "openai",
	})
	if err == nil || !strings.Contains(err.Error(), "api_shape") {
		t.Fatalf("error = %v", err)
	}
}

// TestSwitchAPIShapeCompatibility 覆盖兼容判定：形态与 agent 协议族不匹配时
// 拒绝且不写任何文件。
func TestSwitchAPIShapeCompatibility(t *testing.T) {
	t.Run("openai shape rejected for anthropic agent", func(t *testing.T) {
		pm, _, _ := newTestProviderManager(t)
		addTestProvider(t, pm, AddProviderOptions{
			Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
			Models: []string{"m1"}, APIShape: string(APIShapeOpenAIChat),
		})
		home := t.TempDir()
		sm := NewSwitchManager(pm, "", home)
		_, err := sm.Switch("claude-code", "main", nil, "")
		if err == nil || !strings.Contains(err.Error(), "incompatible") {
			t.Fatalf("Switch error = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(statErr) {
			t.Fatalf("config written despite incompatible shape (stat err = %v)", statErr)
		}
		// 同一形态对 OpenAI 兼容 agent 合法。
		if _, err := sm.Switch("codex", "main", nil, ""); err != nil {
			t.Fatalf("Switch(codex) error = %v", err)
		}
	})

	t.Run("anthropic shape accepted for claude-code", func(t *testing.T) {
		pm, _, _ := newTestProviderManager(t)
		addTestProvider(t, pm, AddProviderOptions{
			Alias: "main", BaseURL: "https://api.example.com/v1", APIKey: "sk-secret",
			Models: []string{"m1"}, APIShape: string(APIShapeAnthropic),
		})
		home := t.TempDir()
		sm := NewSwitchManager(pm, "", home)
		out, err := sm.Switch("claude-code", "main", nil, "")
		if err != nil {
			t.Fatalf("Switch error = %v", err)
		}
		// 形态为 Anthropic 时仍按 Anthropic 协议族归一（剥末段 /v1）。
		if out.BaseURL != "https://api.example.com" {
			t.Fatalf("BaseURL = %q", out.BaseURL)
		}
	})

	t.Run("empty shape keeps inference", func(t *testing.T) {
		pm, _, _ := newTestProviderManager(t)
		addTestProvider(t, pm, AddProviderOptions{
			Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret", Models: []string{"m1"},
		})
		home := t.TempDir()
		sm := NewSwitchManager(pm, "", home)
		out, err := sm.Switch("claude-code", "main", nil, "")
		if err != nil {
			t.Fatalf("Switch error = %v", err)
		}
		if out.BaseURL != "https://api.example.com" {
			t.Fatalf("BaseURL = %q", out.BaseURL)
		}
	})
}
