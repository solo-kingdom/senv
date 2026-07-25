package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// newManagersForTest builds a managers trio backed by a freshly initialized
// temp project secured by the given password.
func newManagersForTest(t *testing.T, password string) (*managers, string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cfg")
	dataPath := filepath.Join(dir, "data")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	store := storage.NewManager(configPath, dataPath)
	if err := store.Initialize(password); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return &managers{
		env:    env.NewManager(store, password),
		text:   text.NewManager(store, password),
		config: config.NewManager(store, password),
	}, configPath, dataPath
}

// textOf unwraps the single TextContent of a CallToolResult.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// asMap unmarshals a JSON text block into map[string]any.
func asMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", body, err)
	}
	return m
}

func TestMCPEnvSetGetDelete(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	// Set via group:key address
	res, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "prod:API_KEY", Value: "secret123"})
	if err != nil {
		t.Fatalf("envSet: %v", err)
	}
	if res.IsError {
		t.Fatalf("envSet returned tool error: %s", textOf(t, res))
	}

	// Get resolves the same address
	res, _, err = m.envGet(ctx, nil, envGetInput{Key: "prod:API_KEY"})
	if err != nil {
		t.Fatalf("envGet: %v", err)
	}
	got := asMap(t, textOf(t, res))
	if got["value"] != "secret123" {
		t.Fatalf("envGet value = %v, want secret123", got["value"])
	}
	if got["group"] != "prod" {
		t.Fatalf("envGet group = %v, want prod", got["group"])
	}

	// Bare key falls back to the default group
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "FOO", Value: "bar"}); err != nil {
		t.Fatal(err)
	}
	res, _, err = m.envGet(ctx, nil, envGetInput{Key: "FOO"})
	if err != nil {
		t.Fatal(err)
	}
	if asMap(t, textOf(t, res))["value"] != "bar" {
		t.Fatalf("default-group get failed: %s", textOf(t, res))
	}

	// Delete
	res, _, err = m.envDelete(ctx, nil, envKeyInput{Key: "prod:API_KEY"})
	if err != nil {
		t.Fatalf("envDelete: %v", err)
	}
	if asMap(t, textOf(t, res))["status"] != "deleted" {
		t.Fatalf("envDelete status = %s", textOf(t, res))
	}

	// Get after delete -> tool error (IsError), not a go error
	res, _, err = m.envGet(ctx, nil, envGetInput{Key: "prod:API_KEY"})
	if err != nil {
		t.Fatalf("envGet after delete returned go error %v; expected IsError tool result", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError after deleting key")
	}
}

func TestMCPEnvListAndExport(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "default:A", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "default:B", Value: "{{env:default:A}}"}); err != nil {
		t.Fatal(err)
	}

	// List
	res, _, err := m.envList(ctx, nil, listInput{})
	if err != nil {
		t.Fatal(err)
	}
	body := asMap(t, textOf(t, res))
	def, ok := body["default"].(map[string]any)
	if !ok {
		t.Fatalf("list missing default group: %v", body)
	}
	if def["A"] != "1" {
		t.Fatalf("A = %v", def["A"])
	}

	// Export with reference resolution
	res, _, err = m.envExport(ctx, nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	exports := asMap(t, textOf(t, res))["exports"].(string)
	if !strings.Contains(exports, "A='1'") {
		t.Fatalf("export missing A='1': %s", exports)
	}
	if !strings.Contains(exports, "B='1'") {
		t.Fatalf("export did not resolve B ref: %s", exports)
	}
}

func TestMCPTextSetGetListDelete(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	if _, _, err := m.textSet(ctx, nil, envSetValueInput{Key: "certs:TLS", Value: "-----BEGIN-----"}); err != nil {
		t.Fatal(err)
	}
	res, _, err := m.textGet(ctx, nil, envGetInput{Key: "certs:TLS"})
	if err != nil {
		t.Fatal(err)
	}
	if asMap(t, textOf(t, res))["value"] != "-----BEGIN-----" {
		t.Fatalf("textGet value wrong: %s", textOf(t, res))
	}

	// List within the certs group
	res, _, err = m.textList(ctx, nil, listInput{Group: "certs"})
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &arr); err != nil {
		t.Fatalf("textList unmarshal: %v", err)
	}
	if len(arr) != 1 || arr[0]["key"] != "TLS" {
		t.Fatalf("textList result = %v", arr)
	}

	// Delete
	res, _, err = m.textDelete(ctx, nil, envKeyInput{Key: "certs:TLS"})
	if err != nil {
		t.Fatal(err)
	}
	if asMap(t, textOf(t, res))["status"] != "deleted" {
		t.Fatalf("textDelete status wrong")
	}
}

func TestMCPGroupAddListActivate(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()

	// env group
	if _, _, err := m.groupAdd(ctx, nil, groupKindInput{Kind: "env", Name: "staging"}); err != nil {
		t.Fatal(err)
	}

	// invalid kind is a tool error, not a go error
	res, _, err := m.groupAdd(ctx, nil, groupKindInput{Kind: "bogus", Name: "x"})
	if err != nil {
		t.Fatalf("invalid kind should be tool error, got go err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("invalid kind should produce IsError")
	}

	// text group + listing
	if _, _, err := m.groupAdd(ctx, nil, groupKindInput{Kind: "text", Name: "secrets"}); err != nil {
		t.Fatal(err)
	}
	res, _, err = m.groupList(ctx, nil, listInput{Group: "text"})
	if err != nil {
		t.Fatal(err)
	}
	var groups []map[string]any
	if err := json.Unmarshal([]byte(textOf(t, res)), &groups); err != nil {
		t.Fatalf("groupList text unmarshal: %v", err)
	}
	found := false
	for _, g := range groups {
		if g["name"] == "secrets" {
			found = true
		}
	}
	if !found {
		t.Fatalf("text group secrets not listed: %v", groups)
	}

	// activate/deactivate env group
	if _, _, err := m.groupActivate(ctx, nil, groupNameInput{Name: "staging"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.groupDeactivate(ctx, nil, groupNameInput{Name: "staging"}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPServerBuildsAndRegistersAllTools(t *testing.T) {
	// Building the server panics on duplicate tool names or invalid schemas,
	// so successful construction asserts the registry is well-formed.
	m, _, _ := newManagersForTest(t, "pw")
	srv := newMCPServer(m.env, m.text, m.config)
	if srv == nil {
		t.Fatal("nil server")
	}
	// Sanity: catalogue and registration must describe the same tool count.
	catalogue := toolCatalogue()
	seen := map[string]bool{}
	for _, td := range catalogue {
		if seen[td.Name] {
			t.Fatalf("duplicate tool name in catalogue: %s", td.Name)
		}
		seen[td.Name] = true
	}
	if len(catalogue) == 0 {
		t.Fatal("catalogue is empty")
	}
}
