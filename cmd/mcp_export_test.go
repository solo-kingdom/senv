package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/storage"
)

// newMCPExportProject prepares an isolated vault plus a redirected HOME, so
// export targets resolve inside the temp dir.
func newMCPExportProject(t *testing.T) string {
	t.Helper()
	newAuditTestProject(t)
	// newAuditTestProject isolates the session cache by redirecting HOME, so
	// the export home must be set afterwards.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Export writes to pi as well; keep the pi extension install out of the test
	// (agentext reports "unavailable" and no subprocess or network is touched).
	t.Setenv("PATH", t.TempDir())
	resetMCPAddFlags(t)
	mcpAddCommand = "npx"
	mcpAddArgs = []string{"-y", "@modelcontextprotocol/server-github"}
	runSSHCommand(t, mcpAddCmd.RunE(&cobra.Command{}, []string{"github"}))

	mcpExportAgents, mcpExportAll = "", true
	mcpExportDryRun, mcpExportYes, mcpExportPrint, mcpExportForce = false, true, false, false
	mcpExportScope = "user"
	mcpUnexportAgents, mcpUnexportAll = "", true
	mcpUnexportDryRun, mcpUnexportYes, mcpUnexportScope = false, true, "user"
	t.Cleanup(func() {
		mcpExportAgents, mcpExportAll = "", false
		mcpExportDryRun, mcpExportYes, mcpExportPrint, mcpExportForce = false, false, false, false
		mcpExportScope = "user"
		mcpUnexportAgents, mcpUnexportAll = "", false
		mcpUnexportDryRun, mcpUnexportYes, mcpUnexportScope = false, false, "user"
	})
	return dir
}

func TestMCPExportRequiresExplicitTarget(t *testing.T) {
	newMCPExportProject(t)
	mcpExportAll, mcpExportAgents = false, ""
	err := mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no export target") {
		t.Fatalf("error = %v, want an explicit-target error", err)
	}

	mcpExportAgents = "nosuchagent"
	err = mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("error = %v, want an unknown-agent error", err)
	}

	mcpExportAll, mcpExportAgents = true, "codex"
	err = mcpExportCmd.RunE(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error = %v, want a mutual-exclusion error", err)
	}
}

func TestMCPExportDryRunDoesNotWrite(t *testing.T) {
	dir := newMCPExportProject(t)
	mcpExportAll = true
	mcpExportDryRun = true
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "Export plan:") || !strings.Contains(out, "github") {
		t.Fatalf("dry-run output missing the plan:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the codex config: %v", err)
	}
}

func TestMCPExportPrintWritesNothing(t *testing.T) {
	dir := newMCPExportProject(t)
	mcpExportAll, mcpExportPrint, mcpExportDryRun = true, true, false
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "[mcp_servers.github]") || !strings.Contains(out, "mcpServers") {
		t.Fatalf("--print output missing snippets:\n%s", out)
	}
	for _, path := range []string{".codex/config.toml", ".claude.json"} {
		if _, err := os.Stat(filepath.Join(dir, path)); !os.IsNotExist(err) {
			t.Fatalf("--print wrote %s", path)
		}
	}
}

func TestMCPExportAndUnexportRoundTrip(t *testing.T) {
	dir := newMCPExportProject(t)
	out := captureStdout(t, func() {
		runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "create") {
		t.Fatalf("export output missing actions:\n%s", out)
	}

	codexPath := filepath.Join(dir, ".codex", "config.toml")
	codexConfig, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatalf("codex config was not written: %v", err)
	}
	if !strings.Contains(string(codexConfig), "[mcp_servers.github]") {
		t.Fatalf("codex config missing the exported entry:\n%s", codexConfig)
	}
	claudePath := filepath.Join(dir, ".claude.json")
	claudeConfig, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("claude config was not written: %v", err)
	}
	if !strings.Contains(string(claudeConfig), "mcpServers") {
		t.Fatalf("claude config missing mcpServers:\n%s", claudeConfig)
	}

	out = captureStdout(t, func() {
		runSSHCommand(t, mcpUnexportCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "remove") {
		t.Fatalf("unexport output missing actions:\n%s", out)
	}
	codexConfig, err = os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codexConfig), "[mcp_servers.github]") {
		t.Fatalf("unexport left the codex entry behind:\n%s", codexConfig)
	}
	claudeConfig, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(claudeConfig), "github") {
		t.Fatalf("unexport left the claude entry behind:\n%s", claudeConfig)
	}
}

