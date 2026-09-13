package agentext

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/agentcfg"
)

// withPrerequisite returns a pi-like target plus the home dir its settings file
// lives in.
func withPrerequisite(t *testing.T) (agentcfg.Target, string) {
	t.Helper()
	home := t.TempDir()
	target := agentcfg.Target{
		ID:   "pi",
		Name: "PI",
		Prerequisite: &agentcfg.Prerequisite{
			Display:      "pi-mcp-adapter extension (`pi install npm:pi-mcp-adapter`)",
			Package:      "pi-mcp-adapter",
			SettingsPath: func(string) string { return filepath.Join(home, "settings.json") },
			Args:         []string{"install", "npm:pi-mcp-adapter"},
		},
	}
	return target, home
}

// stubInstaller replaces the subprocess seams for one test and reports whether
// the installer was invoked.
func stubInstaller(t *testing.T, found bool, out []byte, err error) *bool {
	t.Helper()
	called := false
	originalLookPath, originalRun := lookPath, runInstaller
	lookPath = func(string) (string, error) {
		if !found {
			return "", fmt.Errorf("not found")
		}
		return "/fake/pi", nil
	}
	runInstaller = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		called = true
		return out, err
	}
	t.Cleanup(func() {
		lookPath, runInstaller = originalLookPath, originalRun
	})
	return &called
}

func writeSettings(t *testing.T, home, settings string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureNoPrerequisiteIsSilent(t *testing.T) {
	called := stubInstaller(t, true, nil, nil)
	if result, attempted := Ensure(agentcfg.Target{ID: "cursor", Name: "Cursor"}, t.TempDir()); attempted {
		t.Fatalf("target without prerequisite attempted: %+v", result)
	}
	if *called {
		t.Fatal("installer ran for a target without prerequisite")
	}
}

func TestEnsureSkipsWhenPackageAlreadyListed(t *testing.T) {
	target, home := withPrerequisite(t)
	writeSettings(t, home, `{"theme":"dark","packages":["npm:pi-mcp-adapter"],"other":1}`)
	called := stubInstaller(t, true, nil, nil)

	if result, attempted := Ensure(target, home); attempted {
		t.Fatalf("installed prerequisite attempted again: %+v", result)
	}
	if *called {
		t.Fatal("installer ran for an already installed package")
	}
}

func TestEnsureInstallsMissingPackage(t *testing.T) {
	target, home := withPrerequisite(t)
	called := stubInstaller(t, true, []byte("added 49 packages\nInstalled npm:pi-mcp-adapter\n"), nil)

	result, attempted := Ensure(target, home)
	if !attempted {
		t.Fatal("missing package did not trigger an install attempt")
	}
	if !*called {
		t.Fatal("installer was not invoked")
	}
	if result.Status != StatusInstalled {
		t.Fatalf("status = %q, want %q (%s)", result.Status, StatusInstalled, result.Message)
	}
	if !strings.Contains(result.Message, "pi-mcp-adapter") || !strings.Contains(result.Message, "npm:pi-mcp-adapter") {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestEnsureFailureIsReportedButNotFatal(t *testing.T) {
	target, home := withPrerequisite(t)
	stubInstaller(t, true, []byte("npm ERR! code ENETUNREACH\nnpm ERR! network unreachable\n"), fmt.Errorf("exit status 1"))

	result, attempted := Ensure(target, home)
	if !attempted {
		t.Fatal("failed install must still report an attempt")
	}
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", result.Status, StatusFailed)
	}
	for _, want := range []string{"network unreachable", "config written anyway", "pi install npm:pi-mcp-adapter"} {
		if !strings.Contains(result.Message, want) {
			t.Fatalf("message %q missing %q", result.Message, want)
		}
	}
	if result.Short == "" {
		t.Fatal("failed install needs a short TUI form")
	}
}

func TestEnsureUnavailableWhenInstallerMissing(t *testing.T) {
	target, home := withPrerequisite(t)
	called := stubInstaller(t, false, nil, nil)

	result, attempted := Ensure(target, home)
	if !attempted {
		t.Fatal("missing installer must report an attempt")
	}
	if *called {
		t.Fatal("installer ran despite not being on PATH")
	}
	if result.Status != StatusUnavailable {
		t.Fatalf("status = %q, want %q", result.Status, StatusUnavailable)
	}
	if !strings.Contains(result.Message, "not on PATH") || !strings.Contains(result.Message, "pi install npm:pi-mcp-adapter") {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestPrerequisitePresentShapes(t *testing.T) {
	target, home := withPrerequisite(t)

	cases := []struct {
		name     string
		settings string
		present  bool
	}{
		{"string bare", `{"packages":["pi-mcp-adapter"]}`, true},
		{"string npm prefix", `{"packages":["npm:pi-mcp-adapter"]}`, true},
		{"string pinned", `{"packages":["npm:pi-mcp-adapter@2.33.0"]}`, true},
		{"object source", `{"packages":[{"source":"pi-mcp-adapter","extensions":[]}]}`, true},
		{"other package", `{"packages":["npm:@virdis/subagents"]}`, false},
		{"no packages key", `{"theme":"dark"}`, false},
		{"malformed json", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeSettings(t, home, tc.settings)
			if got := prerequisitePresent(target.Prerequisite, home); got != tc.present {
				t.Fatalf("prerequisitePresent = %v, want %v", got, tc.present)
			}
		})
	}

	// Absent settings file means "unknown": attempt the install.
	if err := os.Remove(filepath.Join(home, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if prerequisitePresent(target.Prerequisite, home) {
		t.Fatal("missing settings file must not report the package as installed")
	}
}

func TestInstallFailureFallsBackToProcessError(t *testing.T) {
	if got := installFailure(nil, fmt.Errorf("exit status 1")); got != "exit status 1" {
		t.Fatalf("installFailure = %q", got)
	}
	long := strings.Repeat("x", maxReasonLen+50)
	if got := installFailure([]byte("\n\n"+long+"\n"), nil); len([]rune(got)) > maxReasonLen {
		t.Fatalf("failure reason not truncated: %d runes", len([]rune(got)))
	}
}

func TestNormalizePackage(t *testing.T) {
	cases := map[string]string{
		"pi-mcp-adapter":            "pi-mcp-adapter",
		"npm:pi-mcp-adapter":        "pi-mcp-adapter",
		"npm:pi-mcp-adapter@2.33.0": "pi-mcp-adapter",
		"@org/pkg":                  "@org/pkg",
		"npm:@org/pkg@1.0.0":        "@org/pkg",
		"  npm:pkg  ":               "pkg",
	}
	for in, want := range cases {
		if got := normalizePackage(in); got != want {
			t.Errorf("normalizePackage(%q) = %q, want %q", in, got, want)
		}
	}
}
