package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
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

// tryGetAIProviderManager 只在无需交互的前提下解析 vault（进程内 memo 或
// 有效会话缓存），拿不到时返回 nil 而不提示密码。供 `senv ai status` 这类
// 「有档案才好、没档案也能用」的只读命令做增强信息（漂移提示）。
func tryGetAIProviderManager() *llm.ProviderManager {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), func(string) (string, error) {
		return "", ErrNeedSession
	})
	if err != nil {
		return nil
	}
	if auth.hasKey() {
		return llm.NewProviderManagerWithKey(auth.storage, auth.key)
	}
	return llm.NewProviderManager(auth.storage, auth.password)
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
	providerAddBaseURL            string
	providerAddAPIKeyStdin        bool
	providerAddAllowHTTP          bool
	providerAddKeyRef             string
	providerAddCatalog            string
	providerAddModels             []string
	providerAddModelCtx           []string
	providerAddModelOut           []string
	providerAddModelReason        []string
	providerAddModelDefaultReason []string
	providerAddDefaultReasoning   string
	providerAddModelModalities    []string
	providerAddDefault            string
	providerAddAPIShape           string
	providerAddForce              bool

	providerEditBaseURL            string
	providerEditAPIKeyStdin        bool
	providerEditRotateKey          bool
	providerEditAllowHTTP          bool
	providerEditKeyRef             string
	providerEditCatalog            string
	providerEditModels             []string
	providerEditModelCtx           []string
	providerEditModelOut           []string
	providerEditModelReason        []string
	providerEditModelDefaultReason []string
	providerEditDefaultReasoning   string
	providerEditModelModalities    []string
	providerEditDefault            string
	providerEditAPIShape           string

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
Every model must resolve a context window: catalog models read models.dev
limit.context, and custom models require --model-context <model>=<tokens>.
The --api-key flag is unsupported because argv and shell history leak secrets.
The base URL is normalized to the OpenAI-compatible shape (a trailing /v1 is
appended when missing, trailing slashes are trimmed) so every agent can derive
its own shape at switch time; the command reports the normalized value.
--api-shape optionally declares the wire protocol (openai-chat | openai-responses
| anthropic); leave it empty to keep deriving the shape from the target agent.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelContexts, err := llm.ParseModelContexts(providerAddModelCtx)
		if err != nil {
			return err
		}
		modelOutputs, err := llm.ParseModelOutputs(providerAddModelOut)
		if err != nil {
			return err
		}
		modelReasoning, err := llm.ParseModelReasoning(providerAddModelReason)
		if err != nil {
			return err
		}
		modelDefaultReasoning, err := llm.ParseModelDefaultReasoning(providerAddModelDefaultReason)
		if err != nil {
			return err
		}
		modelModalities, err := llm.ParseModelModalities(providerAddModelModalities)
		if err != nil {
			return err
		}
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		var apiKey string
		if providerAddKeyRef == "" {
			credential, err := providerCredentialReader(cmd.InOrStdin(), cmd.ErrOrStderr(), providerAddAPIKeyStdin)
			if err != nil {
				return err
			}
			apiKey = string(credential)
		}
		res, err := mgr.AddProvider(llm.AddProviderOptions{
			Alias:                 args[0],
			BaseURL:               providerAddBaseURL,
			AllowHTTP:             providerAddAllowHTTP,
			APIKey:                apiKey,
			KeyRef:                providerAddKeyRef,
			CatalogPath:           catalogCachePath(),
			CatalogProvider:       providerAddCatalog,
			Models:                providerAddModels,
			ModelContexts:         modelContexts,
			ModelOutputs:          modelOutputs,
			ModelReasoning:        modelReasoning,
			ModelDefaultReasoning: modelDefaultReasoning,
			DefaultReasoning:      providerAddDefaultReasoning,
			ModelModalities:       modelModalities,
			RequireModelMetadata:  true,
			DefaultModel:          providerAddDefault,
			APIShape:              providerAddAPIShape,
			Force:                 providerAddForce,
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
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "✓ 已保存 LLM Provider %s（%d 个模型，默认 %s）\n",
			res.Entry.Alias, len(res.Entry.Models), detail)
		fmt.Fprintf(out, "接入地址：%s\n", res.Entry.BaseURL)
		fmt.Fprintf(out, "接入形态：%s\n", apiShapeLabel(res.Entry.APIShape))
		return nil
	},
}

