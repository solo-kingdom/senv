package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
	"golang.org/x/term"
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
	providerAddBaseURL     string
	providerAddAPIKeyStdin bool
	providerAddAllowHTTP   bool
	providerAddKeyRef      string
	providerAddCatalog     string
	providerAddModels      []string
	providerAddDefault     string
	providerAddForce       bool

	// providerCredentialReader is a test seam; production input never becomes
	// a flag value and is dropped when AddProvider returns.
	providerCredentialReader = readProviderCredential
)

var aiProviderAddCmd = &cobra.Command{
	Use:   "add <alias>",
	Short: "Save an LLM provider profile (credential stored in vault)",
	Long: `Save an LLM provider profile with base URL, credential reference and
model set. The model set is the union of models from --catalog-provider
(models.dev cache) and custom --model values. Provide the credential through a
TTY prompt or --api-key-stdin, or reference an existing entry with --key-ref.
The --api-key flag is unsupported because argv and shell history leak secrets.
The base URL is normalized to the OpenAI-compatible shape (a trailing /v1 is
appended when missing, trailing slashes are trimmed) so every agent can derive
its own shape at switch time; the command reports the normalized value.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		var apiKey string
		if providerAddKeyRef == "" {
			credential, err := providerCredentialReader(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			apiKey = string(credential)
		}
		res, err := mgr.AddProvider(llm.AddProviderOptions{
			Alias:           args[0],
			BaseURL:         providerAddBaseURL,
			AllowHTTP:       providerAddAllowHTTP,
			APIKey:          apiKey,
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
		credResult, err := mgr.RemoveProvider(args[0])
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "remove 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+args[0], true, "remove")
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ 已删除 LLM Provider %s\n", args[0])
		if credResult.CredentialRemoved {
			fmt.Fprintln(out, "已同时删除其自有凭据条目")
		} else if credResult.CredentialMissing {
			fmt.Fprintln(out, "自有凭据已不存在，仅删除档案")
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

// readProviderCredential isolates terminal and stdin input from Cobra flag
// binding. The secret is used directly by the provider manager and never
// retained by package state.
func readProviderCredential(stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if providerAddAPIKeyStdin {
		value, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read API key from stdin: %w", err)
		}
		if len(value) == 0 {
			return nil, fmt.Errorf("stdin API key is empty")
		}
		return value, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, fmt.Errorf("credentials are not read from flags: use a TTY prompt or pipe with --api-key-stdin")
	}
	fmt.Fprint(stderr, "API key: ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return nil, fmt.Errorf("read API key prompt: %w", err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("API key is empty")
	}
	return value, nil
}

func init() {
	aiCmd.AddCommand(aiProviderCmd)
	aiProviderAddCmd.Flags().StringVar(&providerAddBaseURL, "base-url", "", "provider base URL (https)")
	aiProviderAddCmd.Flags().BoolVar(&providerAddAPIKeyStdin, "api-key-stdin", false, "read the API key from stdin (no echo, no argv)")
	aiProviderAddCmd.Flags().BoolVar(&providerAddAllowHTTP, "allow-http", false, "explicitly allow an HTTP base URL (default requires HTTPS)")
	aiProviderAddCmd.Flags().StringVar(&providerAddKeyRef, "key-ref", "", "reference to an existing entry: env:<group>/<key> or text:<group>/<key>")
	aiProviderAddCmd.Flags().StringVar(&providerAddCatalog, "catalog-provider", "", "models.dev provider id for auto model loading")
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModels, "model", nil, "custom model id (repeatable)")
	aiProviderAddCmd.Flags().StringVar(&providerAddDefault, "default-model", "", "default model (must be in the model set)")
	aiProviderAddCmd.Flags().BoolVar(&providerAddForce, "force", false, "overwrite an existing profile")
	aiProviderCmd.AddCommand(aiProviderAddCmd, aiProviderListCmd, aiProviderShowCmd, aiProviderRemoveCmd)
}
