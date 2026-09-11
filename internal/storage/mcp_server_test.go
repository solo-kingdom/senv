package storage

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func validMCPServerEntry(alias string) *MCPServerEntry {
	return &MCPServerEntry{
		Alias:       alias,
		Transport:   MCPTransportStdio,
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		Env:         map[string]string{"GITHUB_TOKEN": "{{env:secrets:GH_TOKEN}}"},
		Description: "GitHub server",
	}
}

func TestSaveAndLoadMCPServer(t *testing.T) {
	mgr, _ := setupTestManager(t)
	entry := validMCPServerEntry("github")
	if err := mgr.SaveMCPServerWithKey("github", entry, derivedKey(t, mgr, "test-password")); err != nil {
		t.Fatalf("SaveMCPServerWithKey: %v", err)
	}
	got, err := mgr.LoadMCPServerWithKey("github", derivedKey(t, mgr, "test-password"))
	if err != nil {
		t.Fatalf("LoadMCPServerWithKey: %v", err)
	}
	if got.Command != "npx" || len(got.Args) != 2 || got.Description != "GitHub server" {
		t.Fatalf("loaded entry = %+v", got)
	}
	// References stay raw at rest; only export resolves them.
	if got.Env["GITHUB_TOKEN"] != "{{env:secrets:GH_TOKEN}}" {
		t.Fatalf("env value = %q, want the raw template", got.Env["GITHUB_TOKEN"])
	}

	names, err := mgr.ListMCPServers()
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	if len(names) != 1 || names[0] != "github" {
		t.Fatalf("ListMCPServers = %v, want [github]", names)
	}

	if err := mgr.DeleteMCPServer("github"); err != nil {
		t.Fatalf("DeleteMCPServer: %v", err)
	}
	if names, err = mgr.ListMCPServers(); err != nil || len(names) != 0 {
		t.Fatalf("after delete: names = %v, err = %v", names, err)
	}
	// Deletion is idempotent.
	if err := mgr.DeleteMCPServer("github"); err != nil {
		t.Fatalf("second DeleteMCPServer: %v", err)
	}
}

func TestMCPServerValidation(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	cases := []struct {
		name  string
		entry *MCPServerEntry
		want  string
	}{
		{
			name:  "unsupported transport",
			entry: &MCPServerEntry{Alias: "a", Transport: "webrtc", Command: "npx"},
			want:  "unsupported transport",
		},
		{
			name:  "missing transport",
			entry: &MCPServerEntry{Alias: "a", Command: "npx"},
			want:  "unsupported transport",
		},
		{
			name:  "missing command",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportStdio, Command: "   "},
			want:  "command is required",
		},
		{
			name:  "command whitespace",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportStdio, Command: " npx "},
			want:  "whitespace",
		},
		{
			name: "invalid env key",
			entry: &MCPServerEntry{
				Alias: "a", Transport: MCPTransportStdio, Command: "npx",
				Env: map[string]string{"not a key": "v"},
			},
			want: "shell variable name",
		},
		{
			name:  "stdio with url rejected",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportStdio, Command: "npx", URL: "https://api.example.com/mcp"},
			want:  "must not set url",
		},
		{
			name:  "remote with command rejected",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportHTTP, URL: "https://api.example.com/mcp", Command: "npx"},
			want:  "must not set command",
		},
		{
			name:  "remote with args rejected",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportHTTP, URL: "https://api.example.com/mcp", Args: []string{"-y"}},
			want:  "must not set args",
		},
		{
			name: "remote with env rejected",
			entry: &MCPServerEntry{
				Alias: "a", Transport: MCPTransportSSE, URL: "https://api.example.com/sse",
				Env: map[string]string{"K": "v"},
			},
			want: "must not set env",
		},
		{
			name:  "remote missing url",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportHTTP},
			want:  "url is required",
		},
		{
			name:  "remote url bad scheme",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportHTTP, URL: "ftp://api.example.com/mcp"},
			want:  "http(s)",
		},
		{
			name:  "remote url with space",
			entry: &MCPServerEntry{Alias: "a", Transport: MCPTransportHTTP, URL: "https://api.example.com/mcp key=x"},
			want:  "spaces or control characters",
		},
		{
			name: "invalid header name",
			entry: &MCPServerEntry{
				Alias: "a", Transport: MCPTransportHTTP, URL: "https://api.example.com/mcp",
				Headers: map[string]string{"Not A Header": "v"},
			},
			want: "not a valid HTTP header name",
		},
		{
			name: "header value control char",
			entry: &MCPServerEntry{
				Alias: "a", Transport: MCPTransportHTTP, URL: "https://api.example.com/mcp",
				Headers: map[string]string{"X-Api-Key": "line1\nline2"},
			},
			want: "control characters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mgr.SaveMCPServerWithKey(tc.entry.Alias, tc.entry, key)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SaveMCPServerWithKey() error = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := mgr.LoadMCPServerWithKey("../escape", key); err == nil {
		t.Fatal("LoadMCPServerWithKey() accepted a path-traversal alias")
	}
}

