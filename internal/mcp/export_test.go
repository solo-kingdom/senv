package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/storage"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewManager(filepath.Join(dir, "config"), filepath.Join(dir, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize vault: %v", err)
	}
	return NewManager(store, "test-password"), dir
}

func addProfile(t *testing.T, mgr *Manager, alias, command string, env map[string]string) {
	t.Helper()
	err := mgr.Add(&storage.MCPServerEntry{
		Alias:     alias,
		Transport: storage.MCPTransportStdio,
		Command:   command,
		Args:      []string{"-y", "server-" + alias},
		Env:       env,
	})
	if err != nil {
		t.Fatalf("add profile %s: %v", alias, err)
	}
}

func testExporter(t *testing.T, mgr *Manager, dir string, opts ExporterOptions) *Exporter {
	t.Helper()
	opts.Home = dir
	if opts.LedgerPath == "" {
		opts.LedgerPath = filepath.Join(dir, "ledger", LedgerFileName)
	}
	exporter, err := mgr.NewExporter(opts)
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	return exporter
}

func targetFor(t *testing.T, id string) agentcfg.Target {
	t.Helper()
	target, ok := agentcfg.Find(id)
	if !ok {
		t.Fatalf("agent %s not found", id)
	}
	return target
}

func TestExportJSONCreatesMergesAndIsIdempotent(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{"GITHUB_TOKEN": "secret-token"})
	target := targetFor(t, "cursor")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "uvx", "args": []any{"other-server"}},
		},
		"unrelatedKey": true,
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(cfgPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionCreate {
		t.Fatalf("plan items = %+v", plan.Items)
	}
	if !plan.Items[0].Plaintext {
		t.Fatal("plan did not flag plaintext env")
	}

	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures != 0 {
		t.Fatalf("report = %+v", report)
	}

	root, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(root, &parsed); err != nil {
		t.Fatalf("config not valid JSON: %v", err)
	}
	if parsed["unrelatedKey"] != true {
		t.Fatal("unrelated key was dropped")
	}
	servers := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("pre-existing server was dropped")
	}
	github, ok := servers["github"].(map[string]any)
	if !ok {
		t.Fatalf("exported entry missing: %v", servers)
	}
	if github["command"] != "npx" {
		t.Fatalf("command = %v", github["command"])
	}
	env, _ := github["env"].(map[string]any)
	if env["GITHUB_TOKEN"] != "secret-token" {
		t.Fatalf("env = %v", env)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := os.Stat(cfgPath + agentcfg.BackupSuffix); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}

	// The ledger records the fingerprint but never the plaintext value.
	ledgerBytes, err := os.ReadFile(filepath.Join(dir, "ledger", LedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ledgerBytes), "secret-token") {
		t.Fatalf("ledger leaked a secret value:\n%s", ledgerBytes)
	}

	// Re-export is a no-op.
	before, _ := os.Stat(cfgPath)
	plan, err = exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan (second): %v", err)
	}
	if plan.Items[0].Action != ActionSkip || plan.NeedsWrite() {
		t.Fatalf("second plan = %+v", plan.Items)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute (second): %v", err)
	}
	after, _ := os.Stat(cfgPath)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("idempotent export rewrote the config file")
	}
}

func TestExportDriftNeedsForce(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", nil)
	target := targetFor(t, "claude-code")
	cfgPath := target.ResolveConfigPath(dir, "user")

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, _ := exporter.Plan([]agentcfg.Target{target}, nil)
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("initial export: %v", err)
	}

	// Someone edits the exported entry by hand.
	root, err := agentcfg.ReadJSONRoot(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	agentcfg.SetJSONServer(root, target.JSONServersKey, "github", agentcfg.Server{Command: "local-edit"})
	data, _ := agentcfg.EncodeJSON(root)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err = exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Items[0].Action != ActionDrift {
		t.Fatalf("drifted plan = %+v", plan.Items)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	root, _ = agentcfg.ReadJSONRoot(cfgPath)
	if entry, _ := agentcfg.JSONServer(root, target.JSONServersKey, "github"); entry.Command != "local-edit" {
		t.Fatalf("drifted entry was overwritten without --force: %+v", entry)
	}

	// --force takes it back over.
	forced := testExporter(t, mgr, dir, ExporterOptions{Force: true})
	plan, err = forced.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan (force): %v", err)
	}
	if plan.Items[0].Action != ActionUpdate {
		t.Fatalf("forced plan = %+v", plan.Items)
	}
	if _, err := forced.Execute(plan); err != nil {
		t.Fatalf("Execute (force): %v", err)
	}
	root, _ = agentcfg.ReadJSONRoot(cfgPath)
	if entry, _ := agentcfg.JSONServer(root, target.JSONServersKey, "github"); entry.Command != "npx" {
		t.Fatalf("--force did not restore the entry: %+v", entry)
	}
}

func TestExportRejectsForeignEntryWithoutForce(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", nil)
	target := targetFor(t, "kimi")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{
			"github": map[string]any{"command": "someone-elses-binary"},
		},
	}, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Items[0].Action != ActionDrift {
		t.Fatalf("foreign entry plan = %+v", plan.Items)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	root, _ := agentcfg.ReadJSONRoot(cfgPath)
	if entry, _ := agentcfg.JSONServer(root, target.JSONServersKey, "github"); entry.Command != "someone-elses-binary" {
		t.Fatalf("foreign entry was overwritten: %+v", entry)
	}
}

