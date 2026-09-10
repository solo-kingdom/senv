package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// TestPointerVersion1Compat 覆盖旧指针（只有 model 字段）读作单元素集，
// 且下一次写回升级为新结构。
func TestPointerVersion1Compat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-pointers.json")
	legacy := `{"version":1,"agents":{"claude-code":{"provider":"p1","model":"m1","switched_at":"2026-09-01T10:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := LoadPointers(path)
	if err != nil {
		t.Fatalf("LoadPointers() error = %v", err)
	}
	p, ok := loaded.Get("claude-code")
	if !ok {
		t.Fatal("Get(claude-code) not found")
	}
	if len(p.Models) != 1 || p.Models[0] != "m1" || p.DefaultModel != "m1" {
		t.Fatalf("v1 pointer normalized to %+v, want models [m1] default m1", p)
	}

	if err := SavePointers(path, loaded); err != nil {
		t.Fatalf("SavePointers() error = %v", err)
	}
	raw := string(mustRead(t, path))
	if !strings.Contains(raw, `"models"`) || !strings.Contains(raw, `"default_model"`) {
		t.Fatalf("upgraded pointer missing new fields: %s", raw)
	}
	if strings.Contains(raw, `"model":`) {
		t.Fatalf("upgraded pointer still carries the legacy field: %s", raw)
	}
}

// TestSwitchModelSetResolution 覆盖 Agent 模型集与默认模型的解析与校验。
func TestSwitchModelSetResolution(t *testing.T) {
	t.Run("省略模型集取全集", func(t *testing.T) {
		sm, _ := newTestSwitchManager(t)
		out, err := sm.Switch("claude-code", "main", nil, "")
		if err != nil {
			t.Fatalf("Switch() error = %v", err)
		}
		if len(out.Models) != 2 || out.Models[0] != "m1" || out.Models[1] != "m2" || out.DefaultModel != "m1" {
			t.Fatalf("out = %+v, want models [m1 m2] default m1", out)
		}
	})

	t.Run("显式子集保序", func(t *testing.T) {
		sm, home := newTestSwitchManager(t)
		out, err := sm.Switch("opencode", "main", []string{"m2", "m1"}, "m2")
		if err != nil {
			t.Fatalf("Switch() error = %v", err)
		}
		if len(out.Models) != 2 || out.Models[0] != "m2" || out.Models[1] != "m1" {
			t.Fatalf("Models = %v, want [m2 m1]", out.Models)
		}
		pf, err := LoadPointers(DefaultPointerPath(home))
		if err != nil {
			t.Fatalf("LoadPointers() error = %v", err)
		}
		p, _ := pf.Get("opencode")
		if len(p.Models) != 2 || p.Models[0] != "m2" || p.DefaultModel != "m2" {
			t.Fatalf("pointer = %+v, want ordered [m2 m1] default m2", p)
		}
	})

	t.Run("含档案外模型", func(t *testing.T) {
		sm, _ := newTestSwitchManager(t)
		if _, err := sm.Switch("claude-code", "main", []string{"m1", "nope"}, ""); err == nil ||
			!strings.Contains(err.Error(), "available") {
			t.Fatalf("Switch(bad model) error = %v", err)
		}
	})

	t.Run("默认模型不在集合内", func(t *testing.T) {
		sm, _ := newTestSwitchManager(t)
		if _, err := sm.Switch("claude-code", "main", []string{"m1"}, "m2"); err == nil ||
			!strings.Contains(err.Error(), "not in the selected model set") {
			t.Fatalf("Switch(default outside set) error = %v", err)
		}
	})

	t.Run("无默认模型且模型集多于一个", func(t *testing.T) {
		sm, _ := newTestSwitchManager(t)
		if err := sm.providerManager.save("nodefault", &storage.LLMProviderEntry{
			Alias: "nodefault", BaseURL: "https://api.example.com",
			CredentialRef: "text:llm-keys/nodefault", Models: []string{"m1", "m2"},
		}); err != nil {
			t.Fatalf("save provider: %v", err)
		}
		if _, err := sm.Switch("claude-code", "nodefault", nil, ""); err == nil ||
			!strings.Contains(err.Error(), "no default model") {
			t.Fatalf("Switch(no default) error = %v", err)
		}
	})

	t.Run("无默认模型但模型集恰有一个", func(t *testing.T) {
		sm, _ := newTestSwitchManager(t)
		if err := sm.providerManager.save("single", &storage.LLMProviderEntry{
			Alias: "single", BaseURL: "https://api.example.com",
			CredentialRef: "text:llm-keys/single", Models: []string{"only"},
		}); err != nil {
			t.Fatalf("save provider: %v", err)
		}
		if err := sm.providerManager.textManager().Set(LLMKeysGroup, "single", "sk-secret"); err != nil {
			t.Fatalf("store credential: %v", err)
		}
		out, err := sm.Switch("claude-code", "single", nil, "")
		if err != nil {
			t.Fatalf("Switch() error = %v", err)
		}
		if out.DefaultModel != "only" {
			t.Fatalf("DefaultModel = %q, want only", out.DefaultModel)
		}
	})

}
