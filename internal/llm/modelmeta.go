// 模型目录元数据：从 models.dev 本地缓存里取出单个模型的公开元数据，供各
// agent 投影填充原生字段（claude-code 的 label/description、codex 的
// reasoning levels 与 context、kimi 的 max_context_size）。
//
// 目录是可选增强而非依赖：缓存缺失、provider 未登记、单个模型缺字段时一律
// 返回「未知」（零值），由适配器回退模板，绝不因此让切换失败。
package llm

import (
	"encoding/json"
	"path/filepath"

	"github.com/wii/senv/internal/storage"
)

// ModelMetadata 是单个模型在目录里的元数据子集。
type ModelMetadata struct {
	Name             string
	Description      string
	ContextLimit     int
	OutputLimit      int
	ReasoningEfforts []string
	DefaultReasoning string
	InputModalities  []string
}

// DefaultModelCatalogPath 返回与指针文件同级的目录缓存路径（senv 配置目录
// 下的 cache/models-dev.json），保证 MCP/TUI 与 CLI 读到同一份缓存。
func DefaultModelCatalogPath(pointerPath string) string {
	if pointerPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(pointerPath), "cache", "models-dev.json")
}

// catalogModelEntry 是目录中单个模型的原始结构；只声明投影用得到的字段，
// 未声明的字段忽略（目录透传保存，格式变化不影响读取）。
type catalogModelEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Limit       struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	ReasoningOptions []struct {
		Type    string   `json:"type"`
		Values  []string `json:"values"`
		Default string   `json:"default"`
	} `json:"reasoning_options"`
	Modalities struct {
		Input []string `json:"input"`
	} `json:"modalities"`
}

// LoadModelMetadata 读取目录缓存，返回 catalogProvider 下给定模型的元数据。
// 返回的 map 只包含能解析出内容的模型；未知模型不出现在 map 里，调用方按
// 零值处理。任何读取/解析失败都退化为空结果，不返回错误。
func LoadModelMetadata(catalogPath, catalogProvider string, modelIDs []string) map[string]ModelMetadata {
	out := map[string]ModelMetadata{}
	if catalogPath == "" || catalogProvider == "" || len(modelIDs) == 0 {
		return out
	}
	cat, err := Load(catalogPath)
	if err != nil || cat == nil {
		return out
	}
	var providers map[string]struct {
		Models map[string]catalogModelEntry `json:"models"`
	}
	if err := json.Unmarshal(cat.Providers, &providers); err != nil {
		return out
	}
	provider, ok := providers[catalogProvider]
	if !ok {
		return out
	}
	for _, id := range modelIDs {
		entry, ok := provider.Models[id]
		if !ok {
			continue
		}
		meta := ModelMetadata{
			Name:            entry.Name,
			Description:     entry.Description,
			ContextLimit:    entry.Limit.Context,
			OutputLimit:     entry.Limit.Output,
			InputModalities: append([]string(nil), entry.Modalities.Input...),
		}
		for _, option := range entry.ReasoningOptions {
			if option.Type != "effort" || len(option.Values) == 0 {
				continue
			}
			meta.ReasoningEfforts = append(meta.ReasoningEfforts, option.Values...)
			if meta.DefaultReasoning == "" && option.Default != "" {
				meta.DefaultReasoning = option.Default
			}
		}
		out[id] = meta
	}
	return out
}

// ResolveModelMetadata overlays metadata persisted in the provider profile on
// top of the optional models.dev cache. Persisted values win so an explicit
// context window survives cache loss or upstream changes.
func ResolveModelMetadata(stored map[string]storage.LLMModelInfo, catalogPath, catalogProvider string, modelIDs []string) map[string]ModelMetadata {
	external := LoadModelMetadata(catalogPath, catalogProvider, modelIDs)
	out := make(map[string]ModelMetadata, len(modelIDs))
	for _, id := range modelIDs {
		meta := external[id]
		if info, ok := stored[id]; ok {
			meta = mergeModelMetadata(meta, modelMetadataFromStorage(info))
		}
		if !modelMetadataEmpty(meta) {
			out[id] = meta
		}
	}
	return out
}

func modelMetadataFromStorage(info storage.LLMModelInfo) ModelMetadata {
	return ModelMetadata{
		Name:             info.Name,
		Description:      info.Description,
		ContextLimit:     info.ContextWindow,
		OutputLimit:      info.OutputLimit,
		ReasoningEfforts: append([]string(nil), info.ReasoningEfforts...),
		DefaultReasoning: info.DefaultReasoning,
		InputModalities:  append([]string(nil), info.InputModalities...),
	}
}

func storageModelInfo(meta ModelMetadata) storage.LLMModelInfo {
	return storage.LLMModelInfo{
		Name:             meta.Name,
		Description:      meta.Description,
		ContextWindow:    meta.ContextLimit,
		OutputLimit:      meta.OutputLimit,
		ReasoningEfforts: append([]string(nil), meta.ReasoningEfforts...),
		DefaultReasoning: meta.DefaultReasoning,
		InputModalities:  append([]string(nil), meta.InputModalities...),
	}
}

// clearMetadataDimension 对所有模型执行同一维度的重置，用于「空非 nil map =
// 显式清空该维度」的编辑语义。
func clearMetadataDimension(metadata map[string]ModelMetadata, reset func(*ModelMetadata)) {
	for id, meta := range metadata {
		reset(&meta)
		metadata[id] = meta
	}
}

func mergeModelMetadata(base, override ModelMetadata) ModelMetadata {
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.ContextLimit > 0 {
		base.ContextLimit = override.ContextLimit
	}
	if override.OutputLimit > 0 {
		base.OutputLimit = override.OutputLimit
	}
	if len(override.ReasoningEfforts) > 0 {
		base.ReasoningEfforts = append([]string(nil), override.ReasoningEfforts...)
	}
	if override.DefaultReasoning != "" {
		base.DefaultReasoning = override.DefaultReasoning
	}
	if len(override.InputModalities) > 0 {
		base.InputModalities = append([]string(nil), override.InputModalities...)
	}
	return base
}

func modelMetadataEmpty(meta ModelMetadata) bool {
	return meta.Name == "" && meta.Description == "" && meta.ContextLimit <= 0 &&
		meta.OutputLimit <= 0 && len(meta.ReasoningEfforts) == 0 &&
		meta.DefaultReasoning == "" && len(meta.InputModalities) == 0
}
