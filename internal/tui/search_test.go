package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func newTestManagers(t *testing.T) Managers {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return Managers{
		Env:    env.NewManager(sm, "pw"),
		Text:   text.NewManager(sm, "pw"),
		Config: config.NewManager(sm, "pw"),
	}
}

func gatherAll(t *testing.T, mgrs Managers) []searchResult {
	t.Helper()
	s := newSearchTab(mgrs)
	msg := s.gather()()
	gm, ok := msg.(searchGatheredMsg)
	if !ok {
		t.Fatalf("gather returned %T", msg)
	}
	return gm.all
}

func TestSearchGathersKeysAndNamesOnly(t *testing.T) {
	mgrs := newTestManagers(t)
	// Plant a secret value across all three types.
	mgrs.Env.Set("default", "API_KEY", "topsecret-value")
	mgrs.Env.Set("prod", "DB_URL", "postgres://topsecret")
	if err := mgrs.Text.Set("default", "readme", "contains topsecret-value too"); err != nil {
		t.Fatalf("text set: %v", err)
	}
	src := writeSourceFile(t, "topsecret-value in config body")
	if err := mgrs.Config.Create("app", src, "/etc/app.conf"); err != nil {
		t.Fatalf("config create: %v", err)
	}

	all := gatherAll(t, mgrs)

	// Nothing in the gathered inventory may expose the secret value.
	for _, r := range all {
		if strings.Contains(r.preview, "topsecret") {
			t.Errorf("preview leaks secret value: %+v", r)
		}
		if strings.Contains(r.key, "topsecret") {
			t.Errorf("value surfaced as a key: %+v", r)
		}
	}

	// Expected keys/names are present.
	keys := map[string]bool{}
	for _, r := range all {
		keys[r.key] = true
	}
	for _, want := range []string{"API_KEY", "DB_URL", "readme", "app"} {
		if !keys[want] {
			t.Errorf("expected key %q in search inventory, missing", want)
		}
	}

	// Cross-type aggregation: all three types are represented.
	seen := map[string]bool{}
	for _, r := range all {
		seen[r.resultType] = true
	}
	if !(seen[typeEnv] && seen[typeText] && seen[typeConfig]) {
		t.Errorf("expected all three result types, got %+v", seen)
	}
}

func TestSearchNeverMatchesValues(t *testing.T) {
	mgrs := newTestManagers(t)
	mgrs.Env.Set("default", "API_KEY", "topsecret-value")

	s := newSearchTab(mgrs)
	s.gathered = gatherAll(t, mgrs)

	// Searching for the secret value itself must yield nothing.
	s.input = "topsecret-value"
	s.refilter()
	if len(s.results) != 0 {
		t.Errorf("value-only search leaked %d results: %+v", len(s.results), s.results)
	}

	// Searching for the key works (case-insensitive).
	s.input = "api_key"
	s.refilter()
	if len(s.results) != 1 || s.results[0].key != "API_KEY" {
		t.Errorf("key search failed: %+v", s.results)
	}

	// Empty input restores the full inventory.
	s.input = ""
	s.refilter()
	if len(s.results) == 0 {
		t.Errorf("empty input should show all gathered entries")
	}
}

func TestSearchJumpMessage(t *testing.T) {
	mgrs := newTestManagers(t)
	mgrs.Env.Set("default", "API_KEY", "x")

	s := newSearchTab(mgrs)
	s.gathered = gatherAll(t, mgrs)
	s.input = "api"
	s.refilter()
	if len(s.results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Pressing enter should produce a command yielding searchJumpMsg.
	next, cmd := s.Update(runeKey("enter"))
	if cmd == nil {
		t.Fatal("enter should return a command")
	}
	msg := cmd()
	jmp, ok := msg.(searchJumpMsg)
	if !ok {
		t.Fatalf("expected searchJumpMsg, got %T", msg)
	}
	if jmp.resultType != typeEnv || jmp.key != "API_KEY" {
		t.Errorf("jump = %+v", jmp)
	}
	_ = next
}

func TestSearchEscCloses(t *testing.T) {
	mgrs := newTestManagers(t)
	s := newSearchTab(mgrs)
	_, cmd := s.Update(runeKey("esc"))
	if cmd == nil {
		t.Fatal("esc should return a command")
	}
	if _, ok := cmd().(searchCloseMsg); !ok {
		t.Fatalf("esc should yield searchCloseMsg")
	}
}
