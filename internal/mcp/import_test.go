package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestParseImportFileJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	fixture := `{
	  "mcpServers": {
	    "github": {"command": "npx", "args": ["-y", "github"]},
	    "web": {"url": "https://api.example.com/mcp"}
	  }
	}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ParseImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	stdio, err := BuildImportEntry("github", entries["github"])
	if err != nil || stdio.Transport != storage.MCPTransportStdio || stdio.Command != "npx" {
		t.Fatalf("github = %+v, %v", stdio, err)
	}
	http, err := BuildImportEntry("web", entries["web"])
	if err != nil || http.Transport != storage.MCPTransportHTTP || http.URL != "https://api.example.com/mcp" {
		t.Fatalf("web = %+v, %v", http, err)
	}
}

func TestParseImportFileTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	fixture := `[mcp_servers.remote]
url = "https://api.example.com/mcp"
transport = "streamable-http"

[mcp_servers.local]
command = "npx"
args = ["-y", "server-x"]
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ParseImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := BuildImportEntry("remote", entries["remote"])
	if err != nil || remote.Transport != storage.MCPTransportHTTP {
		t.Fatalf("remote = %+v, %v", remote, err)
	}
	local, err := BuildImportEntry("local", entries["local"])
	if err != nil || local.Command != "npx" || len(local.Args) != 2 {
		t.Fatalf("local = %+v, %v", local, err)
	}
}

func TestParseImportFileBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseImportFile(path)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseImportFileMissing(t *testing.T) {
	_, err := ParseImportFile(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildImportEntryNeitherURLNorCommand(t *testing.T) {
	_, err := BuildImportEntry("broken", map[string]any{"headers": map[string]any{"A": "b"}})
	if err == nil || !strings.Contains(err.Error(), "neither url nor command") {
		t.Fatalf("error = %v", err)
	}
}
