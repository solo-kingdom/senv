package llm

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func newTestProviderManager(t *testing.T) (*ProviderManager, *storage.Manager, string) {
	t.Helper()
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	catalogPath := filepath.Join(base, "cache", "models-dev.json")
	return NewProviderManager(store, "test-password"), store, catalogPath
}

func writeTestCatalog(t *testing.T, path, payload string, fetchedAt time.Time) {
	t.Helper()
	cat, err := Parse([]byte(payload), "https://x.test", fetchedAt)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := Save(path, cat); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

const catalogPayload = `{
	"p1": {"id":"p1","name":"P1","models":{"m2":{"id":"m2"},"m1":{"id":"m1"}}}
}`

func TestParseModelContexts(t *testing.T) {
	got, err := ParseModelContexts([]string{"m1=1000000", "m2=200000"})
	if err != nil {
		t.Fatalf("ParseModelContexts() error = %v", err)
	}
	if got["m1"] != 1_000_000 || got["m2"] != 200000 {
		t.Fatalf("contexts = %v", got)
	}
	for _, spec := range []string{"m1", "m1=nope", "m1=0", "m1=-1", "=1"} {
		if _, err := ParseModelContexts([]string{spec}); err == nil {
			t.Fatalf("ParseModelContexts(%q) unexpectedly succeeded", spec)
		}
	}
	if _, err := ParseModelContexts([]string{"m1=1000", "m1=2000"}); err == nil {
		t.Fatal("duplicate model context unexpectedly succeeded")
	}
}

func TestParseModelOutputs(t *testing.T) {
	got, err := ParseModelOutputs([]string{"m1=32000", " m2 = 64000 "})
	if err != nil {
		t.Fatalf("ParseModelOutputs() error = %v", err)
	}
	if got["m1"] != 32000 || got["m2"] != 64000 {
		t.Fatalf("outputs = %v", got)
	}
	if got, err := ParseModelOutputs(nil); err != nil || got != nil {
		t.Fatalf("ParseModelOutputs(nil) = %v, %v", got, err)
	}
	for _, spec := range []string{"m1", "m1=nope", "m1=0", "m1=-5", "=100"} {
		if _, err := ParseModelOutputs([]string{spec}); err == nil {
			t.Fatalf("ParseModelOutputs(%q) unexpectedly succeeded", spec)
		}
	}
	if _, err := ParseModelOutputs([]string{"m1=100", "m1=200"}); err == nil {
		t.Fatal("duplicate model output unexpectedly succeeded")
	}
}

func TestParseModelReasoning(t *testing.T) {
	got, err := ParseModelReasoning([]string{"m1=low;high", " m2 = medium "})
	if err != nil {
		t.Fatalf("ParseModelReasoning() error = %v", err)
	}
	if strings.Join(got["m1"], ",") != "low,high" || strings.Join(got["m2"], ",") != "medium" {
		t.Fatalf("reasoning = %v", got)
	}
	if got, err := ParseModelReasoning(nil); err != nil || got != nil {
		t.Fatalf("ParseModelReasoning(nil) = %v, %v", got, err)
	}
	for _, spec := range []string{"m1", "m1=", "m1=;", "=low"} {
		if _, err := ParseModelReasoning([]string{spec}); err == nil {
			t.Fatalf("ParseModelReasoning(%q) unexpectedly succeeded", spec)
		}
	}
	if _, err := ParseModelReasoning([]string{"m1=low", "m1=high"}); err == nil {
		t.Fatal("duplicate model reasoning unexpectedly succeeded")
	}
}

func TestAddProviderStoresOutputAndReasoning(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models:                []string{"custom-1"},
		ModelContexts:         map[string]int{"custom-1": 200_000},
		ModelOutputs:          map[string]int{"custom-1": 32_000},
		ModelReasoning:        map[string][]string{"custom-1": {"low", "high"}},
		ModelDefaultReasoning: map[string]string{"custom-1": "high"},
		RequireModelMetadata:  true,
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	info := res.Entry.ModelInfo["custom-1"]
	if info.ContextWindow != 200_000 || info.OutputLimit != 32_000 {
		t.Fatalf("model info = %+v", info)
	}
	if strings.Join(info.ReasoningEfforts, ",") != "low,high" {
		t.Fatalf("reasoning efforts = %v", info.ReasoningEfforts)
	}
	if info.DefaultReasoning != "high" {
		t.Fatalf("default reasoning = %q, want high", info.DefaultReasoning)
	}

	// 不在模型集内的模型要被拒绝。
	_, err = mgr.AddProvider(AddProviderOptions{
		Alias: "other", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 1},
		ModelOutputs: map[string]int{"ghost": 10},
		Force:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "--model-output") {
		t.Fatalf("AddProvider() error = %v, want --model-output membership error", err)
	}
	_, err = mgr.AddProvider(AddProviderOptions{
		Alias: "other", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 1},
		ModelReasoning: map[string][]string{"ghost": {"low"}},
		Force:          true,
	})
	if err == nil || !strings.Contains(err.Error(), "--model-reasoning") {
		t.Fatalf("AddProvider() error = %v, want --model-reasoning membership error", err)
	}
	_, err = mgr.AddProvider(AddProviderOptions{
		Alias: "other", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 1},
		ModelDefaultReasoning: map[string]string{"ghost": "high"},
		Force:                 true,
	})
	if err == nil || !strings.Contains(err.Error(), "--model-default-reasoning") {
		t.Fatalf("AddProvider() error = %v, want --model-default-reasoning membership error", err)
	}
}

