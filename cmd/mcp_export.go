package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/session"
)

var (
	mcpExportAgents string
	mcpExportAll    bool
	mcpExportDryRun bool
	mcpExportYes    bool
	mcpExportPrint  bool
	mcpExportForce  bool
	mcpExportScope  string
)

// resolveMCPTargets picks the export targets. The target set is always
// explicit: writing into several agent configs is not something a bare
// command should decide on its own.
func resolveMCPTargets(all bool, agentList string) ([]agentcfg.Target, error) {
	if all && strings.TrimSpace(agentList) != "" {
		return nil, fmt.Errorf("pass either --all or --agent, not both")
	}
	if !all && strings.TrimSpace(agentList) == "" {
		return nil, fmt.Errorf("no export target: pass --agent <%s> or --all", strings.Join(agentcfg.IDs(), "|"))
	}
	if all {
		return agentcfg.Supported(), nil
	}
	var targets []agentcfg.Target
	seen := map[string]bool{}
	for _, raw := range strings.Split(agentList, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		target, ok := agentcfg.Find(id)
		if !ok {
			return nil, fmt.Errorf("unknown agent %q; supported: %s", id, strings.Join(agentcfg.IDs(), ", "))
		}
		if seen[target.ID] {
			continue
		}
		seen[target.ID] = true
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no export target: pass --agent <%s> or --all", strings.Join(agentcfg.IDs(), "|"))
	}
	return targets, nil
}

// mcpExporter builds an exporter that resolves {{env:...}} references through
// the already-authenticated env and text managers. 档案跨机同步后引用目标可能
// 尚未在本机，因此走宽松模式：未解析引用保留模板原文写入并逐条 warning，
// 不再终止该 agent 的写入（严格语义仅保留给解析器自身错误，如引用环）。
func mcpExporter(scope string) (*mcp.Exporter, error) {
	mgr, err := getMCPManager()
	if err != nil {
		return nil, err
	}
	envMgr, err := getEnvManager()
	if err != nil {
		return nil, err
	}
	textMgr, err := getTextManager()
	if err != nil {
		return nil, err
	}
	getter := &combinedGetter{envManager: envMgr, textManager: textMgr}
	resolveLoose := func(value string) (string, []string, error) {
		return ref.ResolveWithWarnings(value, getter, ref.ResolveOptions{Loose: true})
	}
	return mgr.NewExporter(mcp.ExporterOptions{
		Scope:        scope,
		Force:        mcpExportForce,
		ResolveLoose: resolveLoose,
		LedgerPath:   mcp.LedgerPathForConfigDir(getConfigPath()),
	})
}

var mcpExportCmd = &cobra.Command{
	Use:   "export [alias...]",
	Short: "Export MCP server profiles into agent configs",
	Long: `Merge stored MCP server profiles into the global config files of the
selected coding agents. Always shows a plan first: the plan names every entry
that would be written in clear text, and writes happen only after confirmation.

Entries senv did not write (or that changed after an export) are left alone
unless --force is given. Files are backed up to <file>.bak before a write.

Examples:
  senv mcp export --all --dry-run          # show the plan, write nothing
  senv mcp export --agent codex,cursor     # confirm, then write
  senv mcp export --all --print            # print snippets only
  senv mcp export --all --force            # overwrite drifted entries`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, err := resolveMCPTargets(mcpExportAll, mcpExportAgents)
		if err != nil {
			return err
		}
		exporter, err := mcpExporter(mcpExportScope)
		if err != nil {
			return err
		}
		printLedgerWarnings(exporter.Ledger())

		plan, err := exporter.Plan(targets, args)
		if err != nil {
			return err
		}
		printExportPlan(plan)
		printExportItemWarnings(cmd.ErrOrStderr(), plan)
		if mcpExportPrint {
			printExportSnippets(plan)
			return nil
		}
		if mcpExportDryRun {
			return nil
		}
		if !plan.NeedsWrite() {
			fmt.Println("Nothing to write.")
			return nil
		}
		if !mcpExportYes && !confirmPrompt("应用以上导出计划？(y/N): ") {
			fmt.Println("已取消导出")
			return nil
		}

		report, execErr := exporter.Execute(plan)
		printExportReport(report)
		auditOp(session.AuditOpMCPExport, mcpExportTargetName(targets, args), report.Failures == 0, fmt.Sprintf("export %d 项", len(report.Items)))
		if execErr != nil {
			return execErr
		}
		if report.Failures > 0 {
			return fmt.Errorf("%d 项导出失败", report.Failures)
		}
		return nil
	},
}

