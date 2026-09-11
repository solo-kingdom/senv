package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
func agentHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// aiSwitchModelSetHint 是「模型集偏大」提示的阈值：超过时建议用 --models 缩小。
const aiSwitchModelSetHint = 20

var (
	aiSwitchModels       []string
	aiSwitchDefaultModel string
	// aiSwitchModel 只为让已移除的 --model 给出可读错误而注册，不参与写入。
	aiSwitchModel string
)

var aiSwitchCmd = &cobra.Command{
	Use:   "switch <agent> <provider>",
	Short: "Switch a coding agent to a saved LLM provider/model set",
	Long: `Point a coding agent at a saved LLM provider profile: the agent's
native config files are merged in one transaction, rewritten with temporary
backups, and backups are removed after the pointer is committed.

Supported agents: claude-code, codex, kimi, pi, opencode. Codex reads its
credential from an environment variable, so the key never touches its config.

--models picks the Agent model set (= the subset the agent's own model picker
offers); it accepts a comma-separated list and may be repeated, and defaults to
the provider's full model set. --default-model picks the starting model and
defaults to the profile's default. --model is removed.

The provider's base URL is written in the shape the agent's API family
expects: Claude Code without the /v1 suffix (its SDK appends /v1/messages),
OpenAI-compatible agents with it.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 参数校验前置：--model 与非法 --models 不解锁、不写任何文件。
		models, defaultModel, err := parseAISwitchFlags(cmd)
		if err != nil {
			return err
		}
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		home, err := agentHomeDir()
		if err != nil {
			return err
		}
		sm := llm.NewSwitchManager(mgr, agentPointerPath(), home)
		out, err := sm.Switch(args[0], args[1], models, defaultModel)
		if err != nil {
			auditOp(session.AuditOpLLMSwitch, "agent:"+args[0], false, "switch 失败")
			return err
		}
		auditOp(session.AuditOpLLMSwitch, "agent:"+out.AgentID, true,
			fmt.Sprintf("default:%s models:%d", out.DefaultModel, len(out.Models)))
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s → %s（默认模型 %s，共 %d 个模型）\n  接入地址：%s\n  配置：%s\n",
			out.AgentName, out.Provider, out.DefaultModel, len(out.Models), out.BaseURL, out.ConfigPath)
		if len(out.Models) > aiSwitchModelSetHint {
			fmt.Fprintf(cmd.OutOrStdout(),
				"  提示：模型集有 %d 个模型，可用 --models m1,m2 只写入需要的子集\n", len(out.Models))
		}
		if out.CredentialEnv != "" {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"⚠ %s 从环境变量读取凭据（不写入配置文件）：请确保 %s 已设置，可用 senv env 能力在启动该 agent 的环境中暴露\n",
				out.AgentName, out.CredentialEnv)
		}
		for _, w := range out.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
		}
		return nil
	},
}

// parseAISwitchFlags 解析并校验切换参数，任何非法输入都在解锁 vault 之前
// 返回错误：出现 --model 时提示替代用法，显式空模型集直接拒绝。
func parseAISwitchFlags(cmd *cobra.Command) ([]string, string, error) {
	if cmd.Flags().Changed("model") {
		return nil, "", fmt.Errorf("--model 已移除：请改用 --models m1,m2 选择 Agent 模型集、--default-model <model> 指定默认模型")
	}
	var models []string
	if cmd.Flags().Changed("models") {
		for _, raw := range aiSwitchModels {
			model := strings.TrimSpace(raw)
			if model == "" {
				continue
			}
			models = append(models, model)
		}
		if len(models) == 0 {
			return nil, "", fmt.Errorf("--models 不能为空：省略该参数表示全选 Provider 模型集")
		}
	}
	return models, strings.TrimSpace(aiSwitchDefaultModel), nil
}

var aiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show each coding agent's current provider pointer",
	Long: `Show the local (provider, Agent model set, default model) pointer for every
known coding agent. The pointer describes the last switch performed by senv; it
never requires unlocking the vault.

When the vault is already unlocked, a pointer whose models are no longer in the
provider profile is flagged as drifted. Drift is judged against the profile
only — senv never parses agent config files to decide it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := agentHomeDir()
		if err != nil {
			return err
		}
		// 档案只用于漂移提示：拿不到（未解锁/未初始化）时照常展示指针。
		sm := llm.NewSwitchManager(tryGetAIProviderManager(), agentPointerPath(), home)
		rows, warning, err := sm.Status()
		if err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", warning)
		}
		out := cmd.OutOrStdout()
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
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
					when = " " + t.Local().Format("2006-01-02 15:04")
				}
				pointer = fmt.Sprintf("%s / %s（%d 个模型）%s",
					r.Pointer.Provider, r.Pointer.DefaultModel, len(r.Pointer.Models), when)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.AgentID, state, pointer, r.ConfigPath)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		for _, r := range rows {
			if r.Drift != "" {
				fmt.Fprintf(out, "⚠ %s：%s\n", r.AgentID, r.Drift)
			}
		}
		return nil
	},
}

func init() {
	aiSwitchCmd.Flags().StringSliceVar(&aiSwitchModels, "models", nil,
		"Agent model set to write (comma-separated, repeatable; default: the provider's full model set)")
	aiSwitchCmd.Flags().StringVar(&aiSwitchDefaultModel, "default-model", "",
		"starting model for the agent (default: the provider profile's default model)")
	// --model 的旧语义是「只写这一个模型」，与新语义（选定集 + 默认）不同，
	// 静默兼容会让旧脚本在无提示下改变写入结果，因此直接拒绝（grill D8）。
	aiSwitchCmd.Flags().StringVar(&aiSwitchModel, "model", "",
		"removed: use --models and --default-model")
	// 只隐藏不保留 deprecated 警告：错误信息本身已给出替代用法，避免两段提示。
	_ = aiSwitchCmd.Flags().MarkHidden("model")
	aiCmd.AddCommand(aiSwitchCmd)
	aiCmd.AddCommand(aiStatusCmd)
}
