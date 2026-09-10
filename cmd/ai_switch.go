package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
)

// agentPointerPath 返回 coding agent 指针文件路径。指针是本机状态，
// 放 config 目录而非 vault data 目录，不随同步分发。
func agentPointerPath() string {
	return filepath.Join(getConfigPath(), "agent-pointers.json")
}

// agentHomeDir 返回 agent 配置写入的 home 目录（测试可经 HOME 覆盖）。
func agentHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

var aiSwitchModel string

var aiSwitchCmd = &cobra.Command{
	Use:   "switch <agent> <provider>",
	Short: "Switch a coding agent to a saved LLM provider/model",
	Long: `Point a coding agent at a saved LLM provider profile: the agent's
native config file is merged and rewritten atomically (a .senv-bak backup is
kept), and the local pointer records the new (provider, model).

Supported agents: claude-code, codex, kimi, pi, opencode. Codex reads its
credential from an environment variable, so the key never touches its config.

Omitting --model uses the provider's default model when unambiguous.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		sm := llm.NewSwitchManager(mgr, agentPointerPath(), agentHomeDir())
		out, err := sm.Switch(args[0], args[1], aiSwitchModel)
		if err != nil {
			auditOp(session.AuditOpLLMSwitch, "agent:"+args[0], false, "switch 失败")
			return err
		}
		auditOp(session.AuditOpLLMSwitch, "agent:"+out.AgentID, true,
			"provider:"+out.Provider+" model:"+out.Model)
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s → %s（模型 %s）\n  配置：%s\n",
			out.AgentName, out.Provider, out.Model, out.ConfigPath)
		if out.CredentialEnv != "" {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"⚠ %s 从环境变量读取凭据（不写入配置文件）：请确保 %s 已设置，可用 senv env 能力在启动该 agent 的环境中暴露\n",
				out.AgentName, out.CredentialEnv)
		}
		return nil
	},
}

var aiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show each coding agent's current provider pointer",
	Long: `Show the local (provider, model) pointer for every known coding agent.
The pointer describes the last switch performed by senv; it never requires
unlocking the vault.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sm := llm.NewSwitchManager(nil, agentPointerPath(), agentHomeDir())
		rows, warning := sm.Status()
		if warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", warning)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "AGENT\tSTATE\tPROVIDER/MODEL\tCONFIG")
		for _, r := range rows {
			state := "未切换"
			pointer := "-"
			if !r.Supported {
				state = "不支持"
			} else if r.Pointer != nil {
				state = "已切换"
				when := ""
				if t, err := r.Pointer.SwitchedAtTime(); err == nil {
					when = t.Local().Format("2006-01-02 15:04")
				}
				pointer = r.Pointer.Provider + " / " + r.Pointer.Model + "（" + when + "）"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.AgentID, state, pointer, r.ConfigPath)
		}
		return w.Flush()
	},
}

func init() {
	aiSwitchCmd.Flags().StringVar(&aiSwitchModel, "model", "",
		"model to switch to (defaults to the provider's default model)")
	aiCmd.AddCommand(aiSwitchCmd)
	aiCmd.AddCommand(aiStatusCmd)
}
