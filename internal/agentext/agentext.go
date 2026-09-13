// Package agentext installs the external extensions a coding agent needs for
// the config senv writes to take effect. PI is the current case: it has no
// built-in MCP support, so senv's PI target only works through the external
// pi-mcp-adapter extension.
//
// Installs are best-effort by contract. A missing installer, a failed install
// or a timeout is reported to the caller, which still writes the config: a
// write the user asked for must not be blocked by an optional dependency they
// can also install by hand. senv never removes the extension afterwards.
package agentext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wii/senv/internal/agentcfg"
)

// Status values reported by Result.
const (
	StatusInstalled   = "installed"
	StatusFailed      = "failed"
	StatusUnavailable = "unavailable"
)

// installTimeout bounds the installer subprocess: a fresh npm install can take
// tens of seconds, and senv must not hang forever on a wedged network.
const installTimeout = 3 * time.Minute

// maxReasonLen caps the installer output echoed in a message so reports stay
// on one readable line.
const maxReasonLen = 160

// Result is the outcome of one best-effort prerequisite install.
type Result struct {
	// Status is StatusInstalled, StatusFailed or StatusUnavailable.
	Status string
	// Message is the full single-line report for CLI output.
	Message string
	// Short is a compact form for transient TUI toasts.
	Short string
}

// Ensure installs target's declared prerequisite when it is missing. It returns
// (Result, true) when an install was attempted (whether or not it succeeded)
// and (Result{}, false) when the target declares no prerequisite or it is
// already present, so callers stay silent on the common path.
func Ensure(target agentcfg.Target, home string) (Result, bool) {
	req := target.Prerequisite
	if req == nil {
		return Result{}, false
	}
	if prerequisitePresent(req, home) {
		return Result{}, false
	}

	command := req.Command
	if command == "" {
		command = target.ID
	}
	manual := strings.TrimSpace(command + " " + strings.Join(req.Args, " "))
	if _, err := lookPath(command); err != nil {
		return Result{
			Status:  StatusUnavailable,
			Message: fmt.Sprintf("%s is not on PATH; install %s manually", command, req.Display),
			Short:   fmt.Sprintf("%s not found; install %s manually: %s", command, req.Package, manual),
		}, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	out, err := runInstaller(ctx, command, req.Args)
	if err != nil {
		return Result{
			Status:  StatusFailed,
			Message: fmt.Sprintf("could not install %s: %s; config written anyway, install manually: %s", req.Package, installFailure(out, err), manual),
			Short:   fmt.Sprintf("%s install failed; run: %s", req.Package, manual),
		}, true
	}
	return Result{
		Status:  StatusInstalled,
		Message: fmt.Sprintf("installed %s (%s)", req.Package, manual),
		Short:   fmt.Sprintf("installed %s", req.Package),
	}, true
}

// prerequisitePresent reports whether the agent's settings already list the
// package. An unreadable or malformed settings file counts as "unknown", so
// senv attempts the install instead of silently skipping a needed extension.
func prerequisitePresent(req *agentcfg.Prerequisite, home string) bool {
	if req.Package == "" || req.SettingsPath == nil {
		return false
	}
	data, err := os.ReadFile(req.SettingsPath(home))
	if err != nil {
		return false
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}
	for _, raw := range settings.Packages {
		if packageEntryMatches(raw, req.Package) {
			return true
		}
	}
	return false
}

// packageEntryMatches accepts both settings spellings of a package: the plain
// string form and the object form with a "source" key.
func packageEntryMatches(raw json.RawMessage, pkg string) bool {
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		var object struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return false
		}
		name = object.Source
	}
	return normalizePackage(name) == pkg
}

// normalizePackage maps pi's settings spellings — "name", "npm:name",
// "name@version" and "@scope/name@version" — to the bare package name.
func normalizePackage(entry string) string {
	entry = strings.TrimPrefix(strings.TrimSpace(entry), "npm:")
	if at := strings.LastIndex(entry, "@"); at > 0 {
		entry = entry[:at]
	}
	return entry
}

// installFailure picks the most useful line out of installer output, falling
// back to the process error when it produced none.
func installFailure(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return truncate(line, maxReasonLen)
		}
	}
	return truncate(err.Error(), maxReasonLen)
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}

// Test seams: the installer is a real subprocess in production.
var (
	lookPath     = exec.LookPath
	runInstaller = func(ctx context.Context, command string, args []string) ([]byte, error) {
		return exec.CommandContext(ctx, command, args...).CombinedOutput()
	}
)
