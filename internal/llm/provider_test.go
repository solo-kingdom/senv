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