var (
	mcpUnexportAgents string
	mcpUnexportAll    bool
	mcpUnexportDryRun bool
	mcpUnexportYes    bool
	mcpUnexportScope  string
)

var mcpUnexportCmd = &cobra.Command{
	Use:   "unexport [alias...]",
	Short: "Remove exported MCP server entries from agent configs",
	Long: `Remove entries senv previously exported into agent configs, using the
local ledger to find them. Entries that match what senv wrote are removed
directly; entries modified afterwards require per-item confirmation. Stored
profiles are never touched.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, err := resolveMCPTargets(mcpUnexportAll, mcpUnexportAgents)
		if err != nil {
			return err
		}
		exporter, err := mcpExporter(mcpUnexportScope)
		if err != nil {
			return err
		}
		printLedgerWarnings(exporter.Ledger())

		plan, err := exporter.PlanUnexport(targets, args)
		if err != nil {
			return err
		}
		printUnexportPlan(plan)
		if mcpUnexportDryRun {
			return nil
		}
		if !plan.NeedsWrite() {
			fmt.Println("Nothing to remove.")
			return nil
		}
		if !mcpUnexportYes && !confirmPrompt("应用以上撤回计划？(y/N): ") {
			fmt.Println("已取消撤回")
			return nil
		}

		confirmChanged := func(item mcp.UnexportItem) bool {
			return confirmPrompt(fmt.Sprintf("条目 %s/%s 已被本地修改，确认删除？(y/N): ", item.Agent, item.Alias))
		}
		report, execErr := exporter.ExecuteUnexport(plan, confirmChanged)
		printExportReport(report)
		auditOp(session.AuditOpMCPExport, mcpExportTargetName(targets, args), report.Failures == 0, fmt.Sprintf("unexport %d 项", len(report.Items)))
		if execErr != nil {
			return execErr
		}
		if report.Failures > 0 {
			return fmt.Errorf("%d 项撤回失败", report.Failures)
		}
		return nil
	},
}

func mcpExportTargetName(targets []agentcfg.Target, aliases []string) string {
	name := "agents:" + mcp.TargetIDs(targets)
	if len(aliases) > 0 {
		name += " alias:" + strings.Join(aliases, ",")
	}
	return name
}

func printLedgerWarnings(ledger *mcp.Ledger) {
	for _, warning := range ledger.Warnings() {
		fmt.Printf("⚠ %s\n", warning)
	}
}

func printExportPlan(plan *mcp.ExportPlan) {
	printExportPlanTo(os.Stdout, plan)
}

func printExportPlanTo(w io.Writer, plan *mcp.ExportPlan) {
	fmt.Fprintln(w, "Export plan:")
	for _, item := range plan.Items {
		line := fmt.Sprintf("  %-16s %-20s %-8s %s", item.Agent, item.Alias, item.Action, item.Path)
		if item.Plaintext && (item.Action == mcp.ActionCreate || item.Action == mcp.ActionUpdate) {
			line += "  [明文]"
		}
		if len(item.Warnings) > 0 {
			line += "  [未解析引用]"
		}
		if item.Reason != "" {
			line += "  — " + item.Reason
		}
		fmt.Fprintln(w, line)
	}
}

// printExportItemWarnings 把宽松模式下未解析引用的 warning 写到 stderr：
// 档案已写入但引用目标缺失，用户需补齐后重跑导出。
func printExportItemWarnings(w io.Writer, plan *mcp.ExportPlan) {
	for _, item := range plan.Items {
		for _, warning := range item.Warnings {
			fmt.Fprintf(w, "⚠ %s/%s %s\n", item.Agent, item.Alias, warning)
		}
	}
}

func printExportSnippets(plan *mcp.ExportPlan) {
	for _, item := range plan.Items {
		server := item.Server()
		if item.Action != mcp.ActionCreate && item.Action != mcp.ActionUpdate {
			continue
		}
		target, ok := agentcfg.Find(item.Agent)
		if !ok {
			continue
		}
		fmt.Printf("\n# %s — add to %s\n", item.AgentName, item.Path)
		switch target.Format {
		case agentcfg.FormatJSON:
			entry, err := json.MarshalIndent(map[string]any{
				target.JSONServersKey: map[string]any{item.Alias: jsonEntry(server)},
			}, "", "  ")
			if err != nil {
				fmt.Printf("# render failed: %v\n", err)
				continue
			}
			fmt.Printf("%s\n", entry)
		case agentcfg.FormatTOML:
			fmt.Print(agentcfg.RenderTOMLServerBlock(target.TOMLTableName, item.Alias, server))
		}
	}
}

func jsonEntry(server agentcfg.Server) map[string]any {
	return agentcfg.JSONEntry(server)
}

func printUnexportPlan(plan *mcp.UnexportPlan) {
	fmt.Println("Unexport plan:")
	for _, item := range plan.Items {
		line := fmt.Sprintf("  %-16s %-20s %-8s %s", item.Agent, item.Alias, item.Action, item.Path)
		if item.Reason != "" {
			line += "  — " + item.Reason
		}
		fmt.Println(line)
	}
}

func printExportReport(report mcp.ExportReport) {
	items := report.Items
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Agent != items[j].Agent {
			return items[i].Agent < items[j].Agent
		}
		return items[i].Alias < items[j].Alias
	})
	fmt.Println("Result:")
	for _, item := range items {
		line := fmt.Sprintf("  %-16s %-20s %-8s", item.Agent, item.Alias, item.Action)
		if item.Reason != "" {
			line += "  — " + item.Reason
		}
		fmt.Println(line)
	}
}

func init() {
	mcpCmd.AddCommand(mcpExportCmd)
	mcpCmd.AddCommand(mcpUnexportCmd)

	addTargetFlags := func(cmd *cobra.Command, agents *string, all *bool, scope *string) {
		cmd.Flags().StringVar(agents, "agent", "", "comma-separated agent ids to target")
		cmd.Flags().BoolVar(all, "all", false, "target every supported agent")
		cmd.Flags().StringVar(scope, "scope", "user", "config scope: user or project (project only honored by some agents)")
	}
	addTargetFlags(mcpExportCmd, &mcpExportAgents, &mcpExportAll, &mcpExportScope)
	mcpExportCmd.Flags().BoolVar(&mcpExportDryRun, "dry-run", false, "print the plan without writing")
	mcpExportCmd.Flags().BoolVar(&mcpExportYes, "yes", false, "skip the confirmation prompt")
	mcpExportCmd.Flags().BoolVar(&mcpExportPrint, "print", false, "print config snippets instead of writing")
	mcpExportCmd.Flags().BoolVar(&mcpExportForce, "force", false, "overwrite entries senv did not write, or that changed")

	addTargetFlags(mcpUnexportCmd, &mcpUnexportAgents, &mcpUnexportAll, &mcpUnexportScope)
	mcpUnexportCmd.Flags().BoolVar(&mcpUnexportDryRun, "dry-run", false, "print the plan without changing files")
	mcpUnexportCmd.Flags().BoolVar(&mcpUnexportYes, "yes", false, "skip the confirmation prompt")
}