func TestEditProviderAddsOutputAndReasoningWithoutChangingModels(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 200_000},
		RequireModelMetadata: true,
	}); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	res, err := mgr.EditProvider(EditProviderOptions{
		Alias:                 "main",
		ModelOutputs:          map[string]int{"custom-1": 8_000},
		ModelReasoning:        map[string][]string{"custom-1": {"medium"}},
		ModelDefaultReasoning: map[string]string{"custom-1": "medium"},
	})
	if err != nil {
		t.Fatalf("EditProvider() error = %v", err)
	}
	if strings.Join(res.Entry.Models, ",") != "custom-1" {
		t.Fatalf("models = %v, want unchanged", res.Entry.Models)
	}
	info := res.Entry.ModelInfo["custom-1"]
	if info.OutputLimit != 8_000 || strings.Join(info.ReasoningEfforts, ",") != "medium" || info.ContextWindow != 200_000 {
		t.Fatalf("model info = %+v, want merged metadata", info)
	}
	if info.DefaultReasoning != "medium" {
		t.Fatalf("default reasoning = %q, want medium", info.DefaultReasoning)
	}
}

func TestAddProviderWithCatalogAndCustomModels(t *testing.T) {
	mgr, store, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, catalogPayload, time.Now())

	res, err := mgr.AddProvider(AddProviderOptions{
		Alias:           "main",
		BaseURL:         "https://api.example.com/v1",
		APIKey:          "sk-secret",
		CatalogPath:     catalogPath,
		CatalogProvider: "p1",
		Models:          []string{"custom-1"},
		DefaultModel:    "custom-1",
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}
	want := []string{"custom-1", "m1", "m2"}
	if strings.Join(res.Entry.Models, ",") != strings.Join(want, ",") {
		t.Fatalf("models = %v, want %v", res.Entry.Models, want)
	}
	if res.Entry.CredentialRef != "text:llm-keys/main" {
		t.Fatalf("credential ref = %q", res.Entry.CredentialRef)
	}

	// 凭据已存入 vault 保留组且可解密读回。
	tm := text.NewManager(store, "test-password")
	got, err := tm.Get(LLMKeysGroup, "main")
	if err != nil || got != "sk-secret" {
		t.Fatalf("credential Get() = %q, %v", got, err)
	}
}

func TestAddProviderRequiresContextWindow(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	_, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, RequireModelMetadata: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--model-context") {
		t.Fatalf("AddProvider() error = %v, want explicit context guidance", err)
	}
}

func TestAddProviderStoresContextWindow(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 1_000_000},
		RequireModelMetadata: true,
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if got := res.Entry.ModelInfo["custom-1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("context window = %d, want 1000000", got)
	}
}

func TestAddProviderReadsCatalogContextWindow(t *testing.T) {
	mgr, _, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, `{
		"p1": {"id":"p1","models":{"m1":{"id":"m1","limit":{"context":200000}}}}
	}`, time.Now())
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		CatalogPath: catalogPath, CatalogProvider: "p1", RequireModelMetadata: true,
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if got := res.Entry.ModelInfo["m1"].ContextWindow; got != 200000 {
		t.Fatalf("context window = %d, want 200000", got)
	}
}

