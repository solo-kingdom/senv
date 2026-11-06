package llm

import (
	"strings"
)

// reasoningRank 是思考强度从低到高的固定序。none/off 是关闭，不参与次高推断。
// 认不出的档名没有名次。
var reasoningRank = map[string]int{
	"off":     0,
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
}

// piThinkingLevels 是 pi thinkingLevelMap 的全部键，顺序固定。
var piThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// resolvedDefaultReasoning 返回投影用的默认推理档。档案里的显式默认档优先
// （含 none），否则在已知强度里去掉 none/off 后取次高；只剩一档就用那一档。
// 没有档位、全是关闭档、或全是认不出的档名时返回空。不改档案。
func resolvedDefaultReasoning(meta ModelMetadata) string {
	if effort := strings.TrimSpace(meta.DefaultReasoning); effort != "" {
		return effort
	}
	var ranked []string
	for _, raw := range meta.ReasoningEfforts {
		effort := strings.TrimSpace(raw)
		rank, ok := reasoningRank[strings.ToLower(effort)]
		if !ok || rank == 0 {
			continue
		}
		ranked = append(ranked, effort)
	}
	if len(ranked) == 0 {
		return ""
	}
	// 插入排序，同名次保持原顺序。
	for i := 1; i < len(ranked); i++ {
		item := ranked[i]
		rank := reasoningRank[strings.ToLower(item)]
		j := i
		for j > 0 && reasoningRank[strings.ToLower(ranked[j-1])] > rank {
			ranked[j] = ranked[j-1]
			j--
		}
		ranked[j] = item
	}
	if len(ranked) == 1 {
		return ranked[0]
	}
	return ranked[len(ranked)-2]
}

// piThinkingLevel 把解析出的默认档收成 pi 的思考档名。none 写成 off。
// 不在 pi 七档里的名字返回 false。
func piThinkingLevel(effort string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "off":
		return "off", true
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return strings.ToLower(strings.TrimSpace(effort)), true
	default:
		return "", false
	}
}

// piThinkingLevelMap 把档案档位投影成 pi 的 thinkingLevelMap。有对应档的键
// 写原文字符串，none 映到 off，其余键为 null。没有任何档能映上时返回 nil，
// 避免一份全 null 的表把 pi 的缺省档位全部藏掉。
func piThinkingLevelMap(meta ModelMetadata) map[string]any {
	present := map[string]string{}
	for _, raw := range meta.ReasoningEfforts {
		effort := strings.TrimSpace(raw)
		if effort == "" {
			continue
		}
		present[strings.ToLower(effort)] = effort
	}
	if len(present) == 0 {
		return nil
	}
	out := make(map[string]any, len(piThinkingLevels))
	mapped := false
	for _, level := range piThinkingLevels {
		if level == "off" {
			if value, ok := present["none"]; ok {
				out["off"] = value
				mapped = true
				continue
			}
			if value, ok := present["off"]; ok {
				out["off"] = value
				mapped = true
				continue
			}
			out["off"] = nil
			continue
		}
		if value, ok := present[level]; ok {
			out[level] = value
			mapped = true
			continue
		}
		out[level] = nil
	}
	if !mapped {
		return nil
	}
	return out
}
