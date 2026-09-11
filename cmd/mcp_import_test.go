package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMCPServerRemoteCLIFlow(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)
	resetMCPEditFlags(t)

	// Add a remote profile; values keep their templates.
	mcpAddTransport = "http"
	mcpAddURL = "https://api.example.com/mcp?key={{env:secrets:KEY}}"
	mcpAddHeaders = []string{"Authorization: Bearer {{env:secrets:T}}", "X-Api-Key: abc"}
	mcpAddDescription = "remote server"
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"web-reader"}))

	// list shows the origin only: no query, no header names or values.
	listOut := captureStdout(t, func() {
		runSSHCommand(t, mcpListCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(listOut, "https://api.example.com") {
		t.Fatalf("list output missing remote origin:\n%s", listOut)
	}
	for _, leaked := range []string{"key=", "X-Api-Key", "Authorization", "abc"} {
		if strings.Contains(listOut, leaked) {
			t.Fatalf("list output leaked %q:\n%s", leaked, listOut)
		}
	}

	// get is the decryption surface and shows url and headers.
	getOut := captureStdout(t, func() {
		runSSHCommand(t, mcpGetCmd.RunE(&cobra.Command{}, []string{"web-reader"}))
	})
	if !strings.Contains(getOut, "https://api.example.com/mcp?key={{env:secrets:KEY}}") ||
		!strings.Contains(getOut, "Authorization: Bearer {{env:secrets:T}}") ||
		!strings.Contains(getOut, "X-Api-Key: abc") {
		t.Fatalf("get output missing remote fields:\n%s", getOut)
	}

	mgr, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}

	// edit replaces url and the whole header set.
	mcpEditURL = "https://api.example.com/v2/mcp"
	mcpEditCmd.Flags().Lookup("url").Changed = true
	mcpEditHeaders = []string{"Authorization: Bearer new"}
	mcpEditCmd.Flags().Lookup("header").Changed = true
	runSSHCommand(t, mcpEditCmd.RunE(mcpEditCmd, []string{"web-reader"}))
	entry, err := mgr.Get("web-reader")
	if err != nil {
		t.Fatal(err)
	}
	if entry.URL != "https://api.example.com/v2/mcp" || len(entry.Headers) != 1 || entry.Headers["Authorization"] != "Bearer new" {
		t.Fatalf("edited remote entry = %+v", entry)
	}

	// Switching a remote profile to stdio fails validation and changes nothing.
	mcpEditTransport = "stdio"
	mcpEditCmd.Flags().Lookup("transport").Changed = true
	if err := mcpEditCmd.RunE(mcpEditCmd, []string{"web-reader"}); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("transport switch error = %v", err)
	}
	same, err := mgr.Get("web-reader")
	if err != nil || same.Transport != "http" {
		t.Fatalf("failed switch mutated the profile: %+v, %v", same, err)
	}

	// A valid switch keeps the url and drops nothing else.
	mcpEditTransport = "sse"
	runSSHCommand(t, mcpEditCmd.RunE(mcpEditCmd, []string{"web-reader"}))
	entry, err = mgr.Get("web-reader")
	if err != nil || entry.Transport != "sse" || entry.URL != "https://api.example.com/v2/mcp" {
		t.Fatalf("switched entry = %+v, %v", entry, err)
	}
}

