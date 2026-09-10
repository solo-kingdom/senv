package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newMCPExportProject prepares an isolated vault plus a redirected HOME, so
// export targets resolve inside the temp dir.
func newMCPExportProject(t *testing.T) string {
	t.Helper()
	newAuditTestProject(t)
	// newAuditTestProject isolates the session cache by redirecting HOME, so
	// the export home must be set afterwards.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	resetMCPAddFlags(t)
	mcpAddCommand = "npx"
	mcpAddArgs = []string{"-y", "@modelcontextprotocol/server-github"}
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"github"}))

	mcpExportAgents, mcpExportAll = "", true
	mcpExportDryRun, mcpExportYes, mcpExportPrint, mcpExportForce = false, true, false, false
	mcpExportScope = "user"
	mcpUnexportAgents, mcpUnexportAll = "", true
	mcpUnexportDryRun, mcpUnexportYes, mcpUnexportScope = false, true, "user"
	t.Cleanup(func() {
		mcpExportAgents, mcpExportAll = "", false
		mcpExportDryRun, mcpExportYes, mcpExportPrint, mcpExportForce = false, false, false, false
		mcpExportScope = "user"
		mcpUnexportAgents, mcpUnexportAll = "", false
		mcpUnexportDryRun, mcpUnexportYes, mcpUnexportScope = false, false, "user"
	})
	return dir
}

func TestMCPExportRequiresExplicitTarget(t *testing.T) {
	newMCPExportProject(t)
	mcpExportAll, mcpExportAgents = false, ""
	err := mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no export target") {
		t.Fatalf("error = %v, want an explicit-target error", err)
	}

	mcpExportAgents = "nosuchagent"
	err = mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("error = %v, want an unknown-agent error", err)
	}

	mcpExportAll, mcpExportAgents = true, "codex"
	err = mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error = %v, want a mutual-exclusion error", err)
	}
}

func TestMCPExportDryRunDoesNotWrite(t *testing.T) {
	dir := newMCPExportProject(t)
	mcpExportAll = true
	mcpExportDryRun = true
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "Export plan:") || !strings.Contains(out, "github") {
		t.Fatalf("dry-run output missing the plan:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the codex config: %v", err)
	}
}

func TestMCPExportPrintWritesNothing(t *testing.T) {
	dir := newMCPExportProject(t)
	mcpExportAll, mcpExportPrint, mcpExportDryRun = true, true, false
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "[mcp_servers.github]") || !strings.Contains(out, "mcpServers") {
		t.Fatalf("--print output missing snippets:\n%s", out)
	}
	for _, path := range []string{".codex/config.toml", ".claude.json"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Fatalf("--print wrote %s", path)
		}
	}
}

func TestMCPExportAndUnexportRoundTrip(t *testing.T) {
	dir := newMCPExportProject(t)
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "create") {
		t.Fatalf("export output missing actions:\n%s", out)
	}

	codexPath := filepath.Join(dir, ".codex", "config.toml")
	codexConfig, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf("codex config was not written: %v", err)
	}
	if !strings.Contains(string(codexConfig), "[mcp_servers.github]") {
		t.Fatalf("codex config missing the exported entry:\n%s", codexConfig)
	}
	claudePath := filepath.Join(dir, ".claude.json")
	claudeConfig, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("claude config was not written: %v", err)
	}
	if !strings.Contains(string(claudeConfig), "mcpServers") {
		t.Fatalf("claude config missing mcpServers:\n%s", claudeConfig)
	}

	out = captureStdout(t, func() {
		runSSHCommand(t, mcpUnexportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "remove") {
		t.Fatalf("unexport output missing actions:\n%s", out)
	}
	codexConfig, err = os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codexConfig), "[mcp_servers.github]") {
		t.Fatalf("unexport left the codex entry behind:\n%s", codexConfig)
	}
	claudeConfig, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(claudeConfig), "github") {
		t.Fatalf("unexport left the claude entry behind:\n%s", claudeConfig)
	}
}
