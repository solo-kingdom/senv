package cmd

import (
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
