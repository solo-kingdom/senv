package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestManagerConfigValidation(t *testing.T) {
	invalid := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00name"}
	for _, name := range invalid {
		t.Run("name/"+name, func(t *testing.T) {
			m := newTestManager(t)
			source := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(source, []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			actions := []struct {
				label string
				fn    func() error
			}{
				{"create", func() error { return m.Create(name, source, "", "default", "") }},
				{"read", func() error { _, err := m.Get(name); return err }},
				{"edit", func() error { _, err := m.PrepareEdit(name); return err }},
				{"export", func() error { return m.Export(name, filepath.Join(t.TempDir(), "out")) }},
				{"install", func() error { _, err := m.PlanInstall(Scope{Name: name}); return err }},
				{"uninstall", func() error { _, err := m.PlanUninstall(Scope{Name: name}); return err }},
				{"delete", func() error { return m.Delete(name) }},
			}
			for _, action := range actions {
				if err := action.fn(); err == nil {
					t.Errorf("%s accepted invalid config name %q", action.label, name)
				}
			}
		})
	}
	for _, group := range invalid[1:] { // empty is the documented default-group shorthand.
		t.Run("group/"+group, func(t *testing.T) {
			m := newTestManager(t)
			source := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(source, []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := m.Create("app", source, "", group, ""); err == nil {
				t.Errorf("Create accepted invalid group %q", group)
			}
			if _, err := m.List(group); err == nil {
				t.Errorf("List accepted invalid group %q", group)
			}
			if _, err := m.PlanInstall(Scope{Group: group}); err == nil {
				t.Errorf("PlanInstall accepted invalid group %q", group)
			}
		})
	}
}

func TestManagerConfigIndexValidation(t *testing.T) {
	m := newTestManager(t)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Create("app", source, filepath.Join(t.TempDir(), "target"), "default", ""); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"configs":{"app":{"name":"other","encrypted_file":"app.enc","group":"default"}}}`)
	if err := os.WriteFile(filepath.Join(m.storage.GetConfigPath(), storage.ConfigIndexFile), corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	actions := []struct {
		name string
		fn   func() error
	}{
		{"create", func() error { return m.Create("other", source, "", "default", "") }},
		{"read", func() error { _, err := m.Get("app"); return err }},
		{"list", func() error { _, err := m.List(""); return err }},
		{"edit", func() error { _, err := m.PrepareEdit("app"); return err }},
		{"export", func() error { return m.Export("app", out) }},
		{"install", func() error { _, err := m.PlanInstall(Scope{Name: "app"}); return err }},
		{"delete", func() error { return m.Delete("app") }},
	}
	for _, action := range actions {
		if err := action.fn(); err == nil {
			t.Errorf("%s accepted corrupt config index", action.name)
		}
	}
	if _, err := os.Stat(filepath.Join(m.storage.GetDataPath(), "app.enc")); err != nil {
		t.Fatalf("ciphertext changed after corrupt-index operations: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("export target changed after corrupt index: %v", err)
	}
}