func TestExportTOMLPreservesOtherTables(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{"TOKEN": "abc"})
	target := targetFor(t, "codex")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"uvx\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{`model = "gpt-5"`, "[mcp_servers.other]", "[mcp_servers.github]", `command = "npx"`, "[mcp_servers.github.env]", `TOKEN = "abc"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("codex config missing %q:\n%s", want, text)
		}
	}
}

func TestExportResolveFailureLeavesFileUntouched(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{"TOKEN": "{{env:secrets:MISSING}}"})
	target := targetFor(t, "pi")
	cfgPath := target.ResolveConfigPath(dir, "user")

	resolve := func(string) (string, error) {
		return "", os.ErrNotExist
	}
	exporter := testExporter(t, mgr, dir, ExporterOptions{Resolve: resolve})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Items[0].Action != ActionError {
		t.Fatalf("plan = %+v", plan.Items)
	}
	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("target file was created despite a resolve failure: %v", err)
	}
}

func TestExportPartialFailureKeepsOtherAgents(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", nil)
	good := targetFor(t, "claude-code")
	bad := targetFor(t, "zcode")
	// Make the bad target's config path a directory so the write fails.
	if err := os.MkdirAll(bad.ResolveConfigPath(dir, "user"), 0o755); err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{good, bad}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures != 1 {
		t.Fatalf("failures = %d, want 1 (report %+v)", report.Failures, report.Items)
	}
	if _, err := os.Stat(good.ResolveConfigPath(dir, "user")); err != nil {
		t.Fatalf("successful agent config missing: %v", err)
	}
	// The ledger only records what was actually written.
	ledger := LoadLedger(filepath.Join(dir, "ledger", LedgerFileName))
	if _, ok := ledger.Get(good.ID, "github"); !ok {
		t.Fatal("ledger missing the successful export")
	}
	if _, ok := ledger.Get(bad.ID, "github"); ok {
		t.Fatal("ledger recorded a failed export")
	}
}

func TestLedgerCorruptDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LedgerFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := LoadLedger(path)
	if len(ledger.Warnings()) == 0 {
		t.Fatal("corrupt ledger produced no warning")
	}
	if _, ok := ledger.Get("codex", "github"); ok {
		t.Fatal("corrupt ledger returned a record")
	}
}

func TestUnexportRemovesExportedAndConfirmsChanged(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", nil)
	addProfile(t, mgr, "other", "uvx", nil)
	target := targetFor(t, "claude-code")
	cfgPath := target.ResolveConfigPath(dir, "user")

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Hand-edit one exported entry.
	root, _ := agentcfg.ReadJSONRoot(cfgPath)
	agentcfg.SetJSONServer(root, target.JSONServersKey, "other", agentcfg.Server{Command: "edited"})
	data, _ := agentcfg.EncodeJSON(root)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	unexport, err := exporter.PlanUnexport([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("PlanUnexport: %v", err)
	}
	actions := map[string]string{}
	for _, item := range unexport.Items {
		actions[item.Alias] = item.Action
	}
	if actions["github"] != UnexportRemove || actions["other"] != UnexportChanged {
		t.Fatalf("unexport actions = %v", actions)
	}

	// A nil confirm rejects changed entries; the matching one is removed.
	report, err := exporter.ExecuteUnexport(unexport, nil)
	if err != nil {
		t.Fatalf("ExecuteUnexport: %v", err)
	}
	if report.Failures != 1 {
		t.Fatalf("report = %+v", report.Items)
	}
	root, _ = agentcfg.ReadJSONRoot(cfgPath)
	if _, ok := agentcfg.JSONServer(root, target.JSONServersKey, "github"); ok {
		t.Fatal("unexport left the matching entry behind")
	}
	if entry, ok := agentcfg.JSONServer(root, target.JSONServersKey, "other"); !ok || entry.Command != "edited" {
		t.Fatalf("unexport touched a locally modified entry: %+v", entry)
	}
	// The ledger keeps the entry it could not remove.
	ledger := LoadLedger(filepath.Join(dir, "ledger", LedgerFileName))
	if _, ok := ledger.Get(target.ID, "github"); ok {
		t.Fatal("ledger still records the removed entry")
	}
	if _, ok := ledger.Get(target.ID, "other"); !ok {
		t.Fatal("ledger dropped the entry it could not remove")
	}

	// With confirmation the modified entry is removed too.
	unexport, err = exporter.PlanUnexport([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("PlanUnexport (second): %v", err)
	}
	if _, err := exporter.ExecuteUnexport(unexport, func(UnexportItem) bool { return true }); err != nil {
		t.Fatalf("ExecuteUnexport (confirm): %v", err)
	}
	root, _ = agentcfg.ReadJSONRoot(cfgPath)
	if _, ok := agentcfg.JSONServer(root, target.JSONServersKey, "other"); ok {
		t.Fatal("confirmed unexport did not remove the entry")
	}
}
