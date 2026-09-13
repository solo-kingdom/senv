package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wii/senv/internal/session"
)

func TestSessionStartInsecureCacheFlag(t *testing.T) {
	flag := sessionStartCmd.Flags().Lookup("insecure-cache")
	if flag == nil {
		t.Fatal("--insecure-cache flag is not registered")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--insecure-cache default = %q, want false", flag.DefValue)
	}
	if strings.Contains(sessionCmd.Long, "Keychain") || strings.Contains(sessionStartCmd.Long, "Keychain") {
		t.Fatal("session help still mentions Keychain")
	}
	if !strings.Contains(sessionStartCmd.Long, "--insecure-cache") {
		t.Fatal("session start help does not document --insecure-cache")
	}
	if session.InsecureCacheWarning == "" {
		t.Fatal("insecure cache warning is empty")
	}
}

func TestSessionRefreshCommandRegistered(t *testing.T) {
	cmd, _, err := sessionCmd.Find([]string{"refresh"})
	if err != nil || cmd == nil || cmd.Name() != "refresh" {
		t.Fatalf("session refresh is not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("timeout") == nil {
		t.Fatal("session refresh does not expose --timeout")
	}
	if !strings.Contains(cmd.Long, "never prompts for a password") {
		t.Fatal("session refresh help does not state it never prompts")
	}
}

func TestRunSessionSaveInsecureFallback(t *testing.T) {
	origTerminal, origConfirm, origEnable := stdinIsTerminal, insecureCacheConfirm, enableInsecureCache
	t.Cleanup(func() {
		stdinIsTerminal, insecureCacheConfirm, enableInsecureCache = origTerminal, origConfirm, origEnable
	})

	secureStoreErr := fmt.Errorf("failed to save session cache: %w", session.ErrNoSecureSessionStore)

	t.Run("non-interactive stays fail-closed", func(t *testing.T) {
		stdinIsTerminal = func() bool { return false }
		prompted := false
		insecureCacheConfirm = func(string) bool { prompted = true; return true }
		calls := 0
		err := runSessionSave(func() error { calls++; return secureStoreErr })
		if !errors.Is(err, session.ErrNoSecureSessionStore) {
			t.Fatalf("expected fail-closed error, got %v", err)
		}
		if prompted || calls != 1 {
			t.Fatalf("non-interactive path prompted=%v calls=%d, want no prompt and a single call", prompted, calls)
		}
	})

	t.Run("declined keeps the original error", func(t *testing.T) {
		stdinIsTerminal = func() bool { return true }
		insecureCacheConfirm = func(string) bool { return false }
		calls := 0
		err := runSessionSave(func() error { calls++; return secureStoreErr })
		if !errors.Is(err, session.ErrNoSecureSessionStore) {
			t.Fatalf("expected original error after decline, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("declined prompt retried the save: calls=%d", calls)
		}
	})

	t.Run("accepted opts in and retries once", func(t *testing.T) {
		stdinIsTerminal = func() bool { return true }
		insecureCacheConfirm = func(string) bool { return true }
		enabled := false
		enableInsecureCache = func() { enabled = true }
		calls := 0
		err := runSessionSave(func() error {
			calls++
			if calls == 1 {
				return secureStoreErr
			}
			return nil
		})
		if err != nil {
			t.Fatalf("accepted fallback still failed: %v", err)
		}
		if calls != 2 || !enabled {
			t.Fatalf("accepted fallback calls=%d enabled=%v, want one retry with the hatch enabled", calls, enabled)
		}
	})

	t.Run("unrelated errors pass through without prompting", func(t *testing.T) {
		stdinIsTerminal = func() bool { return true }
		insecureCacheConfirm = func(string) bool { return true }
		calls := 0
		wantErr := errors.New("invalid password")
		err := runSessionSave(func() error { calls++; return wantErr })
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected pass-through error, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("unrelated error triggered a retry: calls=%d", calls)
		}
	})
}

func TestSessionClearAllFlagRegistered(t *testing.T) {
	flag := sessionClearCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatal("session clear does not expose --all")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--all default = %q, want false", flag.DefValue)
	}
}

func TestSessionStatusDocumentsUnverifiable(t *testing.T) {
	if !strings.Contains(sessionStatusCmd.Long, "reason") {
		t.Fatal("session status help does not mention reasons")
	}
}
