package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installWithHome runs installInto with HOME redirected to a temp dir, so we
// never touch the developer's real config.
func installWithHome(t *testing.T, target agentTarget, scope string, printOnly bool) (string, *bytes.Buffer, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	var buf bytes.Buffer
	if err := installInto(target, scope, printOnly, &buf); err != nil {
		t.Fatalf("installInto(%s): %v", target.id, err)
	}
	cfgPath := target.resolveConfigPath(home, scope)
	return cfgPath, &buf, home
}

func TestInstallJSON_CreatesNewConfig(t *testing.T) {
	target, ok := findAgent("claude-code")
	if !ok {
		t.Fatal("claude-code not found")
	}
	cfgPath, _, _ := installWithHome(t, target, "user", false)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("config not valid JSON: %v\n%s", err, data)
	}
	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or wrong type: %v", root)
	}
	senv, ok := servers["senv"].(map[string]any)
	if !ok {
		t.Fatalf("senv server missing: %v", servers)
	}
	if senv["command"] == "" {
		t.Fatalf("senv.command empty")
	}
	args, _ := senv["args"].([]any)
	if len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Fatalf("senv.args = %v, want [mcp serve]", args)
	}
}

func TestInstallJSON_PreservesExistingServers(t *testing.T) {
	target, _ := findAgent("cursor")
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := target.resolveConfigPath(home, "user")

	// Pre-existing config with another server and an unrelated top-level key.
	seed := map[string]any{
		"mcpServers": map[string]any{
			"github": map[string]any{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-github"}},
		},
		"someUserSetting": true,
	}
	seedBytes, _ := json.MarshalIndent(seed, "", "  ")
	seedBytes = append(seedBytes, '\n')
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, seedBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := installInto(target, "user", false, &buf); err != nil {
		t.Fatalf("installInto: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("merged config invalid JSON: %v", err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["github"]; !ok {
		t.Fatalf("existing github server was clobbered: %v", servers)
	}
	if _, ok := servers["senv"]; !ok {
		t.Fatalf("senv not added: %v", servers)
	}
	if root["someUserSetting"] != true {
		t.Fatalf("unrelated top-level key lost: %v", root)
	}
	// Backup must exist.
	if _, err := os.Stat(cfgPath + ".bak"); err != nil {
		t.Fatalf("backup not written: %v", err)
	}
}

func TestInstallPrintDoesNotWrite(t *testing.T) {
	target, _ := findAgent("kimi")
	home := t.TempDir()
	t.Setenv("HOME", home)
	var buf bytes.Buffer
	if err := installInto(target, "user", true, &buf); err != nil {
		t.Fatalf("installInto print: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "senv") || !strings.Contains(out, "command") {
		t.Fatalf("print output missing senv entry: %s", out)
	}
	cfgPath := target.resolveConfigPath(home, "user")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("--print wrote a file at %s", cfgPath)
	}
}

func TestInstallTOML_Codex(t *testing.T) {
	target, _ := findAgent("codex")
	cfgPath, buf, _ := installWithHome(t, target, "user", false)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "[mcp_servers.senv]") {
		t.Fatalf("codex config missing senv table:\n%s", s)
	}
	if !strings.Contains(s, `command =`) || !strings.Contains(s, "mcp") {
		t.Fatalf("codex config missing command/args:\n%s", s)
	}
	if !strings.Contains(buf.String(), "Installed senv MCP server") {
		t.Fatalf("missing success message: %s", buf.String())
	}
}

func TestInstallTOML_PreservesExistingTables(t *testing.T) {
	target, _ := findAgent("codex")
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := target.resolveConfigPath(home, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `# user model preference
model = "gpt-5"
[mcp_servers.existing]
command = "echo"
args = ["hi"]
`
	if err := os.WriteFile(cfgPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := installInto(target, "user", false, &buf); err != nil {
		t.Fatalf("installInto: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if !strings.Contains(s, `model = "gpt-5"`) {
		t.Fatalf("existing top-level key lost:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.existing]") {
		t.Fatalf("existing mcp_servers.existing table lost:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.senv]") {
		t.Fatalf("senv table not added:\n%s", s)
	}
	// Exactly one senv block.
	if strings.Count(s, "[mcp_servers.senv]") != 1 {
		t.Fatalf("expected exactly one senv table, got %d:\n%s", strings.Count(s, "[mcp_servers.senv]"), s)
	}
}

func TestInstallTOML_UpsertReplacesExistingSenv(t *testing.T) {
	// Re-installing should replace, not duplicate, the senv block.
	target, _ := findAgent("codex")
	cfgPath, _, _ := installWithHome(t, target, "user", false)
	// Second install into the same file.
	home, _ := os.UserHomeDir()
	var buf bytes.Buffer
	if err := installInto(target, "user", false, &buf); err != nil {
		t.Fatalf("second installInto: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if c := strings.Count(s, "[mcp_servers.senv]"); c != 1 {
		t.Fatalf("after re-install want 1 senv table, got %d:\n%s", c, s)
	}
	_ = home
}

func TestInstallUnknownAgent(t *testing.T) {
	if _, ok := findAgent("nope"); ok {
		t.Fatal("expected unknown agent to not be found")
	}
	if len(supportedAgentIDs()) < 5 {
		t.Fatalf("expected at least 5 supported agents, got %d", len(supportedAgentIDs()))
	}
}

func TestInstallAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var buf bytes.Buffer
	if err := installAll("user", false, &buf); err != nil {
		t.Fatalf("installAll: %v", err)
	}
	// Every supported agent's config file must now exist.
	for _, a := range supportedAgents() {
		cfgPath := a.resolveConfigPath(home, "user")
		if _, err := os.Stat(cfgPath); err != nil {
			t.Fatalf("installAll did not write %s config (%s): %v", a.id, cfgPath, err)
		}
	}
}

func TestUpsertTomlServer_AppendToEmpty(t *testing.T) {
	got, err := upsertTomlServer("", "senv", "[mcp_servers.senv]\ncommand = \"senv\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "[mcp_servers.senv]") {
		t.Fatalf("append to empty produced unexpected output:\n%s", got)
	}
}
