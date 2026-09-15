package cmd

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func TestResolveExportShellLoosePreservesStaleRef(t *testing.T) {
	dir := t.TempDir()
	cfg := dir + "/config"
	data := dir + "/data"
	store := storage.NewManager(cfg, data)
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("init: %v", err)
	}
	em := env.NewManager(store, "test-password")
	tm := text.NewManager(store, "test-password")

	if err := em.Set("default", "GOOD", "ok"); err != nil {
		t.Fatalf("set GOOD: %v", err)
	}
	if err := em.Set("default", "BAD", "{{text:llm-keys:missing}}"); err != nil {
		t.Fatalf("set BAD: %v", err)
	}

	shell, warnings, err := resolveExportShell(em, tm, "default")
	if err != nil {
		t.Fatalf("resolveExportShell: %v", err)
	}
	if !strings.Contains(shell, "export GOOD='ok'") {
		t.Fatalf("shell missing GOOD: %q", shell)
	}
	if !strings.Contains(shell, "export BAD='{{text:llm-keys:missing}}'") {
		t.Fatalf("shell missing preserved BAD ref: %q", shell)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "BAD") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestResolveExportShellStrictOnCycle(t *testing.T) {
	dir := t.TempDir()
	cfg := dir + "/config"
	data := dir + "/data"
	store := storage.NewManager(cfg, data)
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("init: %v", err)
	}
	em := env.NewManager(store, "test-password")
	tm := text.NewManager(store, "test-password")

	if err := em.Set("default", "A", "{{env:default:B}}"); err != nil {
		t.Fatalf("set A: %v", err)
	}
	if err := em.Set("default", "B", "{{env:default:A}}"); err != nil {
		t.Fatalf("set B: %v", err)
	}

	_, _, err := resolveExportShell(em, tm, "default")
	if err == nil || !strings.Contains(err.Error(), "circular reference") {
		t.Fatalf("expected circular reference error, got %v", err)
	}
}

func TestMCPExportLooseWithStaleRef(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := t.Context()
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "default:GOOD", Value: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.envSet(ctx, nil, envSetValueInput{Key: "default:STALE", Value: "{{text:llm-keys:missing}}"}); err != nil {
		t.Fatal(err)
	}

	res, _, err := m.envExport(ctx, nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	body := asMap(t, textOf(t, res))
	exports, _ := body["exports"].(string)
	if !strings.Contains(exports, "GOOD='ok'") || !strings.Contains(exports, "STALE='{{text:llm-keys:missing}}'") {
		t.Fatalf("exports = %q", exports)
	}
	warnings, ok := body["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %v", body["warnings"])
	}
	if !strings.Contains(warnings[0].(string), "STALE") {
		t.Fatalf("warning text = %v", warnings[0])
	}
}
