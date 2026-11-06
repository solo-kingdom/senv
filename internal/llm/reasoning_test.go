package llm

import "testing"

func TestResolvedDefaultReasoning(t *testing.T) {
	cases := []struct {
		name string
		meta ModelMetadata
		want string
	}{
		{name: "explicit wins", meta: ModelMetadata{ReasoningEfforts: []string{"low", "high", "max"}, DefaultReasoning: "max"}, want: "max"},
		{name: "explicit none", meta: ModelMetadata{ReasoningEfforts: []string{"none", "high"}, DefaultReasoning: "none"}, want: "none"},
		{name: "second highest of three", meta: ModelMetadata{ReasoningEfforts: []string{"low", "high", "max"}}, want: "high"},
		{name: "second of high max", meta: ModelMetadata{ReasoningEfforts: []string{"high", "max"}}, want: "high"},
		{name: "second of low high", meta: ModelMetadata{ReasoningEfforts: []string{"low", "high"}}, want: "low"},
		{name: "only max", meta: ModelMetadata{ReasoningEfforts: []string{"max"}}, want: "max"},
		{name: "none and max", meta: ModelMetadata{ReasoningEfforts: []string{"none", "max"}}, want: "max"},
		{name: "unsorted", meta: ModelMetadata{ReasoningEfforts: []string{"max", "low", "high"}}, want: "high"},
		{name: "unknown ignored", meta: ModelMetadata{ReasoningEfforts: []string{"custom", "low", "max"}}, want: "low"},
		{name: "unknown only", meta: ModelMetadata{ReasoningEfforts: []string{"custom"}}, want: ""},
		{name: "adaptive", meta: ModelMetadata{Reasoning: true}, want: ""},
		{name: "off only", meta: ModelMetadata{ReasoningEfforts: []string{"off", "none"}}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvedDefaultReasoning(tc.meta); got != tc.want {
				t.Fatalf("resolvedDefaultReasoning() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPiThinkingLevelMap(t *testing.T) {
	got := piThinkingLevelMap(ModelMetadata{ReasoningEfforts: []string{"none", "low", "max"}})
	if got == nil {
		t.Fatal("map is nil")
	}
	if got["off"] != "none" || got["low"] != "low" || got["max"] != "max" {
		t.Fatalf("mapped values = %v", got)
	}
	for _, level := range []string{"minimal", "medium", "high", "xhigh"} {
		if got[level] != nil {
			t.Fatalf("%s = %v, want null", level, got[level])
		}
	}
	if piThinkingLevelMap(ModelMetadata{ReasoningEfforts: []string{"custom"}}) != nil {
		t.Fatal("unknown-only efforts must not produce an all-null map")
	}
	if piThinkingLevelMap(ModelMetadata{Reasoning: true}) != nil {
		t.Fatal("adaptive model must omit the map")
	}
}
