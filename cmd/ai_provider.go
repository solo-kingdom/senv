package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
)

// getAIProviderManager 构造已认证的 Provider 管理器（复用既有认证流程）。
func getAIProviderManager() (*llm.ProviderManager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return llm.NewProviderManagerWithKey(auth.storage, auth.key), nil
	}
	return llm.NewProviderManager(auth.storage, auth.password), nil
}

var aiProviderCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage LLM provider profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	providerAddBaseURL string
	providerAddAPIKey  string
	providerAddKeyRef  string
	providerAddCatalog string
	providerAddModels  []string
	providerAddDefault string
	providerAddForce   bool
)

var aiProviderAddCmd = &cobra.Command{
	Use:   "add <alias>",
	Short: "Save an LLM provider profile (credential stored in vault)",
	Long: `Save an LLM provider profile with base URL, credential reference and
model set. The model set is the union of models from --catalog-provider
(models.dev cache) and custom --model values. The credential is provided via
--api-key (stored into the reserved vault text group) or --key-ref (reference
an existing env/text entry).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		res, err := mgr.AddProvider(llm.AddProviderOptions{
			Alias:           args[0],
			BaseURL:         providerAddBaseURL,
			APIKey:          providerAddAPIKey,
			KeyRef:          providerAddKeyRef,
			CatalogPath:     catalogCachePath(),
			CatalogProvider: providerAddCatalog,
			Models:          providerAddModels,
			DefaultModel:    providerAddDefault,
			Force:           providerAddForce,
		})
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "add 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "add")
		for _, w := range res.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
		}
		detail := res.Entry.DefaultModel
		if detail == "" {
			detail = "无"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ 已保存 LLM Provider %s（%d 个模型，默认 %s）\n",
			res.Entry.Alias, len(res.Entry.Models), detail)
		return nil
	},
}

var aiProviderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List LLM provider profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		entries, err := mgr.ListProviders()
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "暂无 LLM Provider，使用 senv ai provider add 添加")
			return nil
		}
		out := cmd.OutOrStdout()
		for _, e := range entries {
			fmt.Fprintf(out, "%s\t%s\t模型数 %d\t默认 %s\t目录 %s\n",
				e.Alias, e.BaseURL, len(e.Models), orDash(e.DefaultModel), orDash(e.CatalogProvider))
		}
		return nil
	},
}

var aiProviderShowCmd = &cobra.Command{
	Use:   "show <alias>",
	Short: "Show one LLM provider profile (never the credential value)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		e, err := mgr.GetProvider(args[0])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "别名：%s\n", e.Alias)
		fmt.Fprintf(out, "Base URL：%s\n", e.BaseURL)
		fmt.Fprintf(out, "凭据引用：%s\n", e.CredentialRef)
		fmt.Fprintf(out, "目录 provider：%s\n", orDash(e.CatalogProvider))
		fmt.Fprintf(out, "模型（%d）：%s\n", len(e.Models), strings.Join(e.Models, ", "))
		fmt.Fprintf(out, "默认模型：%s\n", orDash(e.DefaultModel))
		return nil
	},
}

var aiProviderRemoveCmd = &cobra.Command{
	Use:   "remove <alias>",
	Short: "Remove an LLM provider profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		credRemoved, err := mgr.RemoveProvider(args[0])
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "remove 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+args[0], true, "remove")
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ 已删除 LLM Provider %s\n", args[0])
		if credRemoved {
			fmt.Fprintln(out, "已同时删除其自有凭据条目")
		} else {
			fmt.Fprintln(out, "凭据为外部引用，已保留")
		}
		return nil
	},
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	rootCmd.AddCommand(aiProviderCmd)
	aiProviderAddCmd.Flags().StringVar(&providerAddBaseURL, "base-url", "", "provider base URL (https)")
	aiProviderAddCmd.Flags().StringVar(&providerAddAPIKey, "api-key", "", "API key (stored encrypted in vault)")
	aiProviderAddCmd.Flags().StringVar(&providerAddKeyRef, "key-ref", "", "reference to an existing entry: env:<group>/<key> or text:<group>/<key>")
	aiProviderAddCmd.Flags().StringVar(&providerAddCatalog, "catalog-provider", "", "models.dev provider id for auto model loading")
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModels, "model", nil, "custom model id (repeatable)")
	aiProviderAddCmd.Flags().StringVar(&providerAddDefault, "default-model", "", "default model (must be in the model set)")
	aiProviderAddCmd.Flags().BoolVar(&providerAddForce, "force", false, "overwrite an existing profile")
	aiProviderCmd.AddCommand(aiProviderAddCmd, aiProviderListCmd, aiProviderShowCmd, aiProviderRemoveCmd)
}
