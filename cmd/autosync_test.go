package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func withTestPaths(t *testing.T, configPath, wantedDataPath string) {
	t.Helper()
	oldConfig, oldData := configPathFn, dataPath
	configPathFn = func() string { return configPath }
	dataPath = wantedDataPath
	t.Cleanup(func() {
		configPathFn = oldConfig
		dataPath = oldData
	})
}

func TestAutoSyncNoOpForGitAndDisabledServer(t *testing.T) {
	configPath, dataPath := t.TempDir(), t.TempDir()
	withTestPaths(t, configPath, dataPath)
	store := storage.NewManager(configPath, dataPath)
	settings := storage.NewSettings()
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if sp, err := getAutoSyncServerProvider(); err != nil || sp != nil {
		t.Fatalf("git provider: sp=%v err=%v, want nil/nil", sp, err)
	}

	disabled := false
	settings.Provider = storage.ProviderConfig{
		Type: provider.TypeServer, Address: "https://example.test", Token: "tok", AutoSync: &disabled,
	}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if sp, err := getAutoSyncServerProvider(); err != nil || sp != nil {
		t.Fatalf("disabled server: sp=%v err=%v, want nil/nil", sp, err)
	}
	if _, err := os.Stat(filepath.Join(dataPath, ".senv-sync.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled server touched sync lock: %v", err)
	}
}

func TestAutoPushWarningsDoNotChangeExitStatus(t *testing.T) {
	var out bytes.Buffer
	printAutoPushWarning(&out, 2, errors.New("connect refused"))
	if !strings.Contains(out.String(), "⚠ 2 条待推送") || !strings.Contains(out.String(), "自动重试") {
		t.Fatalf("network warning = %q", out.String())
	}

	out.Reset()
	conflict := &provider.SyncConflictError{Conflicts: []provider.Conflict{{
		Kind: "env", Grp: "prod", Key: "API_KEY", CurrentRevision: 3,
	}}}
	printAutoPushWarning(&out, 1, conflict)
	if !strings.Contains(out.String(), "1 条推送冲突") || !strings.Contains(out.String(), "env/prod/API_KEY") || !strings.Contains(out.String(), "senv sync") {
		t.Fatalf("conflict warning = %q", out.String())
	}
}

type fakeAutoSyncServer struct {
	mu       sync.Mutex
	metadata []byte
	entries  map[string]provider.Entry
	revision int64
	pulls    int
	pushes   int
}

