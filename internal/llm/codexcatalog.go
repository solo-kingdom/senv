// codex 的模型目录文件（model_catalog_json）：codex 用自己的 catalog 作为
// 会话内模型选择器的数据源，因此 Agent 模型集必须落成一份 catalog，而不是
// 只写 config.toml 的单个 model。file schema 未公开，必填字段由
// `codex debug models` 实测确定；写入前做一次自校验，缺字段即失败并回滚。
package llm

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	codexCatalogDirName        = "model-catalogs"
	codexCatalogShellType      = "shell_command"
	codexCatalogVisibility     = "list"
	codexTruncationMode        = "bytes"
	codexTruncationLimit       = 10000
	codexReasoningLevelDesc    = "Reasoning effort"
	codexFallbackReasoning     = "none"
	codexFallbackReasoningDesc = "No extra reasoning"
)

// codexReasoningLevel 对应 catalog 里的 supported_reasoning_levels 条目。
type codexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// codexTruncationPolicy 对应 catalog 里的 truncation_policy。
type codexTruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

// codexModelEntry 是 senv 生成的 catalog 条目。字段一律无 omitempty：
// codex 对 supported_reasoning_levels 与 experimental_supported_tools 等字段
// 是「必须存在」，省略会直接解析失败。supported_reasoning_levels 还必须
// 非空：空数组能解析，但 TUI /model 选择器把 0 档当成「多档、不关父列表」，
// 再把空档合成 1 档自动应用，回车无法退出。
type codexModelEntry struct {
	Slug                       string                `json:"slug"`
	DisplayName                string                `json:"display_name"`
	Description                string                `json:"description"`
	DefaultReasoningLevel      string                `json:"default_reasoning_level"`
	SupportedReasoningLevels   []codexReasoningLevel `json:"supported_reasoning_levels"`
	ShellType                  string                `json:"shell_type"`
	Visibility                 string                `json:"visibility"`
	SupportedInAPI             bool                  `json:"supported_in_api"`
	Priority                   int                   `json:"priority"`
	BaseInstructions           string                `json:"base_instructions"`
	SupportVerbosity           bool                  `json:"support_verbosity"`
	TruncationPolicy           codexTruncationPolicy `json:"truncation_policy"`
	SupportsParallelToolCalls  bool                  `json:"supports_parallel_tool_calls"`
	ExperimentalSupportedTools []string              `json:"experimental_supported_tools"`
	ContextWindow              int                   `json:"context_window,omitempty"`
	MaxContextWindow           int                   `json:"max_context_window,omitempty"`
}

// codexCatalogPath 返回某 provider alias 对应的 catalog 文件路径（senv 自有
// 文件，位于 agent 的 home 下，不在 agent 配置文件清单里）。
func codexCatalogPath(home, alias string) string {
	return filepath.Join(home, ".codex", codexCatalogDirName, senvProviderID(alias)+".json")
}

// codexCatalogRelPath 返回写进 config.toml 的 catalog 路径（相对 home 的
// `~` 形态，与用户既有配置一致）。
func codexCatalogRelPath(alias string) string {
	return "~/.codex/" + codexCatalogDirName + "/" + senvProviderID(alias) + ".json"
}

// buildCodexCatalog 把 Agent 模型集投影成 codex catalog：能取目录真实值的
// 优先取真实值（display_name/description/context/reasoning levels），缺失的
// 用固定模板补齐。返回的字节已通过自校验。
func buildCodexCatalog(req SwitchRequest) ([]byte, error) {
	entries := make([]codexModelEntry, 0, len(req.Models))
	for i, model := range req.Models {
		meta := req.ModelMetadata[model]
		displayName := meta.Name
		if displayName == "" {
			displayName = model
		}
		levels, defaultLevel := codexReasoningLevels(meta.ReasoningEfforts)
		entry := codexModelEntry{
			Slug:                       model,
			DisplayName:                displayName,
			Description:                meta.Description,
			DefaultReasoningLevel:      defaultLevel,
			SupportedReasoningLevels:   levels,
			ShellType:                  codexCatalogShellType,
			Visibility:                 codexCatalogVisibility,
			SupportedInAPI:             true,
			Priority:                   i,
			BaseInstructions:           "You are Codex, a coding agent based on " + displayName + ". You and the user share the same workspace and collaborate to achieve the user's goals.",
			SupportVerbosity:           false,
			TruncationPolicy:           codexTruncationPolicy{Mode: codexTruncationMode, Limit: codexTruncationLimit},
			SupportsParallelToolCalls:  true,
			ExperimentalSupportedTools: []string{},
		}
		if meta.ContextLimit > 0 {
			entry.ContextWindow = meta.ContextLimit
			entry.MaxContextWindow = meta.ContextLimit
		}
		entries = append(entries, entry)
	}
	payload := struct {
		Models []codexModelEntry `json:"models"`
	}{Models: entries}
	if err := validateCodexCatalog(entries); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode codex model catalog: %w", err)
	}
	return append(data, '\n'), nil
}

