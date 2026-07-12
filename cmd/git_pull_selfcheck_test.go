package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitPullSelfCheck_WarnsOnDesyncWithoutModifyingFiles(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	writeEnvFileWithPassword(t, cfg, data, "default", "correct-secret")
	startNeverSession(t, cfg, data, "correct-secret")

	// Snapshot data files before the check.
	envPath := filepath.Join(data, "env_default.json.enc")
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}

	// Simulate a pull that brought mismatched metadata.
	desyncMetadata(t, cfg, data)

	var out bytes.Buffer
	postPullSelfCheck(cfg, data, &out)

	got := out.String()
	if !strings.Contains(got, "doctor") {
		t.Errorf("expected warning to mention `senv doctor`; got:\n%s", got)
	}

	// Data files must not be touched.
	after, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("postPullSelfCheck must not modify data files")
	}
}

func TestGitPullSelfCheck_SilentWithoutSession(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	// Deliberately do NOT start a session.

	var out bytes.Buffer
	postPullSelfCheck(cfg, data, &out)

	if out.Len() != 0 {
		t.Errorf("expected no output without a session, got:\n%s", out.String())
	}
}

func TestGitPullSelfCheck_SilentWhenConsistent(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	writeEnvFileWithPassword(t, cfg, data, "default", "correct-secret")
	startNeverSession(t, cfg, data, "correct-secret")

	var out bytes.Buffer
	postPullSelfCheck(cfg, data, &out)

	if out.Len() != 0 {
		t.Errorf("expected no output when consistent, got:\n%s", out.String())
	}
}