func (s *fakeAutoSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/metadata"):
		if s.metadata == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string][]byte{"blob": s.metadata})
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/metadata"):
		var body struct {
			Blob []byte `json:"blob"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.metadata = body.Blob
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/entries"):
		s.pulls++
		since, _ := strconv64(r.URL.Query().Get("since"))
		entries := make([]provider.Entry, 0)
		for _, entry := range s.entries {
			if entry.Revision > since {
				entries = append(entries, entry)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries, "latest_revision": s.revision})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/entries"):
		s.pushes++
		var body struct {
			Entries []provider.Entry `json:"entries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		acks := make([]map[string]any, 0, len(body.Entries))
		for _, entry := range body.Entries {
			id := entry.Kind + "\x00" + entry.Grp + "\x00" + entry.Key
			current, exists := s.entries[id]
			if exists && current.Revision != entry.BaseRevision {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"conflicts": []map[string]any{{
					"kind": entry.Kind, "grp": entry.Grp, "key": entry.Key, "current_revision": current.Revision,
				}}})
				return
			}
			s.revision++
			entry.Revision = s.revision
			s.entries[id] = entry
			acks = append(acks, map[string]any{
				"kind": entry.Kind, "grp": entry.Grp, "key": entry.Key, "revision": entry.Revision,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revisions": acks, "latest_revision": s.revision})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func strconv64(value string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(value, "%d", &n)
	return n, err
}

func TestMCPReadToolsUseThrottledAutoPull(t *testing.T) {
	t.Setenv("SENV_ALLOW_INSECURE_HTTP", "1")
	password := "auto-sync-password"
	fake := &fakeAutoSyncServer{entries: map[string]provider.Entry{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	configA, dataA := t.TempDir(), t.TempDir()
	storeA := storage.NewManager(configA, dataA)
	if err := storeA.Initialize(password); err != nil {
		t.Fatal(err)
	}
	if err := env.NewManager(storeA, password).Set("default", "API_KEY", "remote-value"); err != nil {
		t.Fatal(err)
	}
	pA := provider.NewServerProvider(server.URL, "test-token", configA, dataA, "main")
	if _, err := pA.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	configB, dataB := t.TempDir(), t.TempDir()
	pB := provider.NewServerProvider(server.URL, "test-token", configB, dataB, "main")
	if err := pB.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// Bootstrap establishes the throttle timestamp. Reset only that field to the
	// legacy zero value so this test observes one window-expired pull.
	statePath := filepath.Join(dataB, ".senv-sync-state.json")
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["last_pull_at"] = 1
	stateData, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(statePath); err != nil || !strings.Contains(string(raw), `"last_pull_at":1`) {
		t.Fatalf("state rewrite: err=%v data=%s", err, raw)
	}

	withTestPaths(t, configB, dataB)
	storeB := storage.NewManager(configB, dataB)
	if got := storeB.GetDataPath(); got != dataB {
		t.Fatalf("test data path = %q, want %q", got, dataB)
	}
	if got := getStorage().GetDataPath(); got != dataB {
		t.Fatalf("global data path = %q, want %q", got, dataB)
	}
	settings := storage.NewSettings()
	settings.Provider = storage.ProviderConfig{
		Type: provider.TypeServer, Address: server.URL, Token: "test-token", Vault: "main",
		SyncThrottle: "1h",
	}
	if err := storeB.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		t.Fatalf("auto sync provider: %v, %v", sp, err)
	}
	if got := sp.SyncThrottleWindow(); got != time.Hour {
		t.Fatalf("throttle = %s, want 1h", got)
	}

	fake.mu.Lock()
	pullsBefore := fake.pulls
	fake.mu.Unlock()
	m := &managers{env: env.NewManager(storeB, password), autoPull: newAutoPuller(&cobra.Command{})}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		result, _, err := m.envGet(ctx, nil, envGetInput{Key: "API_KEY"})
		if err != nil || result.IsError {
			t.Fatalf("envGet %d: err=%v result=%+v", i+1, err, result)
		}
	}
	fake.mu.Lock()
	pullsAfter := fake.pulls
	fake.mu.Unlock()
	if pullsAfter != pullsBefore+1 {
		t.Fatalf("two MCP reads made %d additional pulls, want 1", pullsAfter-pullsBefore)
	}

	// The same command helper honors --refresh even inside the active window.
	refreshCmd := &cobra.Command{}
	addRefreshFlag(refreshCmd)
	if err := refreshCmd.Flags().Set("refresh", "true"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	pullsBefore = fake.pulls
	fake.mu.Unlock()
	autoPull(refreshCmd, true)
	fake.mu.Lock()
	if fake.pulls != pullsBefore+1 {
		fake.mu.Unlock()
		t.Fatalf("refresh made %d additional pulls, want 1", fake.pulls-pullsBefore)
	}
	pushesBefore := fake.pushes
	fake.mu.Unlock()

	// Root's PersistentPostRun delegates here; a local write is drained, while a
	// clean follow-up command makes no additional push.
	if err := m.env.Set("default", "PUSHED_BY_CMD", "value"); err != nil {
		t.Fatal(err)
	}
	postRunAutoPush(&cobra.Command{})
	postRunAutoPush(&cobra.Command{})
	fake.mu.Lock()
	pushesAfter := fake.pushes
	fake.mu.Unlock()
	if pushesAfter != pushesBefore+1 {
		t.Fatalf("write + clean command made %d pushes, want 1", pushesAfter-pushesBefore)
	}
}

func TestReadCommandsExposeRefreshFlag(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"env get": envGetCmd, "env list": envListCmd, "env export": envExportCmd,
		"text get": textGetCmd, "text list": textListCmd,
		"config get": configGetCmd, "config list": configListCmd,
		"session start": sessionStartCmd, "tui": tuiCmd,
	} {
		if cmd.Flags().Lookup("refresh") == nil {
			t.Errorf("%s does not expose --refresh", name)
		}
	}
}
