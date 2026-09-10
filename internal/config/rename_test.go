package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameKeepsTargetAndContent(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "server: 8080\n")
	if err := m.Create("app", src, "/etc/app.conf", "ops", "demo"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := m.Rename("app", "app2"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	info, err := m.Get("app2")
	if err != nil {
		t.Fatalf("get renamed config: %v", err)
	}
	if info.TargetPath != "/etc/app.conf" || info.Group != "ops" || info.Description != "demo" {
		t.Errorf("metadata changed by rename: %+v", info)
	}
	content, err := m.loadConfigFile("app2")
	if err != nil {
		t.Fatalf("load renamed content: %v", err)
	}
	if string(content) != "server: 8080\n" {
		t.Errorf("content = %q, want unchanged", content)
	}
	if _, err := m.Get("app"); err == nil {
		t.Error("old config name still resolves after rename")
	}
}

func TestRenameRejectsConflictsAndMissing(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "src.conf")
	writeFile(t, src, "x=1\n")
	if err := m.Create("app", src, "/etc/app.conf", "", ""); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := m.Create("cli", src, "/etc/cli.conf", "", ""); err != nil {
		t.Fatalf("create cli: %v", err)
	}
	if err := m.Rename("app", "cli"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflict error = %v, want already-exists", err)
	}
	if err := m.Rename("missing", "other"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing error = %v, want not-found", err)
	}
	if err := m.Rename("app", "bad/name"); err == nil {
		t.Error("invalid new name accepted")
	}
	// A failed rename must leave both entries usable.
	if _, err := m.Get("app"); err != nil {
		t.Fatalf("source broken by failed rename: %v", err)
	}
	if _, err := m.Get("cli"); err != nil {
		t.Fatalf("target broken by failed rename: %v", err)
	}
}

func TestSetMetaUpdatesGroupAndDescription(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "src.conf")
	writeFile(t, src, "x=1\n")
	if err := m.Create("app", src, "/etc/app.conf", "ops", "old"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.SetMeta("app", "infra", "new"); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	info, err := m.Get("app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Group != "infra" || info.Description != "new" {
		t.Errorf("meta = %+v, want infra/new", info)
	}
	if err := m.SetMeta("missing", "infra", ""); err == nil {
		t.Error("SetMeta on missing config should fail")
	}
}