func TestPrintExportPlanMarksUnresolvedRefs(t *testing.T) {
	plan := &mcp.ExportPlan{Items: []mcp.ExportItem{
		{Agent: "pi", Alias: "github", Action: mcp.ActionCreate, Path: "/x/config.toml", Plaintext: true,
			Warnings: []string{`MCP server "github" env TOKEN: unresolved reference {{env:secrets:MISSING}}`}},
		{Agent: "pi", Alias: "clean", Action: mcp.ActionUpdate, Path: "/x/config.toml"},
	}}
	var out bytes.Buffer
	printExportPlanTo(&out, plan)
	if !strings.Contains(out.String(), "[未解析引用]") || strings.Count(out.String(), "[未解析引用]") != 1 {
		t.Errorf("plan should mark exactly one unresolved item:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "[明文]") {
		t.Errorf("plaintext marker missing:\n%s", out.String())
	}

	// 同一 alias 的 warning 被赋给全部 target（--all 场景）：重复文本只打一次
	plan.Items = append(plan.Items, mcp.ExportItem{
		Agent: "codex", Alias: "github", Action: mcp.ActionCreate, Path: "/x/config.toml",
		Warnings: plan.Items[0].Warnings,
	})
	var errOut bytes.Buffer
	printExportItemWarnings(&errOut, plan)
	got := errOut.String()
	if !strings.Contains(got, "⚠ pi/github") || !strings.Contains(got, "env:secrets:MISSING") {
		t.Errorf("stderr warnings missing detail:\n%s", got)
	}
	if strings.Count(got, "env:secrets:MISSING") != 1 {
		t.Errorf("duplicated warning across targets must print once:\n%s", got)
	}
	if strings.Contains(got, "clean") {
		t.Errorf("fully resolved item must not warn:\n%s", got)
	}
}

// TestMCPExportLooseWritesLiteralAndWarns：引用缺失的档案宽松导出——文件写入
// 模板原文、stderr 列出缺失引用、退出成功；与 spec "引用解析失败" 场景一致。
func TestMCPExportLooseWritesLiteralAndWarns(t *testing.T) {
	dir := newMCPExportProject(t)
	mgr, err := getMCPManager()
	if err != nil {
		t.Fatalf("getMCPManager: %v", err)
	}
	if err := mgr.Add(&storage.MCPServerEntry{
		Alias:     "needs-cred",
		Transport: storage.MCPTransportStdio,
		Command:   "npx",
		Env:       map[string]string{"TOKEN": "{{env:secrets:MISSING}}", "PLAIN": "plain-value"},
	}); err != nil {
		t.Fatalf("add profile: %v", err)
	}

	mcpExportAll = true
	mcpExportYes = true
	mcpExportDryRun, mcpExportPrint, mcpExportForce = false, false, false
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			runSSHCommand(t, mcpExportCmd.RunE(&cobra.Command{}, nil))
		})
	})
	if !strings.Contains(stdout, "[未解析引用]") {
		t.Errorf("plan missing unresolved-ref marker:\n%s", stdout)
	}
	for _, want := range []string{"env:secrets:MISSING", "needs-cred"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr warning missing %q:\n%s", want, stderr)
		}
	}
	cfgPath := filepath.Join(dir, ".codex", "config.toml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("codex config not written: %v", err)
	}
	if !strings.Contains(string(data), "{{env:secrets:MISSING}}") {
		t.Errorf("template literal not preserved:\n%s", data)
	}
	if !strings.Contains(string(data), "plain-value") {
		t.Errorf("plain env value not written:\n%s", data)
	}
}

// printTargetRequirements 只对依赖外部扩展的目标出提示，原生读取的目标保持安静。
func TestPrintTargetRequirements(t *testing.T) {
	pi, ok := agentcfg.Find("pi")
	if !ok {
		t.Fatal("pi target missing")
	}
	var buf bytes.Buffer
	printTargetRequirements(&buf, []agentcfg.Target{pi})
	if !strings.Contains(buf.String(), "pi-mcp-adapter") {
		t.Fatalf("pi requirement not printed:\n%s", buf.String())
	}

	cursor, ok := agentcfg.Find("cursor")
	if !ok {
		t.Fatal("cursor target missing")
	}
	buf.Reset()
	printTargetRequirements(&buf, []agentcfg.Target{cursor})
	if buf.Len() != 0 {
		t.Fatalf("native target printed a requirement: %q", buf.String())
	}
}

// 真实安装由 internal/agentext 的 fake-installer 测试覆盖；这里验证 CLI 在
// 扩展安装不可用时照常导出，只多一条提示、不把导出判失败。
func TestExportContinuesWhenPrerequisiteUnavailable(t *testing.T) {
	dir := newMCPExportProject(t) // 内含 PATH 隔离：pi 不在 PATH
	t.Setenv("PI_CODING_AGENT_DIR", "")
	mcpExportAgents, mcpExportAll = "pi", false

	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			if err := mcpExportCmd.RunE(&cobra.Command{}, nil); err != nil {
				t.Errorf("export failed despite the unavailable prerequisite: %v", err)
			}
		})
	})
	if !strings.Contains(stdout, "not on PATH") {
		t.Fatalf("prerequisite notice missing from stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, "not on PATH") {
		t.Fatalf("prerequisite notice belongs on stdout:\n%s", stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".pi", "agent", "mcp.json"))
	if err != nil {
		t.Fatalf("pi config not written despite the failed prerequisite: %v", err)
	}
	if !strings.Contains(string(data), `"senv"`) && !strings.Contains(string(data), "github") {
		t.Fatalf("pi config does not contain the exported profile:\n%s", data)
	}
}