// codexReasoningLevels 把档案/目录里的推理档位投影成 catalog 条目。没有可用
// 档位时写入单档 none：空数组能被 codex 解析，但 TUI 选择器无法关闭。
func codexReasoningLevels(efforts []string) ([]codexReasoningLevel, string) {
	levels := make([]codexReasoningLevel, 0, len(efforts))
	for _, effort := range efforts {
		if strings.TrimSpace(effort) == "" {
			continue
		}
		levels = append(levels, codexReasoningLevel{Effort: effort, Description: codexReasoningLevelDesc})
	}
	if len(levels) == 0 {
		levels = []codexReasoningLevel{{
			Effort:      codexFallbackReasoning,
			Description: codexFallbackReasoningDesc,
		}}
	}
	return levels, levels[0].Effort
}

// validateCodexCatalog 校验 codex 解析 catalog 时必须存在的字段；任一缺失或
// 非法即返回带字段名的错误，由调用方回滚并原样上报。
func validateCodexCatalog(entries []codexModelEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("codex model catalog has no models")
	}
	seen := make(map[string]struct{}, len(entries))
	for i, entry := range entries {
		where := fmt.Sprintf("codex model catalog entry %d", i)
		switch {
		case strings.TrimSpace(entry.Slug) == "":
			return fmt.Errorf("%s: slug is empty", where)
		case strings.TrimSpace(entry.DisplayName) == "":
			return fmt.Errorf("%s (%s): display_name is empty", where, entry.Slug)
		case len(entry.SupportedReasoningLevels) == 0:
			return fmt.Errorf("%s (%s): supported_reasoning_levels is empty", where, entry.Slug)
		case strings.TrimSpace(entry.DefaultReasoningLevel) == "":
			return fmt.Errorf("%s (%s): default_reasoning_level is empty", where, entry.Slug)
		case strings.TrimSpace(entry.ShellType) == "":
			return fmt.Errorf("%s (%s): shell_type is empty", where, entry.Slug)
		case strings.TrimSpace(entry.Visibility) == "":
			return fmt.Errorf("%s (%s): visibility is empty", where, entry.Slug)
		case strings.TrimSpace(entry.BaseInstructions) == "":
			return fmt.Errorf("%s (%s): base_instructions and model_messages.instructions_template are both missing", where, entry.Slug)
		case entry.TruncationPolicy.Mode == "" || entry.TruncationPolicy.Limit <= 0:
			return fmt.Errorf("%s (%s): truncation_policy is missing or invalid", where, entry.Slug)
		case entry.ExperimentalSupportedTools == nil:
			return fmt.Errorf("%s (%s): experimental_supported_tools is missing", where, entry.Slug)
		}
		defaultOK := false
		for _, level := range entry.SupportedReasoningLevels {
			if strings.TrimSpace(level.Effort) == "" {
				return fmt.Errorf("%s (%s): supported_reasoning_levels has an empty effort", where, entry.Slug)
			}
			if level.Effort == entry.DefaultReasoningLevel {
				defaultOK = true
			}
		}
		if !defaultOK {
			return fmt.Errorf("%s (%s): default_reasoning_level %q is not in supported_reasoning_levels", where, entry.Slug, entry.DefaultReasoningLevel)
		}
		if _, dup := seen[entry.Slug]; dup {
			return fmt.Errorf("%s: duplicate model slug %q", where, entry.Slug)
		}
		seen[entry.Slug] = struct{}{}
	}
	return nil
}
