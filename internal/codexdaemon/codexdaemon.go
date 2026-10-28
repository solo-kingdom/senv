// Package codexdaemon talks to a machine-local codex app-server daemon.
//
// A running daemon copies its environment once at startup and never refreshes
// it, so a provider or MCP change only takes effect after a restart. That is
// why the switch and export flows offer a restart instead of promising the new
// configuration is live.
//
// Restart runs "codex app-server daemon restart" as a child of senv, and the
// daemon it spawns inherits the caller's environment. senv therefore passes the
// variables it would print for `senv env export`, so a restart issued from a
// shell that never evaluated that export still yields a daemon that can resolve
// its env_key credentials.
package codexdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// binary is the executable name resolved through PATH.
const binary = "codex"

// probeTimeout bounds the status probe; `daemon version` answers over a local
// control socket, so anything slower means codex is wedged.
const probeTimeout = 3 * time.Second

// restartTimeout covers a graceful stop plus a start. Codex's shutdown grace
// window defaults to 60s, so the bound sits above that.
const restartTimeout = 90 * time.Second

// Status reports what the local daemon answered.
type Status struct {
	// Running is true when a managed daemon reported itself as running.
	Running bool
	// Version is the running app-server version, when reported.
	Version string
}

// Probe asks the local codex daemon whether it is running.
//
// It returns (Status{}, nil) whenever the answer is "no daemon" — codex not
// installed, an old CLI without the daemon subcommand, or no running instance.
// Those are all ordinary situations where senv must stay silent. A non-nil
// error is reserved for a daemon that exists but could not be inspected, which
// the caller may still surface.
func Probe(ctx context.Context) (Status, error) {
	path, err := lookPath(binary)
	if err != nil {
		return Status{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := runVersion(ctx, path)
	if err != nil {
		// codex answers "no daemon" with a non-zero exit; an older CLI rejects
		// the subcommand outright. Both mean the same thing here: nothing to
		// restart, stay silent. Only a probe that never completed (timeout,
		// cancellation) is worth reporting.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Status{}, err
		}
		return Status{}, nil
	}

	var report struct {
		Status       string `json:"status"`
		AppServerVer string `json:"appServerVersion"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return Status{}, fmt.Errorf("parse codex daemon status: %w", err)
	}
	return Status{Running: report.Status == "running", Version: report.AppServerVer}, nil
}

// Restart restarts the local daemon, adding env to the environment it inherits.
//
// env is the resolved `senv env export` set. It is layered over the caller's own
// environment rather than replacing it, so PATH, HOME and TLS settings the user
// exported by hand still reach the daemon.
func Restart(ctx context.Context, env map[string]string) error {
	path, err := lookPath(binary)
	if err != nil {
		return fmt.Errorf("codex not found on PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, restartTimeout)
	defer cancel()

	out, err := runRestart(ctx, path, mergeEnv(os.Environ(), env))
	if err != nil {
		if reason := lastLine(string(out)); reason != "" {
			return fmt.Errorf("codex app-server daemon restart failed: %s", reason)
		}
		return fmt.Errorf("codex app-server daemon restart failed: %w", err)
	}
	return nil
}

// mergeEnv layers overrides onto base. Keys are compared by name only, so the
// child sees exactly one value per name.
func mergeEnv(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		merged[name] = value
	}
	for name, value := range overrides {
		merged[name] = value
	}
	out := make([]string, 0, len(merged))
	for name, value := range merged {
		out = append(out, name+"="+value)
	}
	return out
}

// lastLine returns the last non-empty line of command output, which is where
// codex puts its error summary.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// Test seams: production talks to the real codex CLI.
var (
	lookPath   = exec.LookPath
	runVersion = func(ctx context.Context, path string) ([]byte, error) {
		return exec.CommandContext(ctx, path, "app-server", "daemon", "version").Output()
	}
	runRestart = func(ctx context.Context, path string, env []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, "app-server", "daemon", "restart")
		cmd.Env = env
		return cmd.CombinedOutput()
	}
)
