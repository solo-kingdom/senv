package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestManagerEnvValidation(t *testing.T) {
	invalidGroups := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00group"}
	for _, group := range invalidGroups {
		t.Run("group/"+group, func(t *testing.T) {
			m := newTestManager(t)
			actions := []struct {
				name string
				fn   func() error
			}{
				{"create", func() error { return m.Set(group, "API_KEY", "value") }},
				{"read", func() error { _, err := m.Get(group, "API_KEY"); return err }},
				{"AddGroup", func() error { return m.AddGroup(group) }},
				{"activate", func() error { return m.ActivateGroup(group) }},
				{"deactivate", func() error { return m.DeactivateGroup(group) }},
				{"delete", func() error { return m.Delete(group, "API_KEY") }},
			}
			for _, action := range actions {
				if err := action.fn(); err == nil {
					t.Errorf("%s accepted invalid group %q", action.name, group)
				}
			}
			if group != "" {
				if _, err := m.List(group); err == nil {
					t.Errorf("list accepted invalid group %q", group)
				}
			}
		})
	}

	invalidKeys := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "1KEY", "bad-key", "nul\x00key"}
	for _, key := range invalidKeys {
		t.Run("key/"+key, func(t *testing.T) {
			m := newTestManager(t)
			if err := m.Set("default", key, "value"); err == nil {
				t.Errorf("Set accepted invalid key %q", key)
			}
			if _, err := m.Get("default", key); err == nil {
				t.Errorf("Get accepted invalid key %q", key)
			}
			if err := m.Delete("default", key); err == nil {
				t.Errorf("Delete accepted invalid key %q", key)
			}
		})
	}
}

func TestHistoricalEnvIdentity(t *testing.T) {
	m := newTestManager(t)
	envs := filepath.Join(m.storage.GetDataPath(), storage.EnvDirName)
	if err := os.MkdirAll(filepath.Join(envs, "bad:group"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.List(""); err == nil {
		t.Fatal("List accepted an invalid historical env group")
	}
	if _, err := m.ListGroups(); err == nil {
		t.Fatal("ListGroups accepted an invalid historical env group")
	}
}
