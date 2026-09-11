package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/session"
)

// rewriteCurrentVaultCache patches the single runtime cache slot so status
// states that depend on time or boot identity can be exercised without
// waiting or rebooting. It must only be called after a session was written.
func rewriteCurrentVaultCache(t *testing.T, mutate func(doc map[string]any)) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "senv", "session-*"))
	if err != nil {
		t.Fatalf("glob cache: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one cache slot, got %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	mutate(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(matches[0], out, 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}

// TestSessionStatusReportsFourStates drives `session status` through
// no-session / active / expired / invalidated / unverifiable and asserts the
// reason and next step are visible for each.
func TestSessionStatusReportsFourStates(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)

	status := func() string {
		return captureStdout(t, func() {
			if err := sessionStatusCmd.RunE(sessionStatusCmd, nil); err != nil {
				t.Errorf("session status: %v", err)
			}
		})
	}

	if out := status(); !strings.Contains(out, "Session: No active session") {
		t.Fatalf("no-session output = %q", out)
	}

	sm := session.NewManager(cfg, data)
	defer sm.Close()
	timeout, err := session.ParseTimeout("8h")
	if err != nil {
		t.Fatalf("parse timeout: %v", err)
	}
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if out := status(); !strings.Contains(out, "Session: Active") || !strings.Contains(out, "Sliding window") {
		t.Fatalf("active output = %q", out)
	}

	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	})
	if out := status(); !strings.Contains(out, "Session: Expired") || !strings.Contains(out, "senv session start") {
		t.Fatalf("expired output = %q", out)
	}

	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["timeout_type"] = string(session.TimeoutRestart)
		doc["boot_id"] = "some-other-boot"
		doc["expires_at"] = time.Time{}.UTC().Format(time.RFC3339Nano)
	})
	if out := status(); !strings.Contains(out, "Session: Invalidated") || !strings.Contains(out, "system rebooted") {
		t.Fatalf("invalidated output = %q", out)
	}

	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["timeout_type"] = "bogus"
	})
	if out := status(); !strings.Contains(out, "Session: Unverifiable") ||
		!strings.Contains(out, "Cache: retained (not deleted)") ||
		!strings.Contains(out, "senv session clear --all") {
		t.Fatalf("unverifiable output = %q", out)
	}
}

// TestSessionStatusShowsCapAndRetains covers ADR-0017 visibility: an active
// duration session names its absolute cap, and non-reusable states say the
// cache is retained plus exactly one next action.
func TestSessionStatusShowsCapAndRetains(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)

	sm := session.NewManager(cfg, data)
	defer sm.Close()
	timeout, _ := session.ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	active := captureStdout(t, func() {
		if err := sessionStatusCmd.RunE(sessionStatusCmd, nil); err != nil {
			t.Errorf("session status: %v", err)
		}
	})
	if !strings.Contains(active, "Session cap:") {
		t.Fatalf("active output must name the absolute cap: %q", active)
	}

	// Rebooted restart session: retained and not auto-cleared.
	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["timeout_type"] = string(session.TimeoutRestart)
		doc["boot_id"] = "some-other-boot"
		doc["expires_at"] = time.Time{}.UTC().Format(time.RFC3339Nano)
	})
	invalidated := captureStdout(t, func() {
		if err := sessionStatusCmd.RunE(sessionStatusCmd, nil); err != nil {
			t.Errorf("session status: %v", err)
		}
	})
	if !strings.Contains(invalidated, "Cache: retained") {
		t.Fatalf("invalidated output must say the cache is retained: %q", invalidated)
	}
	if strings.Contains(invalidated, "will be cleared") {
		t.Fatalf("invalidated output must not promise a clear: %q", invalidated)
	}
	if _, cache, err := sm.PeekCachedKey(); err != nil || cache == nil {
		t.Fatalf("status must not clear the invalidated cache: cache=%v err=%v", cache, err)
	}
}

// TestSessionRefreshErrorMessages asserts refresh reports a cause plus exactly
// one next action for expired / invalidated / unverifiable sessions.
func TestSessionRefreshErrorMessages(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)

	sm := session.NewManager(cfg, data)
	defer sm.Close()
	timeout, _ := session.ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}

	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	})
	err := sessionRefreshCmd.RunE(sessionRefreshCmd, nil)
	if err == nil {
		t.Fatal("refresh on expired session must fail")
	}
	if !strings.Contains(err.Error(), "next: senv session start") {
		t.Fatalf("expired refresh error = %q, want single next action", err)
	}
	if !strings.Contains(err.Error(), "cache retained") {
		t.Fatalf("expired refresh error = %q, want cache retained", err)
	}
}

// TestSessionRefreshNoMutation asserts refresh never mutates or removes the
// cache on non-reusable states, and never touches stdin for a prompt.
func TestSessionRefreshNoMutation(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)

	sm := session.NewManager(cfg, data)
	defer sm.Close()
	timeout, _ := session.ParseTimeout("restart")
	if err := sm.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	before, cacheBefore, err := sm.PeekCachedKey()
	if err != nil || cacheBefore == nil {
		t.Fatalf("peek before: cache=%v err=%v", cacheBefore, err)
	}

	// Point the cache at another vault so it classifies as invalidated.
	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["data_path_hash"] = "ffffffffffffffff"
	})

	if err := sessionRefreshCmd.RunE(sessionRefreshCmd, nil); err == nil {
		t.Fatal("refresh on invalidated session must fail")
	}

	after, cacheAfter, err := sm.PeekCachedKey()
	if err != nil || cacheAfter == nil {
		t.Fatalf("refresh must not delete the cache: cache=%v err=%v", cacheAfter, err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("refresh must not rewrite the cached key")
	}
}
