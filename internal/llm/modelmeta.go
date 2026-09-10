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
)

// ModelMetadata 是单个模型在目录里的元数据子集。
type ModelMetadata struct {
	Name             string
	Description      string
	ContextLimit     int
	OutputLimit      int
	ReasoningEfforts []string
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
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
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
			Name:         entry.Name,
			Description:  entry.Description,
			ContextLimit: entry.Limit.Context,
			OutputLimit:  entry.Limit.Output,
		}
		for _, option := range entry.ReasoningOptions {
			if option.Type != "effort" || len(option.Values) == 0 {
				continue
			}
			meta.ReasoningEfforts = append(meta.ReasoningEfforts, option.Values...)
		}
		out[id] = meta
	}
	return out
}
