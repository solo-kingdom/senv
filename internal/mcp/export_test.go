package mcp

import (
	"encoding/json"
	"fmt"
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

func addRemoteProfile(t *testing.T, mgr *Manager, alias, transport, url string, headers map[string]string) {
	t.Helper()
	err := mgr.Add(&storage.MCPServerEntry{
		Alias:     alias,
		Transport: transport,
		URL:       url,
		Headers:   headers,
	})
	if err != nil {
		t.Fatalf("add remote profile %s: %v", alias, err)
	}
}

func TestExportRemoteJSON(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportHTTP,
		"https://api.example.com/mcp?key={{env:secrets:KEY}}",
		map[string]string{"Authorization": "Bearer token-1"})
	target := targetFor(t, "claude-code")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "uvx"},
		},
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(cfgPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	resolve := func(value string) (string, error) {
		return strings.ReplaceAll(value, "{{env:secrets:KEY}}", "resolved-key"), nil
	}
	exporter := testExporter(t, mgr, dir, ExporterOptions{Resolve: resolve})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionCreate {
		t.Fatalf("plan items = %+v", plan.Items)
	}
	if !plan.Items[0].Plaintext {
		t.Fatal("remote url/headers were not flagged as plaintext")
	}

	report, err := exporter.Execute(plan)
	if err != nil || report.Failures != 0 {
		t.Fatalf("Execute = %+v, %v", report, err)
	}

	root, err := agentcfg.ReadJSONRoot(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := agentcfg.JSONServer(root, target.JSONServersKey, "web")
	if !ok {
		t.Fatal("remote entry missing after export")
	}
	if entry.Transport != "http" || entry.URL != "https://api.example.com/mcp?key=resolved-key" {
		t.Fatalf("remote entry = %+v", entry)
	}
	if entry.Headers["Authorization"] != "Bearer token-1" {
		t.Fatalf("headers = %v", entry.Headers)
	}
	if entry.Command != "" || len(entry.Env) > 0 {
		t.Fatalf("remote entry must not carry stdio fields: %+v", entry)
	}
	if _, ok := agentcfg.JSONServer(root, target.JSONServersKey, "other"); !ok {
		t.Fatal("sibling server entry was dropped")
	}

	// A second export is a no-op, proving the read-back fingerprint matches.
	plan, err = exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionSkip {
		t.Fatalf("second plan items = %+v", plan.Items)
	}
}

func TestExportRemoteCodexTOML(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportHTTP, "https://api.example.com/mcp", nil)
	target := targetFor(t, "codex")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "[mcp_servers.other]\ncommand = \"uvx\"\n"
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	report, err := exporter.Execute(plan)
	if err != nil || report.Failures != 0 {
		t.Fatalf("Execute = %+v, %v", report, err)
	}

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "url = \"https://api.example.com/mcp\"") {
		t.Fatalf("codex block missing url:\n%s", text)
	}
	webBlock := text[strings.Index(text, "[mcp_servers.web]"):]
	if strings.Contains(webBlock, "command") || strings.Contains(webBlock, "headers") {
		t.Fatalf("codex remote block must not carry command/headers:\n%s", webBlock)
	}
	if !strings.Contains(text, "[mcp_servers.other]") {
		t.Fatalf("existing table was dropped:\n%s", text)
	}
}

func TestExportRemoteHeadersToCodexIsAnError(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportHTTP, "https://api.example.com/mcp",
		map[string]string{"Authorization": "Bearer token-1"})
	codex := targetFor(t, "codex")
	claude := targetFor(t, "claude-code")
	cfgPath := codex.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[mcp_servers.other]\ncommand = \"uvx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{codex, claude}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var codexItem, claudeItem *ExportItem
	for i := range plan.Items {
		switch plan.Items[i].Agent {
		case "codex":
			codexItem = &plan.Items[i]
		case "claude-code":
			claudeItem = &plan.Items[i]
		}
	}
	if codexItem == nil || codexItem.Action != ActionError || !strings.Contains(codexItem.Reason, "headers") {
		t.Fatalf("codex plan item = %+v", codexItem)
	}
	if claudeItem == nil || claudeItem.Action != ActionCreate {
		t.Fatalf("claude-code plan item = %+v", claudeItem)
	}

	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures == 0 {
		t.Fatal("expected failures for the codex target")
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("codex config changed despite the error:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), ".claude.json")); !os.IsNotExist(err) {
		// claude-code lives under a different path; just ensure no crash above.
		_ = err
	}
}

