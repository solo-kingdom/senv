package text

import (
	"strings"
	"testing"
)

func TestManagerTextValidation(t *testing.T) {
	invalid := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00name"}
	for _, group := range invalid {
		t.Run("group/"+group, func(t *testing.T) {
			m, _ := setupTestTextManager(t)
			actions := []struct {
				name string
				fn   func() error
			}{
				{"set", func() error { return m.Set(group, "KEY", "value") }},
				{"get", func() error { _, err := m.Get(group, "KEY"); return err }},
				{"list", func() error { _, err := m.List(group); return err }},
				{"AddGroup", func() error { return m.AddGroup(group) }},
				{"DeleteGroup", func() error { return m.DeleteGroup(group) }},
				{"delete", func() error { return m.Delete(group, "KEY") }},
				{"reader", func() error { return m.SetFromReader(group, "KEY", strings.NewReader("value")) }},
			}
			for _, action := range actions {
				if err := action.fn(); err == nil {
					t.Errorf("%s accepted invalid group %q", action.name, group)
				}
			}
		})
	}
	for _, key := range invalid {
		t.Run("key/"+key, func(t *testing.T) {
			m, _ := setupTestTextManager(t)
			if err := m.Set("notes", key, "value"); err == nil {
				t.Errorf("Set accepted invalid key %q", key)
			}
			if _, err := m.Get("notes", key); err == nil {
				t.Errorf("Get accepted invalid key %q", key)
			}
			if err := m.Delete("notes", key); err == nil {
				t.Errorf("Delete accepted invalid key %q", key)
			}
		})
	}
}
