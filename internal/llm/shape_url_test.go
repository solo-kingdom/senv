package llm

import (
	"strings"
	"testing"
)

func TestParseShapeURLs(t *testing.T) {
	got, err := ParseShapeURLs([]string{
		"openai-chat=https://chat.example.com/v1",
		"anthropic=https://gw.example.com/api/anthropic",
	})
	if err != nil {
		t.Fatalf("ParseShapeURLs() error = %v", err)
	}
	if got["openai-chat"] != "https://chat.example.com/v1" {
		t.Fatalf("openai-chat = %q", got["openai-chat"])
	}
	if got["anthropic"] != "https://gw.example.com/api/anthropic" {
		t.Fatalf("anthropic = %q", got["anthropic"])
	}
	// edit 语义：空 value 合法（清除该形态地址）。
	got, err = ParseShapeURLs([]string{"openai-chat="})
	if err != nil || got["openai-chat"] != "" {
		t.Fatalf("ParseShapeURLs(empty value) = %v, %v", got, err)
	}
	if got, err := ParseShapeURLs(nil); err != nil || got != nil {
		t.Fatalf("ParseShapeURLs(nil) = %v, %v", got, err)
	}
	for _, spec := range []string{"openai=https://x", "=https://x", "openai-chat"} {
		if _, err := ParseShapeURLs([]string{spec}); err == nil {
			t.Fatalf("ParseShapeURLs(%q) unexpectedly succeeded", spec)
		}
	}
	if _, err := ParseShapeURLs([]string{"openai-chat=https://a", "openai-chat=https://b"}); err == nil {
		t.Fatal("duplicate shape unexpectedly succeeded")
	}
}

func TestAddProviderShapeURLs(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "gw", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"},
		ShapeURLs: map[string]string{
			"openai-chat": "https://chat.example.com",
			"anthropic":   "https://gw.example.com/api/anthropic/",
		},
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	// OpenAI 族形态地址沿用兼容归一；anthropic 原样仅收敛尾斜杠。
	if res.Entry.ChatBaseURL != "https://chat.example.com/v1" {
		t.Fatalf("ChatBaseURL = %q", res.Entry.ChatBaseURL)
	}
	if res.Entry.AnthropicBaseURL != "https://gw.example.com/api/anthropic" {
		t.Fatalf("AnthropicBaseURL = %q", res.Entry.AnthropicBaseURL)
	}
	if res.Entry.ResponsesBaseURL != "" {
		t.Fatalf("ResponsesBaseURL = %q, want empty", res.Entry.ResponsesBaseURL)
	}
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "https://chat.example.com/v1") {
		t.Fatalf("warnings missing normalization notice: %v", res.Warnings)
	}
	// 归一后的值持久化。
	saved, err := mgr.GetProvider("gw")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if saved.ChatBaseURL != "https://chat.example.com/v1" || saved.AnthropicBaseURL != "https://gw.example.com/api/anthropic" {
		t.Fatalf("saved shape URLs = %q / %q", saved.ChatBaseURL, saved.AnthropicBaseURL)
	}
}

func TestAddProviderShapeURLRejections(t *testing.T) {
	cases := []struct {
		name     string
		shapeURL map[string]string
		want     string
	}{
		{"非法 key", map[string]string{"openai": "https://x.example.com"}, "invalid shape URL key"},
		{"add 空值", map[string]string{"openai-chat": ""}, "requires a URL"},
		{"userinfo", map[string]string{"anthropic": "https://user:pass@gw.example.com/a"}, "userinfo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _, _ := newTestProviderManager(t)
			_, err := mgr.AddProvider(AddProviderOptions{
				Alias: "gw", BaseURL: "https://api.example.com", APIKey: "sk-secret",
				Models: []string{"m1"}, ShapeURLs: tc.shapeURL,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("AddProvider() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestEditProviderShapeURLs(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"},
		ShapeURLs: map[string]string{
			"openai-chat": "https://chat.example.com/v1",
			"anthropic":   "https://gw.example.com/api/anthropic",
		},
	})

	// 清空单个 key；未出现的 key 保留。
	if _, err := mgr.EditProvider(EditProviderOptions{
		Alias: "main", ShapeURLs: map[string]string{"openai-chat": ""},
	}); err != nil {
		t.Fatalf("EditProvider() error = %v", err)
	}
	e, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if e.ChatBaseURL != "" {
		t.Fatalf("ChatBaseURL = %q, want cleared", e.ChatBaseURL)
	}
	if e.AnthropicBaseURL != "https://gw.example.com/api/anthropic" {
		t.Fatalf("AnthropicBaseURL = %q, want preserved", e.AnthropicBaseURL)
	}

	// 空 map = 清空全部形态地址（flag 传入但无有效值的约定）。
	if _, err := mgr.EditProvider(EditProviderOptions{
		Alias: "main", ShapeURLs: map[string]string{},
	}); err != nil {
		t.Fatalf("EditProvider(empty map) error = %v", err)
	}
	e, _ = mgr.GetProvider("main")
	if e.AnthropicBaseURL != "" {
		t.Fatalf("AnthropicBaseURL = %q, want cleared", e.AnthropicBaseURL)
	}

	// nil = 保留全部。
	if _, err := mgr.EditProvider(EditProviderOptions{
		Alias: "main", ShapeURLs: map[string]string{"anthropic": "https://new.example.com/anthropic"},
	}); err != nil {
		t.Fatalf("EditProvider() error = %v", err)
	}
	e, _ = mgr.GetProvider("main")
	if e.AnthropicBaseURL != "https://new.example.com/anthropic" {
		t.Fatalf("AnthropicBaseURL = %q", e.AnthropicBaseURL)
	}
}

func TestSwitchShapeURLGate(t *testing.T) {
	// 显式 anthropic 形态地址放行 openai-chat 声明（形态地址的存在本身就是
	// 「该族被服务」的声明）。
	mgr, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "gw", BaseURL: "https://api.example.com", APIShape: "openai-chat",
		APIKey: "sk-secret", Models: []string{"m1"},
		ShapeURLs: map[string]string{"anthropic": "https://gw.example.com/api/anthropic"},
	})
	home := t.TempDir()
	out, err := NewSwitchManager(mgr, "", home).Switch("claude-code", "gw", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.BaseURL != "https://gw.example.com/api/anthropic" {
		t.Fatalf("BaseURL = %q", out.BaseURL)
	}

	// 无显式地址时维持旧判定，拒绝文案给出三个动作。
	mgr2, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr2, AddProviderOptions{
		Alias: "chat-only", BaseURL: "https://api.example.com", APIShape: "openai-chat",
		APIKey: "sk-secret", Models: []string{"m1"},
	})
	if _, err := NewSwitchManager(mgr2, "", t.TempDir()).Switch("claude-code", "chat-only", nil, ""); err == nil ||
		!strings.Contains(err.Error(), "incompatible") ||
		!strings.Contains(err.Error(), "--shape-url") ||
		!strings.Contains(err.Error(), "switch to a different provider") {
		t.Fatalf("Switch() error = %v, want three-action message", err)
	}

	// OpenAI 族地址存在即放行 anthropic 声明；codex 线协议维持其默认 responses。
	mgr3, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr3, AddProviderOptions{
		Alias: "resp", BaseURL: "https://api.example.com", APIShape: "anthropic",
		APIKey: "sk-secret", Models: []string{"m1"},
		ShapeURLs: map[string]string{"openai-responses": "https://resp.example.com/v1"},
	})
	out3, err := NewSwitchManager(mgr3, "", t.TempDir()).Switch("codex", "resp", nil, "")
	if err != nil {
		t.Fatalf("Switch(codex) error = %v", err)
	}
	if out3.BaseURL != "https://resp.example.com/v1" {
		t.Fatalf("codex BaseURL = %q", out3.BaseURL)
	}
}

