package storage

import (
	"testing"
)

func TestTreeStorageRoundTripAndCollisions(t *testing.T) {
	m, _ := setupTestManager(t)
	key, err := m.DeriveKeyFromPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SaveTreeGroupMeta("deploy", "Deployment manifests", key); err != nil {
		t.Fatalf("SaveTreeGroupMeta: %v", err)
	}

	type payload struct {
		Content string `json:"content"`
	}
	if err := m.SaveTreeJSON("deploy", "tcbj/caddy/Caddyfile", payload{Content: "example.com"}, key); err != nil {
		t.Fatalf("SaveTreeJSON: %v", err)
	}
	if err := m.SaveTreeJSON("deploy", "BACKUP.md", payload{Content: "# backup"}, key); err != nil {
		t.Fatalf("SaveTreeJSON root: %v", err)
	}

	// Collision: ancestor file vs descendant
	if err := m.SaveTreeJSON("deploy", "BACKUP.md/child", payload{Content: "x"}, key); err == nil {
		t.Fatal("expected collision error when ancestor is already a file")
	}
	// Collision: existing directory vs file
	if err := m.SaveTreeJSON("deploy", "tcbj/caddy", payload{Content: "x"}, key); err == nil {
		t.Fatal("expected collision error when path is already a directory")
	}

	var got payload
	if err := m.LoadTreeJSON("deploy", "tcbj/caddy/Caddyfile", &got, key); err != nil {
		t.Fatalf("LoadTreeJSON: %v", err)
	}
	if got.Content != "example.com" {
		t.Fatalf("got %q, want example.com", got.Content)
	}

	paths, err := m.ListTreePaths("deploy")
	if err != nil {
		t.Fatalf("ListTreePaths: %v", err)
	}
	if len(paths) != 2 || paths[0] != "BACKUP.md" || paths[1] != "tcbj/caddy/Caddyfile" {
		t.Fatalf("unexpected paths: %v", paths)
	}

	if err := m.DeleteTreeFile("deploy", "tcbj/caddy/Caddyfile"); err != nil {
		t.Fatalf("DeleteTreeFile: %v", err)
	}
	// After deleting, tcbj/caddy should be pruned so writing a file there succeeds.
	if err := m.SaveTreeJSON("deploy", "tcbj/caddy", payload{Content: "now a file"}, key); err != nil {
		t.Fatalf("expected pruned dir to allow file write: %v", err)
	}
}
