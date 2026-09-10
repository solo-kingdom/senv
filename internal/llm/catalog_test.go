package llm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validPayload = `{"prov1":{"id":"prov1","name":"P1","models":{"m1":{"id":"m1"}},"env":["P1_API_KEY"]}}`

func TestFetchValidPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validPayload))
	}))
	defer srv.Close()

	cat, err := Fetch(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cat.Version != catalogVersion {
		t.Fatalf("Version = %d, want %d", cat.Version, catalogVersion)
	}
	if cat.Source != srv.URL {
		t.Fatalf("Source = %q, want %q", cat.Source, srv.URL)
	}
	if _, err := cat.FetchedAtTime(); err != nil {
		t.Fatalf("FetchedAtTime() error = %v", err)
	}
	providers, models, err := cat.Counts()
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}
	if providers != 1 || models != 1 {
		t.Fatalf("Counts() = (%d, %d), want (1, 1)", providers, models)
	}
}

func TestFetchFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"server error", http.StatusInternalServerError, validPayload, "unexpected status"},
		{"invalid json", http.StatusOK, `{"prov1":`, "not an object"},
		{"no providers", http.StatusOK, `{}`, "no providers"},
		{"no models", http.StatusOK, `{"prov1":{"id":"prov1"}}`, "no models"},
		{"provider not object", http.StatusOK, `{"prov1":"text"}`, "is not an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := Fetch(srv.URL, srv.Client())
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Fetch() error = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestFetchOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, maxCatalogBytes+1))
	}))
	defer srv.Close()

	if _, err := Fetch(srv.URL, srv.Client()); err == nil {
		t.Fatal("Fetch() expected error for oversized body")
	}
}

func TestProviderModelIDs(t *testing.T) {
	cat, err := Parse([]byte(`{
		"p1": {"id":"p1","models":{"m2":{"id":"m2"},"m1":{"id":"m1"}}},
		"empty": {"id":"empty","models":{}}
	}`), "https://x.test", time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	ids, err := cat.ProviderModelIDs("p1")
	if err != nil {
		t.Fatalf("ProviderModelIDs() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != "m1" || ids[1] != "m2" {
		t.Fatalf("ids = %v, want [m1 m2]", ids)
	}
	if _, err := cat.ProviderModelIDs("missing"); err == nil || !strings.Contains(err.Error(), "no provider") {
		t.Fatalf("missing provider error = %v", err)
	}
	if _, err := cat.ProviderModelIDs("empty"); err == nil || !strings.Contains(err.Error(), "no models") {
		t.Fatalf("empty provider error = %v", err)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "models-dev.json")
	fetched := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cat, err := Parse([]byte(validPayload), "https://example.test/api.json", fetched)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := Save(path, cat); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Source != cat.Source {
		t.Fatalf("Source = %q, want %q", got.Source, cat.Source)
	}
	gotAt, err := got.FetchedAtTime()
	if err != nil || !gotAt.Equal(fetched) {
		t.Fatalf("FetchedAt = %v (err %v), want %v", got.FetchedAt, err, fetched)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != cacheFileMode {
		t.Fatalf("cache perm = %v, want %v", info.Mode().Perm(), cacheFileMode)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	if dirInfo.Mode().Perm() != cacheDirMode {
		t.Fatalf("cache dir perm = %v, want %v", dirInfo.Mode().Perm(), cacheDirMode)
	}
}

func TestSaveReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-dev.json")
	first, err := Parse([]byte(validPayload), "https://a.test", time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := Save(path, first); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	secondPayload := `{"p2":{"id":"p2","models":{"m":{"id":"m"}},"models2":{"n":{"id":"n"}}}}`
	second, err := Parse([]byte(secondPayload), "https://b.test", time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := Save(path, second); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Source != "https://b.test" {
		t.Fatalf("Source = %q, want replaced content", got.Source)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".models-dev-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-dev.json")

	if _, err := Load(path); !errors.Is(err, ErrCacheNotFound) {
		t.Fatalf("Load(missing) error = %v, want ErrCacheNotFound", err)
	}

	corrupt := map[string]struct {
		content string
		wantErr error
	}{
		"broken json": {`{"version":1,`, ErrCacheCorrupt},
		"bad version": {`{"version":99,"fetched_at":"2026-09-09T00:00:00Z","source":"s","providers":{}}`, ErrCacheCorrupt},
		"bad time":    {`{"version":1,"fetched_at":"yesterday","source":"s","providers":{"p":{"models":{"m":{}}}}}`, ErrCacheCorrupt},
		"bad payload": {`{"version":1,"fetched_at":"2026-09-09T00:00:00Z","source":"s","providers":{}}`, ErrCacheCorrupt},
	}
	for name, tc := range corrupt {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), cacheFileMode); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := Load(path); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
