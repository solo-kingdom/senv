package llm

import "testing"

func TestBaseURLForFamily(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		openAI    string
		anthropic string
	}{
		{"无版本段", "https://api.example.com",
			"https://api.example.com/v1", "https://api.example.com"},
		{"仅尾斜杠", "https://api.example.com/",
			"https://api.example.com/v1", "https://api.example.com"},
		{"已带版本段", "https://api.example.com/v1",
			"https://api.example.com/v1", "https://api.example.com"},
		{"版本段加尾斜杠", "https://api.example.com/v1/",
			"https://api.example.com/v1", "https://api.example.com"},
		{"带路径前缀", "https://api.example.com/api/llm/v1",
			"https://api.example.com/api/llm/v1", "https://api.example.com/api/llm"},
		{"纯数字版本段已归一（智谱 /v4）", "https://open.bigmodel.cn/api/paas/v4",
			"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4"},
		{"Anthropic 风格的路径前缀", "https://api.example.com/anthropic",
			"https://api.example.com/anthropic/v1", "https://api.example.com/anthropic"},
		{"非 v1 版本段不被猜测", "https://api.example.com/v1beta",
			"https://api.example.com/v1beta/v1", "https://api.example.com/v1beta"},
		{"本地 HTTP 端点", "http://127.0.0.1:11434/v1",
			"http://127.0.0.1:11434/v1", "http://127.0.0.1:11434"},
		{"保留 query", "https://api.example.com/v1?key=abc",
			"https://api.example.com/v1?key=abc", "https://api.example.com?key=abc"},
		{"保留 fragment", "https://api.example.com#frag",
			"https://api.example.com/v1#frag", "https://api.example.com#frag"},
		{"中间重复斜杠不动", "https://api.example.com//v1",
			"https://api.example.com//v1", "https://api.example.com"},
		{"userinfo 原样保留交由校验拒绝", "https://user:pass@api.example.com",
			"https://user:pass@api.example.com/v1", "https://user:pass@api.example.com"},
		{"解析失败原样返回", "not a url", "not a url", "not a url"},
		{"缺少 host 原样返回", "/v1", "/v1", "/v1"},
		{"空值原样返回", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseURLForFamily(tc.raw, ProtocolOpenAICompatible); got != tc.openAI {
				t.Errorf("openai(%q) = %q, want %q", tc.raw, got, tc.openAI)
			}
			if got := baseURLForFamily(tc.raw, ProtocolAnthropic); got != tc.anthropic {
				t.Errorf("anthropic(%q) = %q, want %q", tc.raw, got, tc.anthropic)
			}
		})
	}
}

func TestBaseURLForFamilyIdempotent(t *testing.T) {
	for _, raw := range []string{
		"https://api.example.com",
		"https://api.example.com/v1",
		"https://api.example.com/v1/",
		"https://api.example.com/api/llm",
		"https://api.example.com/api/llm/v1",
		"https://open.bigmodel.cn/api/paas/v4",
		"https://api.example.com/v1?key=abc",
		"https://api.example.com//v1",
		"https://api.example.com/anthropic",
		"https://api.example.com//v1",
	} {
		for _, family := range []ProtocolFamily{ProtocolOpenAICompatible, ProtocolAnthropic} {
			once := baseURLForFamily(raw, family)
			if twice := baseURLForFamily(once, family); twice != once {
				t.Errorf("family %d not idempotent for %q: %q then %q", family, raw, once, twice)
			}
		}
	}
}

func TestNormalizeAnthropicShapeURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"普通前缀原样", "https://gw.example.com/api/anthropic", "https://gw.example.com/api/anthropic"},
		{"尾斜杠收敛", "https://gw.example.com/api/anthropic/", "https://gw.example.com/api/anthropic"},
		{"多重尾斜杠收敛", "https://gw.example.com/api/anthropic//", "https://gw.example.com/api/anthropic"},
		{"带 /v1 原样保留", "https://gw.example.com/api/anthropic/v1", "https://gw.example.com/api/anthropic/v1"},
		{"空白收敛", "  https://gw.example.com/api/anthropic  ", "https://gw.example.com/api/anthropic"},
		{"空值", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeAnthropicShapeURL(tc.raw); got != tc.want {
				t.Errorf("normalizeAnthropicShapeURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
