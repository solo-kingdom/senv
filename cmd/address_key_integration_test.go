package cmd

import (
	"fmt"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func setupAddressKeyTest(t *testing.T) (*text.Manager, *env.Manager) {
	t.Helper()

	dir := t.TempDir()
	storeMgr := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	password := "test-password"
	return text.NewManager(storeMgr, password), env.NewManager(storeMgr, password)
}

func TestAddressKeyTextGetSetDelete(t *testing.T) {
	textMgr, _ := setupAddressKeyTest(t)

	if err := textMgr.AddGroup("feg"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	// text set feg:ACCOUNT val
	group, key := resolveAddressKey("feg:ACCOUNT", "default")
	if err := textMgr.Set(group, key, "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// text get feg:ACCOUNT == text get -g feg ACCOUNT
	group, key = resolveAddressKey("feg:ACCOUNT", "default")
	gotAddr, err := textMgr.Get(group, key)
	if err != nil {
		t.Fatalf("Get address: %v", err)
	}
	gotFlag, err := textMgr.Get("feg", "ACCOUNT")
	if err != nil {
		t.Fatalf("Get flag: %v", err)
	}
	if gotAddr != gotFlag || gotAddr != "val" {
		t.Errorf("get mismatch: address=%q flag=%q", gotAddr, gotFlag)
	}

	// address overrides -g other
	group, key = resolveAddressKey("feg:ACCOUNT", "other")
	got, err := textMgr.Get(group, key)
	if err != nil {
		t.Fatalf("Get with other flag: %v", err)
	}
	if got != "val" {
		t.Errorf("address should override flagGroup, got %q", got)
	}

	// plain key uses flagGroup default
	group, key = resolveAddressKey("MYKEY", "default")
	if err := textMgr.Set(group, key, "plain-val"); err != nil {
		t.Fatalf("Set plain: %v", err)
	}
	got, err = textMgr.Get("default", "MYKEY")
	if err != nil || got != "plain-val" {
		t.Errorf("plain key path: got %q err=%v", got, err)
	}

	// text delete feg:ACCOUNT
	group, key = resolveAddressKey("feg:ACCOUNT", "default")
	if err := textMgr.Delete(group, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := textMgr.Get("feg", "ACCOUNT"); err == nil {
		t.Error("entry should be deleted")
	}
}

func TestAddressKeyEnvGetSetDelete(t *testing.T) {
	_, envMgr := setupAddressKeyTest(t)

	if err := envMgr.AddGroup("feg"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	group, key := resolveAddressKey("feg:KEY", "default")
	if err := envMgr.Set(group, key, "env-val"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	group, key = resolveAddressKey("feg:KEY", "default")
	gotAddr, err := envMgr.Get(group, key)
	if err != nil {
		t.Fatalf("Get address: %v", err)
	}
	gotFlag, err := envMgr.Get("feg", "KEY")
	if err != nil {
		t.Fatalf("Get flag: %v", err)
	}
	if gotAddr != gotFlag || gotAddr != "env-val" {
		t.Errorf("get mismatch: address=%q flag=%q", gotAddr, gotFlag)
	}

	group, key = resolveAddressKey("feg:KEY", "default")
	if err := envMgr.Delete(group, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := envMgr.Get("feg", "KEY"); err == nil {
		t.Error("entry should be deleted")
	}

	// plain key path unchanged
	group, key = resolveAddressKey("PLAIN", "default")
	if err := envMgr.Set(group, key, "x"); err != nil {
		t.Fatalf("Set plain: %v", err)
	}
	got, err := envMgr.Get("default", "PLAIN")
	if err != nil || got != "x" {
		t.Errorf("plain env key: got %q err=%v", got, err)
	}
}

func TestShorthandSetUnchanged(t *testing.T) {
	textMgr, envMgr := setupAddressKeyTest(t)

	if err := textMgr.AddGroup("mygroup"); err != nil {
		t.Fatalf("AddGroup text: %v", err)
	}
	if err := envMgr.AddGroup("mygroup"); err != nil {
		t.Fatalf("AddGroup env: %v", err)
	}

	// root/text/env shorthand set paths still use parseAddress directly
	for _, tc := range []struct {
		name  string
		addr  string
		value string
	}{
		{"root", "mygroup:mykey", "root-val"},
		{"colon-key", ":onlykey", "colon-key-val"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group, key, ok := parseAddress(tc.addr)
			if !ok {
				t.Fatal("parseAddress failed")
			}
			if err := runTextShorthandWithManager(textMgr, group, key, "", []string{tc.value}); err != nil {
				t.Fatalf("text shorthand: %v", err)
			}
			got, err := textMgr.Get(group, key)
			if err != nil || got != tc.value {
				t.Errorf("text shorthand get: got %q err=%v", got, err)
			}
		})
	}

	group, key, ok := parseAddress("mygroup:envkey")
	if !ok {
		t.Fatal("parseAddress failed")
	}
	if err := runEnvShorthandWithManager(envMgr, group, key, []string{"env-shorthand-val"}); err != nil {
		t.Fatalf("env shorthand: %v", err)
	}
	got, err := envMgr.Get(group, key)
	if err != nil || got != "env-shorthand-val" {
		t.Errorf("env shorthand get: got %q err=%v", got, err)
	}
}

// runTextShorthandWithManager is a test helper that mirrors runTextShorthand without global managers.
func runTextShorthandWithManager(m *text.Manager, group, key, file string, valueArgs []string) error {
	if len(valueArgs) >= 1 {
		return m.Set(group, key, valueArgs[0])
	}
	return m.Set(group, key, "")
}

// runEnvShorthandWithManager is a test helper that mirrors runEnvShorthand without global managers.
func runEnvShorthandWithManager(m *env.Manager, group, key string, valueArgs []string) error {
	if len(valueArgs) == 0 {
		return fmt.Errorf("env set requires a value")
	}
	return m.Set(group, key, valueArgs[0])
}
