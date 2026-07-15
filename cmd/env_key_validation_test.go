package cmd

import "testing"

// TestEnvSetRejectsInvalidKeys covers the spec scenarios where env keys that
// cannot be exported as shell variable names must be rejected at write time.
func TestEnvSetRejectsInvalidKeys(t *testing.T) {
	_, envMgr := setupAddressKeyTest(t)

	for _, key := range []string{
		"openviking/root_api_key",
		"123KEY",
		"my-key",
		"foo.bar",
	} {
		t.Run("reject/"+key, func(t *testing.T) {
			// Plain key path (equivalent to: senv env set <key> <value>).
			group, k := resolveAddressKey(key, "default")
			if err := envMgr.Set(group, k, "v"); err == nil {
				t.Errorf("Set(%q) = nil, want error", key)
			}
			if _, err := envMgr.Get("default", k); err == nil {
				t.Errorf("invalid key %q should not be persisted", key)
			}
		})
	}
}

// TestEnvSetAcceptsValidKeys covers the spec scenarios where valid shell
// variable names are accepted.
func TestEnvSetAcceptsValidKeys(t *testing.T) {
	_, envMgr := setupAddressKeyTest(t)

	for _, key := range []string{"API_KEY", "_PRIVATE"} {
		t.Run("accept/"+key, func(t *testing.T) {
			group, k := resolveAddressKey(key, "default")
			if err := envMgr.Set(group, k, "v"); err != nil {
				t.Fatalf("Set(%q): %v", key, err)
			}
			if got, err := envMgr.Get("default", k); err != nil || got != "v" {
				t.Errorf("Get(%q) = %q, err=%v, want %q", key, got, err, "v")
			}
		})
	}
}

// TestEnvShorthandKeyValidation exercises the group:key shorthand path. The ':'
// is consumed by parseAddress first; the resulting key is then validated.
func TestEnvShorthandKeyValidation(t *testing.T) {
	_, envMgr := setupAddressKeyTest(t)

	if err := envMgr.AddGroup("prod"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	// prod:my/key -> key "my/key" is invalid (contains '/').
	g, k, ok := parseAddress("prod:my/key")
	if !ok {
		t.Fatal("parseAddress failed")
	}
	if err := runEnvShorthandWithManager(envMgr, g, k, []string{"v"}); err == nil {
		t.Errorf("shorthand prod:my/key should be rejected")
	}
	if _, err := envMgr.Get("prod", "my/key"); err == nil {
		t.Errorf("invalid shorthand key should not be persisted")
	}

	// prod:API_KEY -> key "API_KEY" is valid.
	g, k, ok = parseAddress("prod:API_KEY")
	if !ok {
		t.Fatal("parseAddress failed")
	}
	if err := runEnvShorthandWithManager(envMgr, g, k, []string{"secret"}); err != nil {
		t.Fatalf("shorthand prod:API_KEY: %v", err)
	}
	got, err := envMgr.Get("prod", "API_KEY")
	if err != nil || got != "secret" {
		t.Errorf("Get(prod:API_KEY) = %q, err=%v, want %q", got, err, "secret")
	}
}
