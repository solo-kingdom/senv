package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/tree"
)

func newTreeManagersForTest(t *testing.T) *managers {
	t.Helper()
	tmp := t.TempDir()
	store := storage.NewManager(tmp+"/config", tmp+"/data")
	if err := store.Initialize("pw"); err != nil {
		t.Fatalf("init: %v", err)
	}
	key, err := store.DeriveKeyFromPassword("pw")
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	tm := tree.NewManager(store, key)
	if err := tm.AddGroup("deploy", "deploy trees"); err != nil {
		t.Fatalf("add group: %v", err)
	}
	return &managers{tree: tm}
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func TestMCPTreeCatalogue(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range toolCatalogue() {
		names[tl.Name] = true
	}
	for _, want := range []string{"senv_tree_ls", "senv_tree_get", "senv_tree_set"} {
		if !names[want] {
			t.Fatalf("catalogue missing %s", want)
		}
	}
	for _, banned := range []string{"senv_tree_rm", "senv_tree_pull", "senv_tree_push", "senv_tree_delete"} {
		if names[banned] {
			t.Fatalf("tree must not expose %s over MCP", banned)
		}
	}
}

func TestMCPTreeSetGetLsAndSensitiveRefusal(t *testing.T) {
	m := newTreeManagersForTest(t)
	ctx := context.Background()

	res, _, err := m.treeSet(ctx, nil, treeSetInput{Group: "deploy", Path: "tcbj/caddy/Caddyfile", Content: "example.com {}\n"})
	if err != nil || res.IsError {
		t.Fatalf("treeSet: err=%v res=%s", err, toolText(t, res))
	}
	res, _, _ = m.treeGet(ctx, nil, treeGetInput{Group: "deploy", Path: "tcbj/caddy/Caddyfile"})
	if res.IsError || !strings.Contains(toolText(t, res), "example.com") {
		t.Fatalf("treeGet unexpected: %s", toolText(t, res))
	}
	res, _, _ = m.treeLs(ctx, nil, treeLsInput{Group: "deploy", Prefix: "tcbj"})
	if res.IsError || !strings.Contains(toolText(t, res), "tcbj/caddy/Caddyfile") || strings.Contains(toolText(t, res), "example.com") {
		t.Fatalf("treeLs must list paths without content: %s", toolText(t, res))
	}

	for _, p := range []string{".env", "tcbj/.env.prod", "keys/id.pem", "keys/x.key"} {
		res, _, _ = m.treeSet(ctx, nil, treeSetInput{Group: "deploy", Path: p, Content: "SECRET=1"})
		if !res.IsError {
			t.Fatalf("treeSet must refuse sensitive path %q", p)
		}
	}
	res, _, _ = m.treeSet(ctx, nil, treeSetInput{Group: "deploy", Path: "../escape", Content: "x"})
	if !res.IsError {
		t.Fatal("treeSet must refuse path traversal")
	}
}
