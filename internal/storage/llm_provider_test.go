package storage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validProviderEntry(alias string) *LLMProviderEntry {
	return &LLMProviderEntry{
		Alias:           alias,
		BaseURL:         "https://api.example.com",
		CredentialRef:   "text:llm-keys/" + alias,
		CatalogProvider: "p1",
		Models:          []string{"m1", "m2"},
		DefaultModel:    "m1",
		UpdatedAt:       time.Now().Truncate(time.Second).UTC(),
	}
}

func TestSaveAndLoadLLMProvider(t *testing.T) {
	mgr, _ := setupTestManager(t)
	entry := validProviderEntry("main")
	if err := mgr.SaveLLMProvider("main", entry, "test-password"); err != nil {
		t.Fatalf("SaveLLMProvider: %v", err)
	}
	got, err := mgr.LoadLLMProvider("main", "test-password")
	if err != nil {
		t.Fatalf("LoadLLMProvider: %v", err)
	}
	if !reflect.DeepEqual(got, entry) {
		t.Fatalf("loaded entry mismatch: got %+v, want %+v", got, entry)
	}
	if len(got.Models) != 2 {
		t.Fatalf("models = %v, want 2 entries", got.Models)
	}

	// Force-style overwrite replaces the entry.
	entry.DefaultModel = "m2"
	if err := mgr.SaveLLMProvider("main", entry, "test-password"); err != nil {
		t.Fatalf("overwrite SaveLLMProvider: %v", err)
	}
	got, err = mgr.LoadLLMProvider("main", "test-password")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.DefaultModel != "m2" {
		t.Fatalf("DefaultModel = %q, want m2", got.DefaultModel)
	}
}

func TestLLMProviderValidation(t *testing.T) {
	mgr, _ := setupTestManager(t)
	cases := []struct {
		name  string
		entry *LLMProviderEntry
		want  string
	}{
		{"bad alias", validProviderEntry("../a"), "invalid"},
		{"alias mismatch", func() *LLMProviderEntry {
			e := validProviderEntry("main")
			e.Alias = "other"
			return e
		}(), "does not match"},
		{"bad url", func() *LLMProviderEntry {
			e := validProviderEntry("main")
			e.BaseURL = "ftp://x"
			return e
		}(), "base URL"},
		{"missing credential ref", func() *LLMProviderEntry {
			e := validProviderEntry("main")
			e.CredentialRef = ""
			return e
		}(), "credential ref"},
		{"no models", func() *LLMProviderEntry {
			e := validProviderEntry("main")
			e.Models = nil
			return e
		}(), "no models"},
		{"default not in models", func() *LLMProviderEntry {
			e := validProviderEntry("main")
			e.DefaultModel = "nope"
			return e
		}(), "not in models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			saveName := tc.entry.Alias
			if tc.name == "alias mismatch" {
				saveName = "main"
			}
			if err := mgr.SaveLLMProvider(saveName, tc.entry, "test-password"); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SaveLLMProvider() error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestDeleteAndListLLMProviders(t *testing.T) {
	mgr, _ := setupTestManager(t)
	for _, alias := range []string{"b", "a"} {
		if err := mgr.SaveLLMProvider(alias, validProviderEntry(alias), "test-password"); err != nil {
			t.Fatalf("SaveLLMProvider(%s): %v", alias, err)
		}
	}
	names, err := mgr.ListLLMProviders()
	if err != nil {
		t.Fatalf("ListLLMProviders: %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("names = %v, want [a b]", names)
	}
	if err := mgr.DeleteLLMProvider("a"); err != nil {
		t.Fatalf("DeleteLLMProvider: %v", err)
	}
	names, err = mgr.ListLLMProviders()
	if err != nil {
		t.Fatalf("ListLLMProviders: %v", err)
	}
	if len(names) != 1 || names[0] != "b" {
		t.Fatalf("names after delete = %v, want [b]", names)
	}
	// Idempotent delete.
	if err := mgr.DeleteLLMProvider("a"); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

// TestLLMProviderAPIShape 覆盖 api_shape 的缺省、合法取值、非法取值与向后兼容。
func TestLLMProviderAPIShape(t *testing.T) {
	mgr, _ := setupTestManager(t)

	for _, shape := range LLMAPIShapes {
		alias := "shape-" + shape
		entry := validProviderEntry(alias)
		entry.APIShape = shape
		if err := mgr.SaveLLMProvider(alias, entry, "test-password"); err != nil {
			t.Fatalf("SaveLLMProvider(%s): %v", shape, err)
		}
		got, err := mgr.LoadLLMProvider(alias, "test-password")
		if err != nil {
			t.Fatalf("LoadLLMProvider(%s): %v", shape, err)
		}
		if got.APIShape != shape {
			t.Fatalf("APIShape = %q, want %q", got.APIShape, shape)
		}
	}

	bad := validProviderEntry("bad")
	bad.APIShape = "openai"
	if err := mgr.SaveLLMProvider("bad", bad, "test-password"); err == nil ||
		!strings.Contains(err.Error(), "api_shape") {
		t.Fatalf("invalid api_shape error = %v", err)
	}

	// 向后兼容：存量档案的 JSON 没有该字段，反序列化为空且仍然合法。
	var legacy LLMProviderEntry
	raw := `{"alias":"legacy","base_url":"https://api.example.com","credential_ref":"text:llm-keys/legacy","models":["m1"]}`
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatalf("unmarshal legacy entry: %v", err)
	}
	if legacy.APIShape != "" {
		t.Fatalf("legacy APIShape = %q, want empty", legacy.APIShape)
	}
	if err := legacy.ValidateLLMProvider(); err != nil {
		t.Fatalf("legacy entry rejected: %v", err)
	}
	// 空形态也不会被写入 JSON。
	encoded, err := json.Marshal(validProviderEntry("main"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "api_shape") {
		t.Fatalf("empty api_shape should be omitted: %s", encoded)
	}
}
