package codexdaemon

import (
	"fmt"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
)

func TestMain(m *testing.M) {
	crypto.LowerIterationsForTesting()
	m.Run()
}

// newEnvManager builds an env manager over a fresh temp vault.
func newEnvManager(t *testing.T) *env.Manager {
	t.Helper()
	dir := t.TempDir()
	storeMgr := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return env.NewManager(storeMgr, "test-password")
}

// stubGetter resolves every reference to a fixed value, standing in for the
// text side of the lookup.
type stubGetter struct {
	env  map[string]string
	text map[string]string
}

func (g stubGetter) GetEnvValue(group, key string) (string, error) {
	if v, ok := g.env[group+"/"+key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("env %s/%s not found", group, key)
}

func (g stubGetter) GetTextValue(group, key string) (string, error) {
	if v, ok := g.text[group+"/"+key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("text %s/%s not found", group, key)
}

func TestExportVarsResolvesReferences(t *testing.T) {
	mgr := newEnvManager(t)
	if err := mgr.Set("default", "DEEPSEEK_API_KEY", "{{text:ai-keys:deepseek}}"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("default", "PLAIN", "literal"); err != nil {
		t.Fatal(err)
	}
	getter := stubGetter{text: map[string]string{"ai-keys/deepseek": "sk-resolved"}}

	vars, warnings, err := ExportVars(mgr, getter)
	if err != nil {
		t.Fatalf("ExportVars: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if vars["DEEPSEEK_API_KEY"] != "sk-resolved" {
		t.Errorf("DEEPSEEK_API_KEY = %q, want the resolved text value", vars["DEEPSEEK_API_KEY"])
	}
	if vars["PLAIN"] != "literal" {
		t.Errorf("PLAIN = %q, want literal", vars["PLAIN"])
	}
}

// A reference synced from another machine may be missing locally. Export keeps
// the template and warns, matching `senv env export` — the restart must not fail
// over one unresolved key.
func TestExportVarsKeepsUnresolvedReference(t *testing.T) {
	mgr := newEnvManager(t)
	if err := mgr.Set("default", "MISSING", "{{text:nowhere:key}}"); err != nil {
		t.Fatal(err)
	}

	vars, warnings, err := ExportVars(mgr, stubGetter{})
	if err != nil {
		t.Fatalf("ExportVars: %v", err)
	}
	if vars["MISSING"] != "{{text:nowhere:key}}" {
		t.Errorf("MISSING = %q, want the unresolved template", vars["MISSING"])
	}
	if len(warnings) == 0 {
		t.Fatal("unresolved reference produced no warning")
	}
}

func TestExportVarsOnlyIncludesActiveGroups(t *testing.T) {
	mgr := newEnvManager(t)
	if err := mgr.AddGroup("inactive", "inactive test group"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("inactive", "SHOULD_NOT_EXPORT", "v"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Set("default", "SHOULD_EXPORT", "v"); err != nil {
		t.Fatal(err)
	}

	vars, _, err := ExportVars(mgr, stubGetter{})
	if err != nil {
		t.Fatalf("ExportVars: %v", err)
	}
	if _, ok := vars["SHOULD_NOT_EXPORT"]; ok {
		t.Error("inactive group's variable leaked into the export set")
	}
	if _, ok := vars["SHOULD_EXPORT"]; !ok {
		t.Error("default group's variable missing from the export set")
	}
}

func TestExportVarsEmptyVault(t *testing.T) {
	vars, warnings, err := ExportVars(newEnvManager(t), stubGetter{})
	if err != nil {
		t.Fatalf("ExportVars: %v", err)
	}
	if len(vars) != 0 || len(warnings) != 0 {
		t.Fatalf("empty vault produced vars=%v warnings=%v", vars, warnings)
	}
}