func TestSwitchShapeURLResolution(t *testing.T) {
	// claude-code：显式 anthropic 地址原样写回且标注来源；带 /v1 的值不被修正。
	mgr, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models:    []string{"m1"},
		ShapeURLs: map[string]string{"anthropic": "https://gw.example.com/api/anthropic/v1"},
	})
	out, err := NewSwitchManager(mgr, "", t.TempDir()).Switch("claude-code", "main", nil, "")
	if err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if out.BaseURL != "https://gw.example.com/api/anthropic/v1" || out.BaseURLSource != "anthropic_base_url" {
		t.Fatalf("BaseURL/Source = %q / %q", out.BaseURL, out.BaseURLSource)
	}

	// codex 只讲 Responses：声明 openai-chat 且仅有 chat_base_url 的档案在门禁
	// 处拒绝，文案给可行动作。
	mgr2, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr2, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIShape: "openai-chat",
		APIKey: "sk-secret", Models: []string{"m1"},
		ShapeURLs: map[string]string{"openai-chat": "https://chat.example.com/v1"},
	})
	if _, err := NewSwitchManager(mgr2, "", t.TempDir()).Switch("codex", "main", nil, ""); err == nil ||
		!strings.Contains(err.Error(), "openai-chat") ||
		!strings.Contains(err.Error(), "--shape-url") {
		t.Fatalf("Switch(codex) error = %v, want chat-wire rejection", err)
	}

	// 同一档案补 responses_base_url 后放行：codex 取 responses 地址而非 chat。
	mgr2b, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr2b, AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIShape: "openai-chat",
		APIKey: "sk-secret", Models: []string{"m1"},
		ShapeURLs: map[string]string{
			"openai-chat":      "https://chat.example.com/v1",
			"openai-responses": "https://resp.example.com/v1",
		},
	})
	out2, err := NewSwitchManager(mgr2b, "", t.TempDir()).Switch("codex", "main", nil, "")
	if err != nil {
		t.Fatalf("Switch(codex with responses url) error = %v", err)
	}
	if out2.BaseURL != "https://resp.example.com/v1" || out2.BaseURLSource != "responses_base_url" {
		t.Fatalf("codex BaseURL/Source = %q / %q", out2.BaseURL, out2.BaseURLSource)
	}

	mgr3, _, _ := newTestProviderManager(t)
	addTestProvider(t, mgr3, AddProviderOptions{
		Alias: "plain", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"},
	})
	// 未设任何形态地址：地址与来源均为推断路径（回归现状）。
	out3, err := NewSwitchManager(mgr3, "", t.TempDir()).Switch("codex", "plain", nil, "")
	if err != nil {
		t.Fatalf("Switch(codex plain) error = %v", err)
	}
	if out3.BaseURL != "https://api.example.com/v1" || out3.BaseURLSource != baseURLSourceInferred {
		t.Fatalf("plain BaseURL/Source = %q / %q", out3.BaseURL, out3.BaseURLSource)
	}
	out4, err := NewSwitchManager(mgr3, "", t.TempDir()).Switch("claude-code", "plain", nil, "")
	if err != nil {
		t.Fatalf("Switch(claude-code plain) error = %v", err)
	}
	if out4.BaseURL != "https://api.example.com" || out4.BaseURLSource != baseURLSourceInferred {
		t.Fatalf("plain anthropic BaseURL/Source = %q / %q", out4.BaseURL, out4.BaseURLSource)
	}
}