func TestMCPImportJSON(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)

	fixture := `{
	  "mcpServers": {
	    "explicit-sse": {"type": "sse", "url": "https://api.example.com/sse"},
	    "typed-http": {"type": "http", "url": "https://api.example.com/mcp", "headers": {"Authorization": "Bearer {{env:secrets:T}}"}},
	    "untyped-remote": {"url": "https://other.example.com/mcp", "headers": {"X-Api-Key": "k"}},
	    "stdio-entry": {"command": "npx", "args": ["-y", "server-x"], "env": {"K": "{{env:secrets:V}}"}},
	    "broken": {"headers": {"A": "b"}}
	  },
	  "otherKey": true
	}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	// Dry run writes nothing.
	mcpImportDryRun = true
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpImportCmd.RunE(mcpImportCmd, []string{path}))
	})
	if !strings.Contains(out, "4 created") || !strings.Contains(out, "1 failed") {
		t.Fatalf("dry run summary wrong:\n%s", out)
	}
	if mcpImportDryRun { // restore before vault inspection
		mcpImportDryRun = false
	}
	mgr, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}
	if names, _ := mgr.List(); len(names) != 0 {
		t.Fatalf("dry run created profiles: %v", names)
	}

	// The real import creates four profiles, reports the broken entry,
	// and exits non-zero because of the failure.
	var importErr error
	out = captureStdout(t, func() {
		importErr = mcpImportCmd.RunE(mcpImportCmd, []string{path})
	})
	if importErr == nil || !strings.Contains(importErr.Error(), "1 项导入失败") {
		t.Fatalf("import error = %v, want a partial-failure error", importErr)
	}
	if !strings.Contains(out, "4 created, 0 conflict, 1 failed") {
		t.Fatalf("import summary wrong:\n%s", out)
	}

	entry, err := mgr.Get("typed-http")
	if err != nil || entry.Transport != "http" || entry.URL != "https://api.example.com/mcp" {
		t.Fatalf("typed-http = %+v, %v", entry, err)
	}
	if entry.Headers["Authorization"] != "Bearer {{env:secrets:T}}" {
		t.Fatalf("headers not stored raw: %+v", entry)
	}
	sse, err := mgr.Get("explicit-sse")
	if err != nil || sse.Transport != "sse" {
		t.Fatalf("explicit-sse = %+v, %v", sse, err)
	}
	untyped, err := mgr.Get("untyped-remote")
	if err != nil || untyped.Transport != "http" || untyped.Headers["X-Api-Key"] != "k" {
		t.Fatalf("untyped-remote = %+v, %v", untyped, err)
	}
	stdio, err := mgr.Get("stdio-entry")
	if err != nil || stdio.Command != "npx" || len(stdio.Args) != 2 {
		t.Fatalf("stdio-entry = %+v, %v", stdio, err)
	}
	if stdio.Env["K"] != "{{env:secrets:V}}" {
		t.Fatalf("env not stored raw: %+v", stdio)
	}
	servers, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 4 {
		t.Fatalf("profile count = %d, want 4", len(servers))
	}

	// Re-importing conflicts instead of overwriting (the broken entry still
	// fails, so the command exits non-zero).
	var reimportErr error
	out = captureStdout(t, func() {
		reimportErr = mcpImportCmd.RunE(mcpImportCmd, []string{path})
	})
	if reimportErr == nil || !strings.Contains(reimportErr.Error(), "1 项导入失败") {
		t.Fatalf("re-import error = %v", reimportErr)
	}
	if !strings.Contains(out, "4 conflict") || !strings.Contains(out, "already exists") {
		t.Fatalf("re-import output wrong:\n%s", out)
	}
	entry, _ = mgr.Get("typed-http")
	if entry.URL != "https://api.example.com/mcp" {
		t.Fatalf("conflict overwrote the profile: %+v", entry)
	}
}

func TestMCPImportCodexTOML(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)

	fixture := `model = "gpt"

[mcp_servers.remote]
url = "https://api.example.com/mcp"
transport = "streamable-http"

[mcp_servers.local]
command = "npx"
args = ["-y", "server-x"]
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		runSSHCommand(t, mcpImportCmd.RunE(mcpImportCmd, []string{path}))
	})
	if !strings.Contains(out, "2 created, 0 conflict, 0 failed") {
		t.Fatalf("import summary wrong:\n%s", out)
	}
	mgr, err := getMCPManager()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := mgr.Get("remote")
	if err != nil || remote.Transport != "http" || remote.URL != "https://api.example.com/mcp" {
		t.Fatalf("remote = %+v, %v", remote, err)
	}
	local, err := mgr.Get("local")
	if err != nil || local.Command != "npx" || len(local.Args) != 2 {
		t.Fatalf("local = %+v, %v", local, err)
	}
}

func TestMCPImportBadFiles(t *testing.T) {
	newAuditTestProject(t)
	resetMCPAddFlags(t)
	mcpImportDryRun = false

	if err := mcpImportCmd.RunE(mcpImportCmd, []string{filepath.Join(t.TempDir(), "missing.json")}); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("missing file error = %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpImportCmd.RunE(mcpImportCmd, []string{bad}); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("bad file error = %v", err)
	}
	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte(`{"other": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpImportCmd.RunE(mcpImportCmd, []string{empty}); err == nil || !strings.Contains(err.Error(), "no MCP server entries") {
		t.Fatalf("no-entries error = %v", err)
	}
}
