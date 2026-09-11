package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// resetMCPAddFlags clears the package-level add/edit flags between direct RunE
// invocations (the CLI tests bypass cobra's own parsing).
func resetMCPAddFlags(t *testing.T) {
	t.Helper()
	prev := []any{mcpAddTransport, mcpAddCommand, mcpAddArgs, mcpAddEnv, mcpAddDescription, mcpAddURL, mcpAddHeaders}
	mcpAddTransport = "stdio"
	mcpAddCommand, mcpAddArgs, mcpAddEnv, mcpAddDescription = "", nil, nil, ""
	mcpAddURL, mcpAddHeaders = "", nil
	t.Cleanup(func() {
		mcpAddTransport = prev[0].(string)
		mcpAddCommand = prev[1].(string)
		mcpAddArgs = prev[2].([]string)
		mcpAddEnv = prev[3].([]string)
		mcpAddDescription = prev[4].(string)
		mcpAddURL = prev[5].(string)
		mcpAddHeaders = prev[6].([]string)
	})
}

func resetMCPEditFlags(t *testing.T) {
	t.Helper()
	fields := []string{"command", "arg", "env", "unset-env", "description", "transport", "url", "header", "unset-header"}
	for _, name := range fields {
		if flag := mcpEditCmd.Flags().Lookup(name); flag != nil {
			flag.Changed = false
		}
	}
	mcpEditCommand, mcpEditArgs, mcpEditEnv, mcpEditUnsetEnv, mcpEditDescription = "", nil, nil, nil, ""
	mcpEditTransport, mcpEditURL, mcpEditHeaders, mcpEditUnsetHeader = "", "", nil, nil
	t.Cleanup(func() {
		mcpEditCommand, mcpEditArgs, mcpEditEnv, mcpEditUnsetEnv, mcpEditDescription = "", nil, nil, nil, ""
		mcpEditTransport, mcpEditURL, mcpEditHeaders, mcpEditUnsetHeader = "", "", nil, nil
		for _, name := range fields {
			if flag := mcpEditCmd.Flags().Lookup(name); flag != nil {
				flag.Changed = false
			}
		}
	})
}

func TestMCPServerCLIFlow(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)
	resetMCPEditFlags(t)

	mcpAddCommand = "npx"
	mcpAddArgs = []string{"-y", "@modelcontextprotocol/server-github"}
	mcpAddEnv = []string{"GITHUB_TOKEN={{env:secrets:GH_TOKEN}}", "LOG_LEVEL=debug"}
	mcpAddDescription = "GitHub official server"
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"github"}))

	// list shows env key names but never env values.
	listOut := captureStdout(t, func() {
		runSSHCommand(t, mcpListCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(listOut, "github") || !strings.Contains(listOut, "GITHUB_TOKEN") {
		t.Fatalf("list output missing profile summary:\n%s", listOut)
	}
	if strings.Contains(listOut, "GH_TOKEN") || strings.Contains(listOut, "debug") {
		t.Fatalf("list output leaked env values:\n%s", listOut)
	}

	// get is the CLI decryption surface and does show values.
	getOut := captureStdout(t, func() {
		runSSHCommand(t, mcpGetCmd.RunE(&cobra.Command{}, []string{"github"}))
	})
	if !strings.Contains(getOut, "{{env:secrets:GH_TOKEN}}") {
		t.Fatalf("get output missing raw env template:\n%s", getOut)
	}

	// edit replaces provided fields without touching the alias.
	mcpEditCommand = "uvx"
	mcpEditCmd.Flags().Lookup("command").Changed = true
	mcpEditUnsetEnv = []string{"LOG_LEVEL"}
	mcpEditCmd.Flags().Lookup("unset-env").Changed = true
	mcpEditDescription = "updated"
	mcpEditCmd.Flags().Lookup("description").Changed = true
	runSSHCommand(t, mcpEditCmd.RunE(mcpEditCmd, []string{"github"}))

	mgr, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := mgr.Get("github")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Alias != "github" || entry.Command != "uvx" || entry.Description != "updated" {
		t.Fatalf("edited entry = %+v", entry)
	}
	if _, ok := entry.Env["LOG_LEVEL"]; ok {
		t.Fatal("--unset-env did not remove LOG_LEVEL")
	}
	if entry.Env["GITHUB_TOKEN"] != "{{env:secrets:GH_TOKEN}}" {
		t.Fatal("--unset-env removed the wrong env key")
	}

	deleteOut := captureStdout(t, func() {
		runSSHCommand(t, mcpDeleteCmd.RunE(&cobra.Command{}, []string{"github"}))
	})
	if !strings.Contains(deleteOut, "unexport") {
		t.Fatalf("delete should point at `senv mcp unexport`:\n%s", deleteOut)
	}
	if _, err := mgr.Get("github"); err == nil {
		t.Fatal("profile still readable after delete")
	}
}

func TestMCPServerAddValidation(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)

	cases := []struct {
		name    string
		command string
		args    []string
		env     []string
		want    string
	}{
		{name: "missing command", command: "", want: "command is required"},
		{name: "bad env pair", command: "npx", env: []string{"NOPE"}, want: "expected KEY=VALUE"},
		{name: "duplicate env key", command: "npx", env: []string{"A=1", "A=2"}, want: "duplicate --env"},
		{name: "invalid env key", command: "npx", env: []string{"bad key=1"}, want: "shell variable name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcpAddCommand, mcpAddArgs, mcpAddEnv = tc.command, tc.args, tc.env
			err := mcpAddCmd.RunE(&cobra.Command{}, []string{"probe"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("add error = %v, want %q", err, tc.want)
			}
		})
	}

	// A remote add without --url is rejected by profile validation.
	mcpAddCommand, mcpAddArgs, mcpAddEnv = "npx", nil, nil
	mcpAddTransport = "http"
	err := mcpAddCmd.RunE(&cobra.Command{}, []string{"remote"})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("transport error = %v", err)
	}

	// Duplicate alias is rejected rather than overwritten.
	mcpAddTransport = "stdio"
	mcpAddCommand = "npx"
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"dup"}))
	err = mcpAddCmd.RunE(&cobra.Command{}, []string{"dup"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate add error = %v", err)
	}
}

func TestMCPServerDeleteLeavesAgentConfigUntouched(t *testing.T) {
	dir := t.TempDir()
	newAuditTestProject(t)
	resetMCPAddFlags(t)

	agentConfig := filepath.Join(dir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(agentConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := []byte("[mcp_servers.other]\ncommand = \"npx\"\n")
	if err := os.WriteFile(agentConfig, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	mcpAddCommand = "npx"
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"github"}))
	captureStdout(t, func() {
		runSSHCommand(t, mcpDeleteCmd.RunE(&cobra.Command{}, []string{"github"}))
	})

	got, err := os.ReadFile(agentConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(seed) {
		t.Fatalf("delete modified an agent config file:\n%s", got)
	}
}
