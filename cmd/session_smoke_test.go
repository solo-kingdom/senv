package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/session"
)

// captureStderr redirects os.Stderr for the duration of fn.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	fn()
	_ = writer.Close()
	os.Stderr = old
	return <-done
}

// sessionSmokeStdin returns a rewindable password source. promptPassword reads
// each prompt through a fresh bufio.Reader, so the test must rewind the file
// before every password prompt.
func sessionSmokeStdin(t *testing.T, password string) func() {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("temp stdin: %v", err)
	}
	if _, err := file.WriteString(password + "\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	old := os.Stdin
	t.Cleanup(func() {
		os.Stdin = old
		_ = file.Close()
	})
	return func() {
		os.Stdin = file
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			t.Fatalf("rewind stdin: %v", err)
		}
	}
}

// TestSessionLifecycleSmoke walks the documented lifecycle end to end:
// start -> status -> refresh (no prompt) -> equivalent-path reuse ->
// clear (current vault) -> clear --all -> unverifiable retention.
func TestSessionLifecycleSmoke(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	useProjectPaths(t, cfg, data)
	feedPassword := sessionSmokeStdin(t, "correct-secret")

	// start
	feedPassword()
	if out := captureStdout(t, func() {
		if err := sessionStartCmd.RunE(sessionStartCmd, nil); err != nil {
			t.Errorf("session start: %v", err)
		}
	}); !strings.Contains(out, "Session started") {
		t.Fatalf("start output = %q", out)
	}

	// status -> active
	if out := captureStdout(t, func() {
		if err := sessionStatusCmd.RunE(sessionStatusCmd, nil); err != nil {
			t.Errorf("session status: %v", err)
		}
	}); !strings.Contains(out, "Session: Active") {
		t.Fatalf("status output = %q", out)
	}

	// refresh never prompts for a password
	if err := sessionRefreshCmd.Flags().Set("timeout", "8h"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	refreshed := captureStdout(t, func() {
		if err := sessionRefreshCmd.RunE(sessionRefreshCmd, nil); err != nil {
			t.Errorf("session refresh: %v", err)
		}
	})
	if !strings.Contains(refreshed, "Session refreshed") {
		t.Fatalf("refresh output = %q", refreshed)
	}

	// equivalent path spelling reuses the same vault slot, without a prompt
	equivalent := data + string(os.PathSeparator) + "."
	if _, err := resolveAuth(cfg, equivalent, func(string) (string, error) {
		t.Fatal("equivalent path spelling must reuse the cached session")
		return "", nil
	}); err != nil {
		t.Fatalf("resolveAuth on equivalent path: %v", err)
	}

	// clear only the current vault
	if out := captureStdout(t, func() {
		if err := sessionClearCmd.RunE(sessionClearCmd, nil); err != nil {
			t.Errorf("session clear: %v", err)
		}
	}); !strings.Contains(out, "current vault") {
		t.Fatalf("clear output = %q", out)
	}
	if status := session.NewManager(cfg, data).DescribeCache(); status.State != session.StateNoSession {
		t.Fatalf("session must be gone after clear, got %s", status.State)
	}

	// clear --all with a fresh session
	feedPassword()
	if err := sessionStartCmd.RunE(sessionStartCmd, nil); err != nil {
		t.Fatalf("re-start session: %v", err)
	}
	if err := sessionClearCmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("set --all: %v", err)
	}
	t.Cleanup(func() { _ = sessionClearCmd.Flags().Set("all", "false") })
	if out := captureStdout(t, func() {
		if err := sessionClearCmd.RunE(sessionClearCmd, nil); err != nil {
			t.Errorf("session clear --all: %v", err)
		}
	}); !strings.Contains(out, "All session caches cleared") {
		t.Fatalf("clear --all output = %q", out)
	}

	// unverifiable: corrupt the slot's timeout type, then a business read must
	// report the reason, keep the cache, and audit the event.
	feedPassword()
	if err := sessionStartCmd.RunE(sessionStartCmd, nil); err != nil {
		t.Fatalf("start before corruption: %v", err)
	}
	rewriteCurrentVaultCache(t, func(doc map[string]any) {
		doc["timeout_type"] = "bogus"
	})
	var unverifiableOut string
	stderr := captureStderr(t, func() {
		unverifiableOut = captureStdout(t, func() {
			if err := sessionStatusCmd.RunE(sessionStatusCmd, nil); err != nil {
				t.Errorf("session status: %v", err)
			}
		})
	})
	if !strings.Contains(unverifiableOut, "Session: Unverifiable") || !strings.Contains(unverifiableOut, "retained") {
		t.Fatalf("unverifiable status = %q (stderr=%q)", unverifiableOut, stderr)
	}
	cache, err := session.NewManager(cfg, data).LoadCache()
	if err != nil || cache == nil {
		t.Fatalf("unverifiable cache must be retained: cache=%v err=%v", cache, err)
	}
	if _, err := session.NewManager(cfg, data).GetCachedKey(); err == nil {
		t.Fatal("corrupt session must not be reusable")
	}
	raw, err := os.ReadFile(session.AuditLogPath())
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	log := string(raw)
	for _, want := range []string{"session_start", "session_clear", "session_unverifiable"} {
		if !strings.Contains(log, want) {
			t.Errorf("audit log missing %q", want)
		}
	}
	if strings.Contains(log, cache.Key) {
		t.Error("audit log leaked the cached key")
	}
	// the unverifiable cache must still exist on disk after the failed read
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "senv")); err != nil {
		t.Fatalf("runtime cache dir missing: %v", err)
	}
}