var aiProviderEditCmd = &cobra.Command{
	Use:   "edit <alias>",
	Short: "Edit an existing LLM provider profile (alias is immutable)",
	Long: `Edit an LLM provider profile in place. The alias is the primary key and
cannot be changed. Only the flags you pass are modified; omitted fields keep
their current value.

Credential rotation follows the same semantics as add: --api-key-stdin (or
--rotate-key for a TTY prompt) overwrites the profile's own credential;
--key-ref switches to an external reference and deletes the own credential;
passing neither keeps the credential and its reference untouched.

When the model set, catalog provider or model context metadata changes, every
final model must resolve a context window; legacy profiles can still edit other
fields without being forced to backfill model metadata.

The base URL and api_shape are validated exactly like add. Any failure leaves
the profile, credential and references untouched.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var modelContexts map[string]int
		if cmd.Flags().Changed("model-context") {
			var err error
			modelContexts, err = llm.ParseModelContexts(providerEditModelCtx)
			if err != nil {
				return err
			}
		}
		var modelOutputs map[string]int
		if cmd.Flags().Changed("model-output") {
			var err error
			modelOutputs, err = llm.ParseModelOutputs(providerEditModelOut)
			if err != nil {
				return err
			}
		}
		var modelReasoning map[string][]string
		if cmd.Flags().Changed("model-reasoning") {
			var err error
			modelReasoning, err = llm.ParseModelReasoning(providerEditModelReason)
			if err != nil {
				return err
			}
		}
		var modelDefaultReasoning map[string]string
		if cmd.Flags().Changed("model-default-reasoning") {
			var err error
			modelDefaultReasoning, err = llm.ParseModelDefaultReasoning(providerEditModelDefaultReason)
			if err != nil {
				return err
			}
		}
		var modelModalities map[string][]string
		if cmd.Flags().Changed("model-modalities") {
			var err error
			modelModalities, err = llm.ParseModelModalities(providerEditModelModalities)
			if err != nil {
				return err
			}
		}
		mgr, err := getAIProviderManager()
		if err != nil {
			return err
		}
		opts := llm.EditProviderOptions{Alias: args[0], CatalogPath: catalogCachePath()}
		if cmd.Flags().Changed("base-url") {
			opts.BaseURL = &providerEditBaseURL
		}
		if cmd.Flags().Changed("allow-http") {
			opts.AllowHTTP = providerEditAllowHTTP
		}
		if cmd.Flags().Changed("api-shape") {
			opts.APIShape = &providerEditAPIShape
		}
		if cmd.Flags().Changed("catalog-provider") {
			opts.CatalogProvider = &providerEditCatalog
		}
		if cmd.Flags().Changed("model") {
			opts.Models = providerEditModels
		}
		if cmd.Flags().Changed("model-context") {
			opts.ModelContexts = modelContexts
		}
		if cmd.Flags().Changed("model-output") {
			opts.ModelOutputs = modelOutputs
		}
		if cmd.Flags().Changed("model-reasoning") {
			opts.ModelReasoning = modelReasoning
		}
		if cmd.Flags().Changed("model-default-reasoning") {
			opts.ModelDefaultReasoning = modelDefaultReasoning
		}
		if cmd.Flags().Changed("default-reasoning") {
			opts.DefaultReasoning = &providerEditDefaultReasoning
		}
		if cmd.Flags().Changed("model-modalities") {
			opts.ModelModalities = modelModalities
		}
		if cmd.Flags().Changed("default-model") {
			opts.DefaultModel = &providerEditDefault
		}
		if cmd.Flags().Changed("key-ref") {
			opts.KeyRef = &providerEditKeyRef
		}
		if providerEditAPIKeyStdin || providerEditRotateKey {
			credential, err := providerCredentialReader(cmd.InOrStdin(), cmd.ErrOrStderr(), providerEditAPIKeyStdin)
			if err != nil {
				return err
			}
			opts.APIKey = string(credential)
		}
		opts.RequireModelMetadata = cmd.Flags().Changed("model") ||
			cmd.Flags().Changed("catalog-provider") || cmd.Flags().Changed("model-context") ||
			cmd.Flags().Changed("model-reasoning") || cmd.Flags().Changed("model-default-reasoning") ||
			cmd.Flags().Changed("default-reasoning")
		res, err := mgr.EditProvider(opts)
		if err != nil {
			auditOp(session.AuditOpLLMProvider, "provider:"+args[0], false, "edit 失败")
			return err
		}
		auditOp(session.AuditOpLLMProvider, "provider:"+res.Entry.Alias, true, "edit")
		out := cmd.OutOrStdout()
		for _, w := range res.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
		}
		fmt.Fprintf(out, "✓ 已更新 LLM Provider %s（%d 个模型）\n", res.Entry.Alias, len(res.Entry.Models))
		fmt.Fprintf(out, "接入地址：%s\n", res.Entry.BaseURL)
		fmt.Fprintf(out, "接入形态：%s\n", apiShapeLabel(res.Entry.APIShape))
		return nil
	},
}

// apiShapeLabel renders the effective api_shape, making the fallback explicit.
func apiShapeLabel(shape string) string {
	if shape == "" {
		return "（未声明，切换时按目标 agent 协议族推断）"
	}
	return shape
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
			fmt.Fprintf(out, "%s\t%s\t模型数 %d\t默认 %s\t形态 %s\t目录 %s\n",
				e.Alias, e.BaseURL, len(e.Models), orDash(e.DefaultModel), orDash(e.APIShape), orDash(e.CatalogProvider))
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
		fmt.Fprintf(out, "接入形态：%s\n", orDash(e.APIShape))
		fmt.Fprintf(out, "模型（%d）：%s\n", len(e.Models), strings.Join(e.Models, ", "))
		if len(e.ModelInfo) > 0 {
			fmt.Fprintln(out, "模型信息：")
			for _, model := range e.Models {
				info, ok := e.ModelInfo[model]
				if !ok {
					continue
				}
				details := modelInfoDetails(info)
				if details == "" {
					continue
				}
				fmt.Fprintf(out, "  %s: %s\n", model, details)
			}
		}
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

// modelInfoDetails 把单条模型信息渲染成紧凑键值串（空字段跳过）。
func modelInfoDetails(info storage.LLMModelInfo) string {
	var parts []string
	if info.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("context=%d", info.ContextWindow))
	}
	if info.OutputLimit > 0 {
		parts = append(parts, fmt.Sprintf("output=%d", info.OutputLimit))
	}
	if len(info.ReasoningEfforts) > 0 {
		parts = append(parts, "reasoning="+strings.Join(info.ReasoningEfforts, ";"))
	}
	if info.DefaultReasoning != "" {
		parts = append(parts, "default_reasoning="+info.DefaultReasoning)
	}
	if len(info.InputModalities) > 0 {
		parts = append(parts, "modalities="+strings.Join(info.InputModalities, ","))
	}
	if info.Name != "" {
		parts = append(parts, "name="+info.Name)
	}
	return strings.Join(parts, " ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// readProviderCredential isolates terminal and stdin input from Cobra flag
// binding. fromStdin comes from the calling command's own --api-key-stdin flag
// so add and edit each honor their own flag rather than a shared variable. The
// secret is used directly by the provider manager and never retained by
// package state.
func readProviderCredential(stdin io.Reader, stderr io.Writer, fromStdin bool) ([]byte, error) {
	if fromStdin {
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
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModelCtx, "model-context", nil, "model context window: <model>=<tokens> (repeatable; required when metadata is unavailable)")
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModelOut, "model-output", nil, "model output limit: <model>=<tokens> (repeatable)")
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModelReason, "model-reasoning", nil, "model reasoning efforts: <model>=<effort>[;<effort>...] (repeatable; marks the model as reasoning-capable)")
	aiProviderAddCmd.Flags().StringSliceVar(&providerAddModelDefaultReason, "model-default-reasoning", nil, "model default reasoning effort: <model>=<effort> (repeatable; required when the model has reasoning efforts)")
	aiProviderAddCmd.Flags().StringVar(&providerAddDefaultReasoning, "default-reasoning", "", "collection default reasoning effort; fills models that have efforts but no resolved default")
	aiProviderAddCmd.Flags().StringArrayVar(&providerAddModelModalities, "model-modalities", nil, "model input modalities: <model>=<mod>[,<mod>...] (repeatable; text,image,audio,video,pdf)")
	aiProviderAddCmd.Flags().StringVar(&providerAddDefault, "default-model", "", "default model (must be in the model set)")
	aiProviderAddCmd.Flags().StringVar(&providerAddAPIShape, "api-shape", "", "API shape: "+llm.APIShapeList()+" (empty derives it from the target agent)")
	aiProviderAddCmd.Flags().BoolVar(&providerAddForce, "force", false, "overwrite an existing profile")
	aiProviderEditCmd.Flags().StringVar(&providerEditBaseURL, "base-url", "", "new provider base URL (https)")
	aiProviderEditCmd.Flags().BoolVar(&providerEditAPIKeyStdin, "api-key-stdin", false, "rotate the own credential with a key read from stdin (no echo, no argv)")
	aiProviderEditCmd.Flags().BoolVar(&providerEditRotateKey, "rotate-key", false, "rotate the own credential through a TTY prompt")
	aiProviderEditCmd.Flags().BoolVar(&providerEditAllowHTTP, "allow-http", false, "explicitly allow an HTTP base URL (default requires HTTPS)")
	aiProviderEditCmd.Flags().StringVar(&providerEditKeyRef, "key-ref", "", "switch to an external reference: env:<group>/<key> or text:<group>/<key>")
	aiProviderEditCmd.Flags().StringVar(&providerEditCatalog, "catalog-provider", "", "models.dev provider id; re-assembles the model set")
	aiProviderEditCmd.Flags().StringSliceVar(&providerEditModels, "model", nil, "custom model id (repeatable); replaces the model set")
	aiProviderEditCmd.Flags().StringSliceVar(&providerEditModelCtx, "model-context", nil, "model context window: <model>=<tokens> (repeatable; required when metadata is unavailable)")
	aiProviderEditCmd.Flags().StringSliceVar(&providerEditModelOut, "model-output", nil, "model output limit: <model>=<tokens> (repeatable)")
	aiProviderEditCmd.Flags().StringSliceVar(&providerEditModelReason, "model-reasoning", nil, "model reasoning efforts: <model>=<effort>[;<effort>...] (repeatable; marks the model as reasoning-capable)")
	aiProviderEditCmd.Flags().StringSliceVar(&providerEditModelDefaultReason, "model-default-reasoning", nil, "model default reasoning effort: <model>=<effort> (repeatable; required when changing reasoning efforts)")
	aiProviderEditCmd.Flags().StringVar(&providerEditDefaultReasoning, "default-reasoning", "", "collection default reasoning effort; fills models that have efforts but no resolved default")
	aiProviderEditCmd.Flags().StringArrayVar(&providerEditModelModalities, "model-modalities", nil, "model input modalities: <model>=<mod>[,<mod>...] (repeatable; text,image,audio,video,pdf)")
	aiProviderEditCmd.Flags().StringVar(&providerEditDefault, "default-model", "", "default model (must be in the final model set); empty clears it")
	aiProviderEditCmd.Flags().StringVar(&providerEditAPIShape, "api-shape", "", "API shape: "+llm.APIShapeList()+" (empty clears the field)")
	aiProviderCmd.AddCommand(aiProviderAddCmd, aiProviderEditCmd, aiProviderListCmd, aiProviderShowCmd, aiProviderRemoveCmd)
}
