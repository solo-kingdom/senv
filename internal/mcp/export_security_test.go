package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/storage"
)

func TestResolveEntryRejectsResolvedControlChars(t *testing.T) {
	exp := &Exporter{
		opts: ExporterOptions{
			Resolve: func(v string) (string, error) {
				if v == "{{env:g:BAD_HDR}}" {
					return "Bearer abc\r\nX-Evil: 1", nil
				}
				if v == "{{env:g:BAD_URL}}" {
					return "https://example.com/\n", nil
				}
				return v, nil
			},
		},
	}

	_, _, err := exp.resolveEntry(&storage.MCPServerEntry{
		Alias:     "hdr",
		Transport: storage.MCPTransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   map[string]string{"Authorization": "{{env:g:BAD_HDR}}"},
	})
	if err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("expected header control char error, got: %v", err)
	}

	_, _, err = exp.resolveEntry(&storage.MCPServerEntry{
		Alias:     "u",
		Transport: storage.MCPTransportHTTP,
		URL:       "{{env:g:BAD_URL}}",
	})
	if err == nil || !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("expected url control char error, got: %v", err)
	}
}

func TestUnexportRemovesLingeringBackup(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	store := storage.NewManager(filepath.Join(dir, "config"), filepath.Join(dir, "data"))
	if err := store.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(store, "pw")
	if err := mgr.Add(&storage.MCPServerEntry{
		Alias:     "gh",
		Transport: storage.MCPTransportStdio,
		Command:   "npx",
		Env:       map[string]string{"TOKEN": "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	target, ok := agentcfg.Find("claude-code")
	if !ok {
		t.Fatal("claude-code target missing")
	}
	exp, err := mgr.NewExporter(ExporterOptions{
		Home:       home,
		Scope:      "user",
		LedgerPath: filepath.Join(dir, "ledger", LedgerFileName),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := exp.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exp.Execute(plan); err != nil {
		t.Fatal(err)
	}
	cfgPath := target.ResolveConfigPath(home, "user")
	// Simulate legacy lingering .bak with plaintext secret.
	if err := os.WriteFile(cfgPath+agentcfg.BackupSuffix, []byte("legacy-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	uPlan, err := exp.PlanUnexport([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exp.ExecuteUnexport(uPlan, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfgPath + agentcfg.BackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("expected .bak to be removed after unexport, err=%v", err)
	}
}