func TestSaveAndLoadRemoteMCPServer(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	entry := &MCPServerEntry{
		Alias:     "web-reader",
		Transport: MCPTransportHTTP,
		URL:       "https://api.example.com/mcp?key={{env:secrets:KEY}}",
		Headers:   map[string]string{"Authorization": "Bearer {{text:secrets:T}}"},
	}
	if err := mgr.SaveMCPServerWithKey("web-reader", entry, key); err != nil {
		t.Fatalf("SaveMCPServerWithKey: %v", err)
	}
	got, err := mgr.LoadMCPServerWithKey("web-reader", key)
	if err != nil {
		t.Fatalf("LoadMCPServerWithKey: %v", err)
	}
	if got.Transport != MCPTransportHTTP {
		t.Fatalf("transport = %q, want http", got.Transport)
	}
	// URL and header values stay raw at rest; only export resolves them.
	if got.URL != "https://api.example.com/mcp?key={{env:secrets:KEY}}" {
		t.Fatalf("url = %q, want the raw template", got.URL)
	}
	if got.Headers["Authorization"] != "Bearer {{text:secrets:T}}" {
		t.Fatalf("header = %q, want the raw template", got.Headers["Authorization"])
	}
}

func TestMCPServerFilePermissions(t *testing.T) {
	mgr, base := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveMCPServerWithKey("github", validMCPServerEntry("github"), key); err != nil {
		t.Fatalf("save: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Join(base, "data", MCPServerDirName))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(filepath.Join(base, "data", MCPServerDirName, "github"+ConfigFileSuffix))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestRekeyMigratesMCPServers(t *testing.T) {
	mgr, _ := setupTestManager(t)
	oldKey := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveMCPServerWithKey("github", validMCPServerEntry("github"), oldKey); err != nil {
		t.Fatalf("save: %v", err)
	}
	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKeyWithIterations("new-password", newSalt, crypto.DefaultIterations)
	newHash := crypto.HashPassword("new-password")
	newPasswordKey, _ := crypto.Encrypt(newKey, []byte(newHash))
	result, err := mgr.Rekey(oldKey, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, crypto.DefaultIterations)
	if err != nil {
		t.Fatalf("Rekey() error = %v", err)
	}
	if result.MCPServerFiles != 1 {
		t.Fatalf("MCPServerFiles = %d, want 1", result.MCPServerFiles)
	}
	if _, err := mgr.LoadMCPServerWithKey("github", oldKey); err == nil {
		t.Fatal("old key unexpectedly decrypts MCP server profile")
	}
	if _, err := mgr.LoadMCPServerWithKey("github", newKey); err != nil {
		t.Fatalf("new key cannot decrypt MCP server profile: %v", err)
	}
}

func TestHasOrphanedMCPServers(t *testing.T) {
	mgr, base := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveMCPServerWithKey("github", validMCPServerEntry("github"), key); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.Remove(filepath.Join(base, "config", MetadataFile)); err != nil {
		t.Fatalf("remove metadata: %v", err)
	}
	if !NewManager(filepath.Join(base, "config"), filepath.Join(base, "data")).HasOrphanedData() {
		t.Fatal("HasOrphanedData() = false, want true for MCP server ciphertext")
	}
}

func TestCheckConsistencyReportsBadMCPServerCiphertext(t *testing.T) {
	mgr, base := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveMCPServerWithKey("github", validMCPServerEntry("github"), key); err != nil {
		t.Fatalf("save: %v", err)
	}
	bad := filepath.Join(base, "data", MCPServerDirName, "github"+ConfigFileSuffix)
	if err := os.WriteFile(bad, []byte("not ciphertext"), 0o600); err != nil {
		t.Fatalf("write bad ciphertext: %v", err)
	}
	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency() error = %v", err)
	}
	if report.MCPServerFiles.Total != 1 || report.MCPServerFiles.OK != 0 || len(report.MCPServerFiles.Failed) != 1 {
		t.Fatalf("MCP server probes = %+v", report.MCPServerFiles)
	}
	if report.AllOK() {
		t.Fatal("AllOK() = true for bad MCP server ciphertext")
	}
}

func TestLoadMCPServerValidatesDecryptedFields(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	raw, err := json.Marshal(map[string]any{
		"alias": "bad", "transport": "sse", "command": "npx",
	})
	if err != nil {
		t.Fatalf("marshal bad entry: %v", err)
	}
	if err := mgr.saveSSHEntry(MCPServerDirName, "bad", json.RawMessage(raw), key); err != nil {
		t.Fatalf("write raw encrypted profile: %v", err)
	}
	_, err = mgr.LoadMCPServerWithKey("bad", key)
	if err == nil || !strings.Contains(err.Error(), "invalid MCP server") {
		t.Fatalf("LoadMCPServerWithKey() error = %v, want validation failure", err)
	}
}
