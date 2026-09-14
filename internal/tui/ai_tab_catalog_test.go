package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const aiCatalogPayload = `{"prov1":{"id":"prov1","name":"P1","models":{"m1":{"id":"m1"}}}}`

func TestAICatalogRefreshWritesCacheAndToastsCounts(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(aiCatalogPayload))
	}))
	t.Cleanup(srv.Close)
	tab.catalogURL = srv.URL

	out, cmd := tab.Update(runeKey("R"))
	tab = out.(*aiTab)
	msgs := runCmd(cmd)
	if len(msgs) == 0 {
		t.Fatal("R must produce a catalog command")
	}
	out, follow := tab.Update(msgs[0])
	tab = out.(*aiTab)
	var toast string
	for _, m := range runCmd(follow) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if !strings.Contains(toast, "1 providers") || !strings.Contains(toast, "1 models") {
		t.Fatalf("refresh toast missing counts: %q", toast)
	}
	data, err := os.ReadFile(tab.mgr.LLMCatalog)
	if err != nil || !strings.Contains(string(data), "prov1") {
		t.Fatalf("cache not updated: %v\n%s", err, data)
	}
}

func TestAICatalogRefreshHTTPErrorKeepsOldCache(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	if err := os.MkdirAll(filepath.Dir(tab.mgr.LLMCatalog), 0o700); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"version":1,"fetched_at":"2020-01-01T00:00:00Z","source":"old","providers":{}}`)
	if err := os.WriteFile(tab.mgr.LLMCatalog, old, 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(aiCatalogPayload))
	}))
	t.Cleanup(srv.Close)
	tab.catalogURL = srv.URL

	out, cmd := tab.Update(runeKey("R"))
	tab = out.(*aiTab)
	msgs := runCmd(cmd)
	out, follow := tab.Update(msgs[0])
	tab = out.(*aiTab)
	var got string
	for _, m := range runCmd(follow) {
		if em, ok := m.(errMsg); ok {
			got = em.err.Error()
		}
	}
	if !strings.Contains(got, "unexpected status") {
		t.Fatalf("HTTP error missing, got %q", got)
	}
	after, err := os.ReadFile(tab.mgr.LLMCatalog)
	if err != nil || string(after) != string(old) {
		t.Fatalf("old cache must be unchanged: %v\n%s", err, after)
	}
}

func TestAICatalogRefreshBadJSONKeepsOldCache(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	old := []byte("keep-me")
	if err := os.MkdirAll(filepath.Dir(tab.mgr.LLMCatalog), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tab.mgr.LLMCatalog, old, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	t.Cleanup(srv.Close)
	tab.catalogURL = srv.URL

	out, cmd := tab.Update(runeKey("R"))
	tab = out.(*aiTab)
	msgs := runCmd(cmd)
	out, follow := tab.Update(msgs[0])
	tab = out.(*aiTab)
	var got string
	for _, m := range runCmd(follow) {
		if em, ok := m.(errMsg); ok {
			got = em.err.Error()
		}
	}
	if got == "" {
		t.Fatal("bad JSON must surface as errMsg")
	}
	after, _ := os.ReadFile(tab.mgr.LLMCatalog)
	if string(after) != string(old) {
		t.Fatalf("old cache mutated:\n%s", after)
	}
}

func TestAICatalogRefreshEmptyPathWarns(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	tab.mgr.LLMCatalog = ""
	out, cmd := tab.Update(runeKey("R"))
	tab = out.(*aiTab)
	var toast string
	for _, m := range runCmd(cmd) {
		if tm, ok := m.(toastMsg); ok {
			toast = tm.text
		}
	}
	if !strings.Contains(toast, "senv ai refresh") {
		t.Fatalf("empty path toast = %q", toast)
	}
}

func TestAICatalogCtrlRDoesNotFetch(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(aiCatalogPayload))
	}))
	t.Cleanup(srv.Close)
	tab.catalogURL = srv.URL

	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	tab = flushTab(out, cmd).(*aiTab)
	if hits.Load() != 0 {
		t.Fatalf("ctrl+r must not hit the catalog source, hits=%d", hits.Load())
	}
	if !tab.loaded {
		t.Fatal("ctrl+r must still reload locally")
	}
}

func TestAICatalogRefreshBinding(t *testing.T) {
	tab := newAITab(Managers{})
	set := registeredKeySet(tab.Bindings())
	if !set["R"] {
		t.Fatalf("Bindings missing R: %v", set)
	}
}