func TestAddProviderExplicitContextOverridesCatalog(t *testing.T) {
	mgr, _, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, `{
		"p1": {"id":"p1","models":{"m1":{"id":"m1","limit":{"context":128000}}}}
	}`, time.Now())
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		CatalogPath: catalogPath, CatalogProvider: "p1",
		ModelContexts: map[string]int{"m1": 1_000_000}, RequireModelMetadata: true,
	})
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if got := res.Entry.ModelInfo["m1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("context window = %d, want explicit 1000000", got)
	}
}

func TestEditProviderAddsContextWindowWithoutChangingModels(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"},
	}); err != nil {
		t.Fatalf("seed legacy provider: %v", err)
	}
	res, err := mgr.EditProvider(EditProviderOptions{
		Alias: "main", ModelContexts: map[string]int{"custom-1": 1_000_000},
		RequireModelMetadata: true,
	})
	if err != nil {
		t.Fatalf("EditProvider() error = %v", err)
	}
	if strings.Join(res.Entry.Models, ",") != "custom-1" {
		t.Fatalf("models = %v, want unchanged", res.Entry.Models)
	}
	if got := res.Entry.ModelInfo["custom-1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("context window = %d, want 1000000", got)
	}
}

func TestAddProviderForcePreservesModelMetadata(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"custom-1"}, ModelContexts: map[string]int{"custom-1": 1_000_000},
		RequireModelMetadata: true,
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	res, err := mgr.AddProvider(AddProviderOptions{
		Alias: "main", BaseURL: "https://new.example.com", Models: []string{"custom-1"},
		RequireModelMetadata: true, Force: true,
	})
	if err != nil {
		t.Fatalf("force AddProvider() error = %v", err)
	}
	if got := res.Entry.ModelInfo["custom-1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("context window = %d, want preserved 1000000", got)
	}
}

func TestAddProviderFailures(t *testing.T) {
	t.Run("catalog cache missing", func(t *testing.T) {
		mgr, _, catalogPath := newTestProviderManager(t)
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a/v1", CatalogPath: catalogPath,
			CatalogProvider: "p1", APIKey: "k",
		})
		if err == nil || !strings.Contains(err.Error(), "senv ai refresh") {
			t.Fatalf("error = %v, want refresh hint", err)
		}
	})
	t.Run("catalog provider missing", func(t *testing.T) {
		mgr, _, catalogPath := newTestProviderManager(t)
		writeTestCatalog(t, catalogPath, catalogPayload, time.Now())
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", CatalogPath: catalogPath,
			CatalogProvider: "p9", APIKey: "k",
		})
		if err == nil || !strings.Contains(err.Error(), "no provider") {
			t.Fatalf("error = %v, want no provider", err)
		}
	})
	t.Run("empty model set", func(t *testing.T) {
		mgr, _, catalogPath := newTestProviderManager(t)
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", CatalogPath: catalogPath, APIKey: "k",
		})
		if err == nil || !strings.Contains(err.Error(), "model set is empty") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("default model not in set", func(t *testing.T) {
		mgr, _, catalogPath := newTestProviderManager(t)
		writeTestCatalog(t, catalogPath, catalogPayload, time.Now())
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", CatalogPath: catalogPath,
			CatalogProvider: "p1", APIKey: "k", DefaultModel: "nope",
		})
		if err == nil || !strings.Contains(err.Error(), "not in the model set") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("mutually exclusive credential flags", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a",
			APIKey: "k", KeyRef: "env:g/K",
			Models: []string{"m"},
		})
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing credential", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", Models: []string{"m"},
		})
		if err == nil || !strings.Contains(err.Error(), "--api-key-stdin") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("stale catalog warns but succeeds", func(t *testing.T) {
		mgr, _, catalogPath := newTestProviderManager(t)
		writeTestCatalog(t, catalogPath, catalogPayload, time.Now().Add(-10*24*time.Hour))
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a/v1", CatalogPath: catalogPath,
			CatalogProvider: "p1", APIKey: "k",
		})
		if err != nil {
			t.Fatalf("AddProvider() error = %v", err)
		}
		if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "senv ai refresh") {
			t.Fatalf("warnings = %v, want only the stale hint (use an already normalized base URL here)", res.Warnings)
		}
	})
}