func TestExportRemoteUnsupportedTargetIsPlanError(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportSSE, "https://api.example.com/sse", nil)
	target := targetFor(t, "claude-desktop")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{"mcpServers": map[string]any{"other": map[string]any{"command": "uvx"}}}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(cfgPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	exporter := testExporter(t, mgr, dir, ExporterOptions{})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionError {
		t.Fatalf("plan items = %+v", plan.Items)
	}
	if plan.Items[0].Reason == "" {
		t.Fatal("error item carries no reason")
	}
	report, err := exporter.Execute(plan)
	if err != nil || report.Failures == 0 {
		t.Fatalf("Execute = %+v, %v; want failures", report, err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("claude-desktop config changed despite the error:\n%s", after)
	}
}

func TestExportRemoteResolveFailureLeavesFileUntouched(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportHTTP, "https://api.example.com/mcp?key={{env:secrets:MISSING}}", nil)
	target := targetFor(t, "cursor")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("{\"mcpServers\":{}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	resolve := func(value string) (string, error) {
		return "", fmt.Errorf("reference not found: %s", value)
	}
	exporter := testExporter(t, mgr, dir, ExporterOptions{Resolve: resolve})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionError {
		t.Fatalf("plan items = %+v", plan.Items)
	}
	report, err := exporter.Execute(plan)
	if err != nil || report.Failures == 0 {
		t.Fatalf("Execute = %+v, %v; want failures", report, err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("cursor config changed despite the resolve failure")
	}
}

func TestUnexportRemoteEntry(t *testing.T) {
	mgr, dir := newTestManager(t)
	addRemoteProfile(t, mgr, "web", storage.MCPTransportHTTP, "https://api.example.com/mcp", nil)
	target := targetFor(t, "cursor")
	cfgPath := target.ResolveConfigPath(dir, "user")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := map[string]any{"mcpServers": map[string]any{"other": map[string]any{"command": "uvx"}}}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(cfgPath, append(data, '\n'), 0o600); err != nil {
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

	unexportPlan, err := exporter.PlanUnexport([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("PlanUnexport: %v", err)
	}
	if len(unexportPlan.Items) != 1 || unexportPlan.Items[0].Action != UnexportRemove {
		t.Fatalf("unexport plan = %+v", unexportPlan.Items)
	}
	report, err := exporter.ExecuteUnexport(unexportPlan, nil)
	if err != nil || report.Failures != 0 {
		t.Fatalf("ExecuteUnexport = %+v, %v", report, err)
	}
	root, _ := agentcfg.ReadJSONRoot(cfgPath)
	if _, ok := agentcfg.JSONServer(root, target.JSONServersKey, "web"); ok {
		t.Fatal("remote entry survived unexport")
	}
	if _, ok := agentcfg.JSONServer(root, target.JSONServersKey, "other"); !ok {
		t.Fatal("sibling entry was dropped by unexport")
	}
}

// TestExportLooseResolvesUnresolvedReferences：宽松模式下引用缺失保留模板
// 原文写入并出 warning，不终止写入；补齐凭据后重跑按漂移语义处理。
func TestExportLooseResolvesUnresolvedReferences(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{
		"TOKEN": "{{env:secrets:MISSING}}",
		"OKVAR": "plain",
	})
	target := targetFor(t, "pi")
	cfgPath := target.ResolveConfigPath(dir, "user")

	resolved := 0
	resolveLoose := func(value string) (string, []string, error) {
		if value == "{{env:secrets:MISSING}}" {
			resolved++
			return value, []string{"unresolved reference {{env:secrets:MISSING}}: env group 'secrets' key 'MISSING' not found"}, nil
		}
		resolved++
		return value, nil, nil
	}
	exporter := testExporter(t, mgr, dir, ExporterOptions{ResolveLoose: resolveLoose})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Items[0].Action != ActionCreate {
		t.Fatalf("loose mode should still create: %+v", plan.Items[0])
	}
	if len(plan.Items[0].Warnings) != 1 || !strings.Contains(plan.Items[0].Warnings[0], "env:secrets:MISSING") {
		t.Fatalf("warnings = %+v", plan.Items[0].Warnings)
	}
	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures != 0 {
		t.Fatalf("loose mode must not fail: %+v", report)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("target file missing: %v", err)
	}
	if !strings.Contains(string(data), "{{env:secrets:MISSING}}") {
		t.Errorf("template literal not preserved:\n%s", data)
	}
	if !strings.Contains(string(data), "plain") {
		t.Errorf("resolvable value not written:\n%s", data)
	}
	if resolved < 2 {
		t.Errorf("resolver invoked %d times, want >= 2", resolved)
	}
}

// TestExportLooseHardErrorStillPoisonsTarget：宽松模式只放宽"引用缺失"；
// 解析器自身错误（如引用环）仍是 plan 级错误，目标不动。
func TestExportLooseHardErrorStillPoisonsTarget(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{"TOKEN": "{{env:secrets:MISSING}}"})
	target := targetFor(t, "pi")
	cfgPath := target.ResolveConfigPath(dir, "user")

	resolveLoose := func(string) (string, []string, error) {
		return "", nil, fmt.Errorf("reference cycle detected")
	}
	exporter := testExporter(t, mgr, dir, ExporterOptions{ResolveLoose: resolveLoose})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Items[0].Action != ActionError {
		t.Fatalf("hard resolve error must stay an error: %+v", plan.Items[0])
	}
	report, err := exporter.Execute(plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Failures != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("target file was created despite a hard resolve failure: %v", err)
	}
}

// TestExportLooseFullyResolvedNoWarnings：引用齐全时宽松路径与严格路径结果一致（无 warning）。
func TestExportLooseFullyResolvedNoWarnings(t *testing.T) {
	mgr, dir := newTestManager(t)
	addProfile(t, mgr, "github", "npx", map[string]string{"TOKEN": "static"})
	target := targetFor(t, "pi")
	cfgPath := target.ResolveConfigPath(dir, "user")

	resolveLoose := func(value string) (string, []string, error) { return value, nil, nil }
	exporter := testExporter(t, mgr, dir, ExporterOptions{ResolveLoose: resolveLoose})
	plan, err := exporter.Plan([]agentcfg.Target{target}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items[0].Warnings) != 0 {
		t.Fatalf("warnings = %+v, want empty", plan.Items[0].Warnings)
	}
	if _, err := exporter.Execute(plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(data), "static") {
		t.Fatalf("resolved write missing: %v\n%s", err, data)
	}
}
