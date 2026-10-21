package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func TestMain(m *testing.M) {
	crypto.LowerIterationsForTesting()
	os.Exit(m.Run())
}

func testSources(t *testing.T) Sources {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := store.Initialize("pw"); err != nil {
		t.Fatalf("init: %v", err)
	}
	return Sources{
		Env:    env.NewManager(store, "pw"),
		Text:   text.NewManager(store, "pw"),
		Backup: backup.NewManager(store, "pw"),
		Config: config.NewManager(store, "pw"),
		SSH:    ssh.NewManager(store, "pw"),
		LLM:    llm.NewProviderManager(store, "pw"),
		MCP:    mcp.NewManager(store, "pw"),
	}
}

func TestGatherDropsValuesAndMatchesDescription(t *testing.T) {
	src := testSources(t)
	const secret = "only-in-value-xyzzy"
	desc := "payment gateway note"
	if err := src.Env.AddGroup("svc", "services"); err != nil {
		t.Fatal(err)
	}
	if err := src.Env.SetWithDescription("svc", "TOKEN", secret, &desc); err != nil {
		t.Fatal(err)
	}
	if err := src.Text.Set("default", "notes", "body has "+secret); err != nil {
		t.Fatal(err)
	}

	items := Gather(src)
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, secret) {
		t.Fatalf("gather JSON leaked secret: %s", body)
	}
	if strings.Contains(body, `"value"`) {
		t.Fatalf("gather JSON has value field: %s", body)
	}

	res, err := Run(items, "payment", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Key != "TOKEN" {
		t.Fatalf("description search hits = %+v", res.Hits)
	}

	res, err = Run(items, secret, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Fatalf("value-only query must miss, got %+v", res.Hits)
	}
}

func TestGatherIncludesSSHHostname(t *testing.T) {
	src := testSources(t)
	if err := src.SSH.AddHost(&storage.HostEntry{
		Alias: "box", Hostname: "web.internal", User: "root", Port: 22,
	}); err != nil {
		t.Fatal(err)
	}
	items := Gather(src)
	res, err := Run(items, "web.internal", []Kind{KindSSH}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Key != "box" {
		t.Fatalf("ssh hits = %+v", res.Hits)
	}
}
