package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/search"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func testSearchSources(t *testing.T) search.Sources {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := store.Initialize("pw"); err != nil {
		t.Fatalf("init: %v", err)
	}
	return search.Sources{
		Env:    env.NewManager(store, "pw"),
		Text:   text.NewManager(store, "pw"),
		Backup: backup.NewManager(store, "pw"),
		Config: config.NewManager(store, "pw"),
		SSH:    ssh.NewManager(store, "pw"),
	}
}

func TestRunSearchJSONNoValueAndKindFilter(t *testing.T) {
	src := testSearchSources(t)
	secret := "super-secret-value"
	desc := "LiteLLM admin UI"
	if err := src.Env.AddGroup("svc", "services"); err != nil {
		t.Fatal(err)
	}
	if err := src.Env.SetWithDescription("svc", "LITELLM_UI_PASSWORD", secret, &desc); err != nil {
		t.Fatal(err)
	}
	if err := src.Text.Set("default", "notes", "mentions litellm in body only"); err != nil {
		t.Fatal(err)
	}
	if err := src.SSH.AddHost(&storage.HostEntry{Alias: "edge", Hostname: "web.example"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runSearch(&buf, src, "litellm", []search.Kind{search.KindEnv}, 20, true); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if strings.Contains(body, secret) || strings.Contains(body, `"value"`) {
		t.Fatalf("leaked value: %s", body)
	}
	var res search.Result
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Key != "LITELLM_UI_PASSWORD" {
		t.Fatalf("hits = %+v", res.Hits)
	}

	buf.Reset()
	if err := runSearch(&buf, src, "web", []search.Kind{search.KindSSH}, 20, true); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Kind != search.KindSSH {
		t.Fatalf("ssh filter hits = %+v", res.Hits)
	}
}

func TestRunSearchEmptyQuery(t *testing.T) {
	src := testSearchSources(t)
	err := runSearch(&bytes.Buffer{}, src, "  ", nil, 20, false)
	if err == nil {
		t.Fatal("want empty query error")
	}
}

func TestSearchCmdRegistered(t *testing.T) {
	if searchCmd.Use != "search <query>" {
		t.Fatalf("Use = %q", searchCmd.Use)
	}
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("search not registered on root")
	}
}