func TestAddProviderDuplicateAndForce(t *testing.T) {
	mgr, _, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, catalogPayload, time.Now())
	opts := AddProviderOptions{
		Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m"},
	}
	if _, err := mgr.AddProvider(opts); err != nil {
		t.Fatalf("first add: %v", err)
	}
	first, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := mgr.AddProvider(opts); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate error = %v", err)
	}
	opts.Force = true
	opts.DefaultModel = "m"
	if _, err := mgr.AddProvider(opts); err != nil {
		t.Fatalf("force add: %v", err)
	}
	second, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("get after force: %v", err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("CreatedAt changed on force: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestRemoveProviderCredentialDisposition(t *testing.T) {
	t.Run("owned credential removed together", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add: %v", err)
		}
		removed, err := mgr.RemoveProvider("main")
		if err != nil || !removed.CredentialRemoved {
			t.Fatalf("RemoveProvider() = %v, %v; want removed", removed, err)
		}
		tm := text.NewManager(store, "test-password")
		if _, err := tm.Get(LLMKeysGroup, "main"); err == nil {
			t.Fatal("credential still readable after remove")
		}
	})
	t.Run("external ref kept", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "ext", BaseURL: "https://a", KeyRef: "env:openai/KEY", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add: %v", err)
		}
		removed, err := mgr.RemoveProvider("ext")
		if err != nil || removed.CredentialRemoved {
			t.Fatalf("RemoveProvider() = %v, %v; want kept", removed, err)
		}
	})
	t.Run("missing alias errors", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		if _, err := mgr.RemoveProvider("ghost"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestListProvidersSorted(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	for _, alias := range []string{"b", "a"} {
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: alias, BaseURL: "https://a", APIKey: "k", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add %s: %v", alias, err)
		}
	}
	entries, err := mgr.ListProviders()
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(entries) != 2 || entries[0].Alias != "a" || entries[1].Alias != "b" {
		t.Fatalf("aliases = %v, want [a b]", []string{entries[0].Alias, entries[1].Alias})
	}
}

func TestParseModelDefaultReasoning(t *testing.T) {
	got, err := ParseModelDefaultReasoning([]string{"m1=high", " m2 = low "})
	if err != nil {
		t.Fatalf("ParseModelDefaultReasoning() error = %v", err)
	}
	if got["m1"] != "high" || got["m2"] != "low" {
		t.Fatalf("defaults = %v", got)
	}
	if got, err := ParseModelDefaultReasoning(nil); err != nil || got != nil {
		t.Fatalf("ParseModelDefaultReasoning(nil) = %v, %v", got, err)
	}
	for _, spec := range []string{"m1", "m1=", "=high"} {
		if _, err := ParseModelDefaultReasoning([]string{spec}); err == nil {
			t.Fatalf("ParseModelDefaultReasoning(%q) unexpectedly succeeded", spec)
		}
	}
	if _, err := ParseModelDefaultReasoning([]string{"m1=high", "m1=low"}); err == nil {
		t.Fatal("duplicate default reasoning unexpectedly succeeded")
	}
}

func TestParseModelModalities(t *testing.T) {
	got, err := ParseModelModalities([]string{"m1=text,image", "m2=text;audio"})
	if err != nil {
		t.Fatalf("ParseModelModalities() error = %v", err)
	}
	if strings.Join(got["m1"], ",") != "text,image" || strings.Join(got["m2"], ",") != "text,audio" {
		t.Fatalf("modalities = %v", got)
	}
	if _, err := ParseModelModalities([]string{"m1=nope"}); err == nil {
		t.Fatal("invalid modality unexpectedly succeeded")
	}
	if _, err := ParseModelModalities([]string{"m1=text", "m1=image"}); err == nil {
		t.Fatal("duplicate modalities unexpectedly succeeded")
	}
}

func TestAssembleDefaultReasoningAndModalities(t *testing.T) {
	mgr, _, catalogPath := newTestProviderManager(t)
	writeTestCatalog(t, catalogPath, `{
		"p1": {"id":"p1","models":{
			"m1": {"id":"m1","limit":{"context":128000},
				"reasoning_options":[{"type":"effort","values":["low","high"]}],
				"modalities":{"input":["text","image"]}},
			"m2": {"id":"m2","limit":{"context":200000}}
		}}
	}`, time.Now())

	t.Run("collection fills models with efforts only", func(t *testing.T) {
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "col", BaseURL: "https://api.example.com", APIKey: "k",
			CatalogPath: catalogPath, CatalogProvider: "p1",
			DefaultReasoning:     "high",
			RequireModelMetadata: true,
		})
		if err != nil {
			t.Fatalf("AddProvider() error = %v", err)
		}
		if got := res.Entry.ModelInfo["m1"].DefaultReasoning; got != "high" {
			t.Fatalf("m1 default = %q, want collection high", got)
		}
		if got := res.Entry.ModelInfo["m2"].DefaultReasoning; got != "" {
			t.Fatalf("m2 default = %q, want empty (no efforts)", got)
		}
		if strings.Join(res.Entry.ModelInfo["m1"].InputModalities, ",") != "text,image" {
			t.Fatalf("m1 modalities = %v, want catalog input", res.Entry.ModelInfo["m1"].InputModalities)
		}
	})

	t.Run("missing default rejected", func(t *testing.T) {
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "miss", BaseURL: "https://api.example.com", APIKey: "k",
			Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
			ModelReasoning:       map[string][]string{"m1": {"low", "high"}},
			RequireModelMetadata: true,
		})
		if err == nil || !strings.Contains(err.Error(), "default reasoning") {
			t.Fatalf("error = %v, want missing default", err)
		}
	})

	t.Run("default out of efforts rejected", func(t *testing.T) {
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "bad", BaseURL: "https://api.example.com", APIKey: "k",
			Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
			ModelReasoning:        map[string][]string{"m1": {"low", "high"}},
			ModelDefaultReasoning: map[string]string{"m1": "xhigh"},
			RequireModelMetadata:  true,
		})
		if err == nil || !strings.Contains(err.Error(), "not in reasoning efforts") {
			t.Fatalf("error = %v, want membership error", err)
		}
	})

	t.Run("no efforts does not require default", func(t *testing.T) {
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "plain", BaseURL: "https://api.example.com", APIKey: "k",
			Models:               []string{"custom-1"},
			ModelContexts:        map[string]int{"custom-1": 1_000_000},
			RequireModelMetadata: true,
		})
		if err != nil {
			t.Fatalf("AddProvider() error = %v", err)
		}
		if got := res.Entry.ModelInfo["custom-1"].DefaultReasoning; got != "" {
			t.Fatalf("default = %q, want empty", got)
		}
	})

	t.Run("does not infer first effort", func(t *testing.T) {
		_, err := mgr.AddProvider(AddProviderOptions{
			Alias: "infer", BaseURL: "https://api.example.com", APIKey: "k",
			Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
			ModelReasoning:       map[string][]string{"m1": {"low", "high"}},
			RequireModelMetadata: true,
		})
		if err == nil {
			t.Fatal("inferred first effort unexpectedly succeeded")
		}
	})

	t.Run("explicit modalities override catalog", func(t *testing.T) {
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "mods", BaseURL: "https://api.example.com", APIKey: "k",
			CatalogPath: catalogPath, CatalogProvider: "p1",
			DefaultReasoning:     "high",
			ModelModalities:      map[string][]string{"m1": {"text", "video"}},
			RequireModelMetadata: true, Force: true,
		})
		if err != nil {
			t.Fatalf("AddProvider() error = %v", err)
		}
		if strings.Join(res.Entry.ModelInfo["m1"].InputModalities, ",") != "text,video" {
			t.Fatalf("modalities = %v, want explicit", res.Entry.ModelInfo["m1"].InputModalities)
		}
	})

	t.Run("edit backfills default without changing models", func(t *testing.T) {
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "legacy", BaseURL: "https://api.example.com", APIKey: "k",
			Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
		})
		if err != nil {
			t.Fatalf("seed legacy: %v", err)
		}
		entry := res.Entry
		info := entry.ModelInfo["m1"]
		info.ReasoningEfforts = []string{"low", "high"}
		entry.ModelInfo["m1"] = info
		if err := mgr.storage.SaveLLMProvider("legacy", entry, "test-password"); err != nil {
			t.Fatalf("seed efforts without default: %v", err)
		}
		got, err := mgr.EditProvider(EditProviderOptions{
			Alias:                 "legacy",
			ModelDefaultReasoning: map[string]string{"m1": "high"},
			RequireModelMetadata:  true,
		})
		if err != nil {
			t.Fatalf("EditProvider() error = %v", err)
		}
		if strings.Join(got.Entry.Models, ",") != "m1" {
			t.Fatalf("models = %v, want unchanged", got.Entry.Models)
		}
		info = got.Entry.ModelInfo["m1"]
		if info.DefaultReasoning != "high" || strings.Join(info.ReasoningEfforts, ",") != "low,high" {
			t.Fatalf("model info = %+v", info)
		}
	})
}
