package agentcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTOMLServerBlockPreventsInjection(t *testing.T) {
	srv := Server{
		Command: "npx",
		Args:    []string{`evil"]`, "newline\n[mcp_servers.pwned]\ncommand = \"id\""},
		Env:     map[string]string{"KEY": "val\"\n#comment\x01"},
	}
	block := RenderTOMLServerBlock("mcp_servers", "my]server.v1", srv, true, "http_headers")
	merged, err := UpsertTOMLServer("", "mcp_servers", "my]server.v1", block)
	if err != nil {
		t.Fatalf("UpsertTOMLServer failed: %v\nblock:\n%s", err, block)
	}
	servers, err := TOMLServers(merged, "mcp_servers")
	if err != nil {
		t.Fatalf("TOMLServers failed: %v", err)
	}
	if _, ok := servers["pwned"]; ok {
		t.Fatalf("TOML injection created rogue pwned server:\n%s", merged)
	}
	got, ok := TOMLServerFor(merged, "mcp_servers", "my]server.v1", "http_headers")
	if !ok {
		t.Fatalf("round trip failed for special alias:\n%s", merged)
	}
	if len(got.Args) != 2 || got.Args[0] != srv.Args[0] || got.Args[1] != srv.Args[1] {
		t.Fatalf("args round trip mismatch: got %q, want %q", got.Args, srv.Args)
	}
	if got.Env["KEY"] != srv.Env["KEY"] {
		t.Fatalf("env round trip mismatch: got %q, want %q", got.Env["KEY"], srv.Env["KEY"])
	}

	remote := Server{
		Transport: "http",
		URL:       "https://api.example.com/mcp",
		Headers:   map[string]string{"X-Test#Header.1": "a\"b\nc"},
	}
	rBlock := RenderTOMLServerBlock("mcp_servers", "remote.1", remote, true, "http_headers")
	rMerged, err := UpsertTOMLServer("", "mcp_servers", "remote.1", rBlock)
	if err != nil {
		t.Fatalf("UpsertTOMLServer remote failed: %v\n%s", err, rBlock)
	}
	gotRemote, ok := TOMLServerFor(rMerged, "mcp_servers", "remote.1", "http_headers")
	if !ok || gotRemote.Headers["X-Test#Header.1"] != "a\"b\nc" {
		t.Fatalf("remote header round trip mismatch: ok=%v headers=%v", ok, gotRemote.Headers)
	}
}

func TestWriteConfigRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Target is symlink
	targetLink := filepath.Join(dir, "mcp.json")
	if err := os.Symlink(outside, targetLink); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithBackup(targetLink, []byte("{}")); err == nil {
		t.Fatal("WriteWithBackup must reject symlink target")
	}
	if err := WriteSensitive(targetLink, []byte("{}"), 0o755); err == nil {
		t.Fatal("WriteSensitive must reject symlink target")
	}

	// 2. Parent dir is symlink
	parentTarget := t.TempDir()
	parentLink := filepath.Join(dir, "symdir")
	if err := os.Symlink(parentTarget, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := WriteSensitive(filepath.Join(parentLink, "mcp.json"), []byte("{}"), 0o755); err == nil {
		t.Fatal("WriteSensitive must reject symlink parent directory")
	}

	// 3. .bak is symlink
	realTarget := filepath.Join(dir, "real.json")
	if err := os.WriteFile(realTarget, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, realTarget+BackupSuffix); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithBackup(realTarget, []byte("new")); err == nil {
		t.Fatal("WriteWithBackup must reject symlink .bak")
	}
	got, _ := os.ReadFile(outside)
	if !strings.EqualFold(string(got), "keep") {
		t.Fatalf("outside file was modified: %q", string(got))
	}
}
