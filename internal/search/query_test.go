package search

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateQueryRejectsEmpty(t *testing.T) {
	for _, q := range []string{"", "   ", "\t"} {
		if err := ValidateQuery(q); err == nil {
			t.Errorf("ValidateQuery(%q) = nil, want error", q)
		}
	}
	if err := ValidateQuery("litellm"); err != nil {
		t.Fatalf("ValidateQuery(litellm): %v", err)
	}
}

func TestMatchesDescriptionAndNotValue(t *testing.T) {
	items := []Item{
		{
			Kind: KindEnv, Group: "svc", Key: "X",
			Description: "payment gateway token",
			match:       JoinMatch("svc", "X", "payment gateway token"),
		},
		{
			Kind: KindEnv, Group: "svc", Key: "Y",
			Description: "other",
			match:       JoinMatch("svc", "Y", "other"),
			// value would be "payment-secret" but is not in match
		},
	}
	res, err := Run(items, "payment", nil, DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Key != "X" {
		t.Fatalf("hits = %+v, want only X", res.Hits)
	}
	if Matches(items[1].MatchText(), "payment-secret") {
		t.Fatal("value-only string must not match metadata haystack")
	}
}

func TestMultiTokenAND(t *testing.T) {
	items := []Item{
		{Kind: KindEnv, Key: "LITELLM_UI_PASSWORD", match: "svc LITELLM_UI_PASSWORD LiteLLM"},
		{Kind: KindEnv, Key: "OTHER", match: "svc OTHER lite only"},
	}
	res, err := Run(items, "lite password", nil, DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Key != "LITELLM_UI_PASSWORD" {
		t.Fatalf("hits = %+v", res.Hits)
	}
}

func TestLimitTruncates(t *testing.T) {
	var items []Item
	for i := 0; i < 5; i++ {
		key := string(rune('A' + i))
		items = append(items, Item{Kind: KindEnv, Key: key, match: "shared " + key})
	}
	res, err := Run(items, "shared", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.Total != 5 || len(res.Hits) != 2 {
		t.Fatalf("truncated=%v total=%d hits=%d", res.Truncated, res.Total, len(res.Hits))
	}
}

func TestResultJSONHasNoValue(t *testing.T) {
	res, err := Run([]Item{{
		Kind: KindEnv, Group: "g", Key: "K", Description: "d", Preview: "***",
		match: "g K d",
	}}, "K", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"value"`) {
		t.Fatalf("JSON must not contain value key: %s", raw)
	}
	if strings.Contains(string(raw), "match") {
		t.Fatalf("JSON must not leak match field: %s", raw)
	}
}

func TestRankExactBeforeSubstring(t *testing.T) {
	items := []Item{
		{Kind: KindEnv, Key: "API_KEY_EXTRA", match: "API_KEY_EXTRA"},
		{Kind: KindEnv, Key: "API_KEY", match: "API_KEY"},
	}
	res, err := Run(items, "API_KEY", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 2 || res.Hits[0].Key != "API_KEY" {
		t.Fatalf("want exact first, got %+v", res.Hits)
	}
}

func TestParseKinds(t *testing.T) {
	kinds, err := ParseKinds([]string{"env,text", "ssh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 3 {
		t.Fatalf("kinds = %v", kinds)
	}
	if _, err := ParseKinds([]string{"nope"}); err == nil {
		t.Fatal("want unknown kind error")
	}
}

func TestEmptyQueryMatchesAllForTUI(t *testing.T) {
	if !Matches("anything", "") {
		t.Fatal("empty query should match for TUI browse")
	}
	if !Matches("anything", "   ") {
		t.Fatal("whitespace query should match for TUI browse")
	}
}
