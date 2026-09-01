package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestCreateWithGroupAndDescription(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "key: value\n")

	if err := m.Create("app", src, "~/etc/app.conf", "work", "work app config"); err != nil {
		t.Fatalf("create: %v", err)
	}

	info, err := m.Get("app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Group != "work" {
		t.Errorf("group = %q, want work", info.Group)
	}
	if info.Description != "work app config" {
		t.Errorf("description = %q, want work app config", info.Description)
	}
}

func TestCreateDefaultGroup(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "key: value\n")

	if err := m.Create("app", src, "/etc/app.conf", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := m.Get("app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Group != storage.ConfigDefaultGroup {
		t.Errorf("group = %q, want %q", info.Group, storage.ConfigDefaultGroup)
	}
}

func TestListGroupFilter(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	for _, tc := range []struct{ name, group string }{
		{"a", "work"}, {"b", "work"}, {"c", "personal"},
	} {
		src := filepath.Join(dir, tc.name+".conf")
		writeFile(t, src, "x\n")
		if err := m.Create(tc.name, src, "/tmp/"+tc.name, tc.group, ""); err != nil {
			t.Fatalf("create %s: %v", tc.name, err)
		}
	}

	all, err := m.List("")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("list all = %d, want 3", len(all))
	}

	work, err := m.List("work")
	if err != nil {
		t.Fatalf("list work: %v", err)
	}
	if len(work) != 2 {
		t.Errorf("list work = %d, want 2", len(work))
	}
	for _, info := range work {
		if info.Group != "work" {
			t.Errorf("unexpected group %q in filtered list", info.Group)
		}
	}

	groups, err := m.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if strings.Join(groups, ",") != "personal,work" {
		t.Errorf("groups = %v, want [personal work]", groups)
	}
}

func TestSetMeta(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "x\n")
	if err := m.Create("app", src, "/tmp/app", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := m.SetMeta("app", "prod", "prod config"); err != nil {
		t.Fatalf("setmeta: %v", err)
	}
	info, err := m.Get("app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Group != "prod" || info.Description != "prod config" {
		t.Errorf("meta = %q/%q, want prod/prod config", info.Group, info.Description)
	}

	// empty group falls back to default
	if err := m.SetMeta("app", "", "d"); err != nil {
		t.Fatalf("setmeta empty group: %v", err)
	}
	info, _ = m.Get("app")
	if info.Group != storage.ConfigDefaultGroup {
		t.Errorf("group = %q, want default", info.Group)
	}

	if err := m.SetMeta("missing", "g", "d"); err == nil {
		t.Error("expected error for missing config")
	}
}

func TestOldFormatIndexCompat(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "x\n")
	if err := m.Create("app", src, "/tmp/app", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Rewrite the index in the old format (no group/description fields).
	oldJSON := []byte(`{"configs":{"app":{"name":"app","encrypted_file":"app.enc","target_path":"/tmp/app","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}}}`)
	idxPath := filepath.Join(m.storage.GetConfigPath(), storage.ConfigIndexFile)
	if err := os.WriteFile(idxPath, oldJSON, 0o600); err != nil {
		t.Fatalf("write old index: %v", err)
	}

	infos, err := m.List("")
	if err != nil {
		t.Fatalf("list old index: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("list = %d, want 1", len(infos))
	}
	if infos[0].Group != storage.ConfigDefaultGroup {
		t.Errorf("group = %q, want default for old-format entry", infos[0].Group)
	}
	if infos[0].Description != "" {
		t.Errorf("description = %q, want empty for old-format entry", infos[0].Description)
	}

	// Encrypted content still decrypts.
	content, err := m.loadConfigFile("app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(content) != "x\n" {
		t.Errorf("content = %q, want x", string(content))
	}
}

func TestResolveTargetPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	t.Setenv("SENV_TEST_APP", "myapp")

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"plain absolute", "/etc/app.conf", "/etc/app.conf", false},
		{"home expand", "~/.config/app.conf", filepath.Join(home, ".config/app.conf"), false},
		{"env var", "$SENV_TEST_APP/config.yaml", "myapp/config.yaml", false},
		{"env var braces", "${SENV_TEST_APP}/config.yaml", "myapp/config.yaml", false},
		{"home plus env", "~/.config/$SENV_TEST_APP/c.yaml", filepath.Join(home, ".config/myapp/c.yaml"), false},
		{"undefined var", "$SENV_NOPE/c.yaml", "", true},
		{"undefined var braces", "${SENV_NOPE}/c.yaml", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTargetPath(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveTargetPath(%q) expected error, got %q", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveTargetPath(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ResolveTargetPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExportResolvesEnvVars(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "app.conf")
	writeFile(t, src, "secret: 1\n")
	if err := m.Create("app", src, "$SENV_TEST_OUT/app.conf", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}

	outDir := t.TempDir()
	t.Setenv("SENV_TEST_OUT", outDir)
	if err := m.Export("app", ""); err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "app.conf"))
	if err != nil {
		t.Fatalf("read exported: %v", err)
	}
	if string(data) != "secret: 1\n" {
		t.Errorf("exported = %q, want secret: 1", string(data))
	}
}
