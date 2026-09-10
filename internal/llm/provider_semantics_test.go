package llm

import (
	"errors"
	"strings"
	"testing"

	"github.com/wii/senv/internal/text"
)

func TestAddProviderURLPolicy(t *testing.T) {
	opts := AddProviderOptions{
		Alias: "local", BaseURL: "http://127.0.0.1:11434/v1",
		APIKey: "k", Models: []string{"m"},
	}
	mgr3, _, _ := newTestProviderManager(t)
	if _, err := mgr3.AddProvider(opts); err == nil ||
		!strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP without --allow-http error = %v", err)
	}
	opts.AllowHTTP = true
	mgr2, _, _ := newTestProviderManager(t)
	if _, err := mgr2.AddProvider(opts); err != nil {
		t.Fatalf("AddProvider(allowed HTTP) error = %v", err)
	}
	opts.BaseURL = "https://user:pass@example.com"
	opts.AllowHTTP = false
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(opts); err == nil ||
		!strings.Contains(err.Error(), "userinfo") {
		t.Fatalf("userinfo error = %v", err)
	}
}

func TestForceCredentialSemantics(t *testing.T) {
	t.Run("metadata-only update preserves owned credential", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old-key", Models: []string{"m1"},
		}); err != nil {
			t.Fatalf("initial add: %v", err)
		}
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://b", Models: []string{"m1"}, Force: true,
		})
		if err != nil {
			t.Fatalf("forced metadata update: %v", err)
		}
		if res.Entry.CredentialRef != OwnedCredentialRef("main") {
			t.Fatalf("credential ref = %q", res.Entry.CredentialRef)
		}
		value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
		if err != nil || value != "old-key" {
			t.Fatalf("credential after force = %q, %v; want old-key", value, err)
		}
	})
	t.Run("new owned credential overwrites", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"},
		}); err != nil {
			t.Fatalf("initial add: %v", err)
		}
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://b", APIKey: "new", Models: []string{"m1"}, Force: true,
		}); err != nil {
			t.Fatalf("owned overwrite: %v", err)
		}
		value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
		if err != nil || value != "new" {
			t.Fatalf("credential = %q, %v; want new", value, err)
		}
	})
	t.Run("owned to external deletes old credential", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "old", Models: []string{"m1"},
		}); err != nil {
			t.Fatalf("initial add: %v", err)
		}
		res, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://b", KeyRef: "env:llm/KEY", Models: []string{"m1"}, Force: true,
		})
		if err != nil {
			t.Fatalf("owned to external: %v", err)
		}
		if res.Entry.CredentialRef != "env:llm/KEY" {
			t.Fatalf("credential ref = %q", res.Entry.CredentialRef)
		}
		if _, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main"); err == nil {
			t.Fatal("old owned credential survived external switch")
		}
	})
}

func TestRemoveProviderCompensation(t *testing.T) {
	t.Run("missing owned credential continues", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add: %v", err)
		}
		if err := text.NewManager(store, "test-password").Delete(LLMKeysGroup, "main"); err != nil {
			t.Fatalf("delete credential: %v", err)
		}
		result, err := mgr.RemoveProvider("main")
		if err != nil || !result.CredentialMissing {
			t.Fatalf("RemoveProvider() = %+v, %v; want missing", result, err)
		}
	})
	t.Run("credential failure keeps profile", func(t *testing.T) {
		mgr, _, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "k", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add: %v", err)
		}
		mgr.removeCredential = func() error { return errors.New("injected vault failure") }
		if _, err := mgr.RemoveProvider("main"); err == nil || !strings.Contains(err.Error(), "provider kept") {
			t.Fatalf("RemoveProvider() error = %v, want retained provider", err)
		}
		if _, err := mgr.GetProvider("main"); err != nil {
			t.Fatalf("provider was not retained: %v", err)
		}
	})
	t.Run("profile failure restores credential", func(t *testing.T) {
		mgr, store, _ := newTestProviderManager(t)
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: "main", BaseURL: "https://a", APIKey: "restore-me", Models: []string{"m"},
		}); err != nil {
			t.Fatalf("add: %v", err)
		}
		mgr.removeProfile = func() error { return errors.New("injected profile failure") }
		if _, err := mgr.RemoveProvider("main"); err == nil {
			t.Fatal("RemoveProvider() unexpectedly succeeded")
		}
		value, err := text.NewManager(store, "test-password").Get(LLMKeysGroup, "main")
		if err != nil || value != "restore-me" {
			t.Fatalf("credential = %q, %v; want restored", value, err)
		}
	})
}
