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
	codexCatalogDirName     = "model-catalogs"
	codexCatalogShellType   = "shell_command"
	codexCatalogVisibility  = "list"
	codexTruncationMode     = "bytes"
	codexTruncationLimit    = 10000
	codexReasoningLevelDesc = "Reasoning effort"
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

// codexModelEntry 是 senv 生成的 catalog 条目。supported_reasoning_levels 与
// experimental_supported_tools 等字段必须存在，省略会直接解析失败；空数组
// 能解析。空档位只用于推理能力未知/不支持的模型，默认档同时省略。
type codexModelEntry struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// 推理能力未知/不支持的模型省略这两个键，而不是写单档 none：none 是
	// 有效的「关闭思考」指令，M3.1 这类强制思考模型会直接 400（2013）。
	DefaultReasoningLevel      string                `json:"default_reasoning_level,omitempty"`
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
	InputModalities            []string              `json:"input_modalities"`
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
		levels, defaultLevel := codexReasoningLevels(meta)
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
			InputModalities:            codexInputModalities(meta.InputModalities),
		}
		if entry.SupportedReasoningLevels == nil {
			entry.SupportedReasoningLevels = []codexReasoningLevel{}
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

// codexReasoningLevels 把档案声明的推理档位投影成 catalog 条目。无档位或旧
// 档案缺默认档时返回空档位且不设默认档，让 codex 不发 effort；绝不从列表
// 首项推断默认档，也绝不把「未知」投影成 none。
func codexReasoningLevels(meta ModelMetadata) ([]codexReasoningLevel, string) {
	levels := make([]codexReasoningLevel, 0, len(meta.ReasoningEfforts))
	for _, effort := range meta.ReasoningEfforts {
		if strings.TrimSpace(effort) == "" {
			continue
		}
		levels = append(levels, codexReasoningLevel{Effort: effort, Description: codexReasoningLevelDesc})
	}
	if len(levels) == 0 || strings.TrimSpace(meta.DefaultReasoning) == "" {
		return nil, ""
	}
	return levels, meta.DefaultReasoning
}

// codexInputModalities 收敛到 codex catalog 枚举允许的 text/image/audio
// （codex 解析器拒绝其他取值，会让整个 catalog 加载失败）；无声明或被全部
// 过滤时回退 ["text"]——codex 要求该字段非空。
func codexInputModalities(mods []string) []string {
	filtered := filterModalities(mods, "text", "image", "audio")
	if len(filtered) == 0 {
		return []string{"text"}
	}
	return filtered
}

func validCodexModalities(mods []string) bool {
	for _, mod := range mods {
		switch mod {
		case "text", "image", "audio":
		default:
			return false
		}
	}
	return true
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
		case !validCodexModalities(entry.InputModalities):
			return fmt.Errorf("%s (%s): input_modalities has values outside text/image/audio", where, entry.Slug)
		}
		if len(entry.SupportedReasoningLevels) == 0 {
			if strings.TrimSpace(entry.DefaultReasoningLevel) != "" {
				return fmt.Errorf("%s (%s): default_reasoning_level %q is set without supported_reasoning_levels", where, entry.Slug, entry.DefaultReasoningLevel)
			}
			break
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
		if strings.TrimSpace(entry.DefaultReasoningLevel) == "" {
			return fmt.Errorf("%s (%s): default_reasoning_level is empty", where, entry.Slug)
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
