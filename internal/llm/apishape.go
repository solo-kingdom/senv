package llm

import (
	"fmt"
	"strings"

	"github.com/wii/senv/internal/storage"
)

// APIShape 声明一份 provider 档案服务的接口形态。空值表示「未声明」：切换时
// 按目标 coding agent 的协议族归一接入地址（ADR-0004 / ADR-0006 的旧行为）。
type APIShape string

const (
	APIShapeOpenAIChat      APIShape = storage.LLMAPIShapeOpenAIChat
	APIShapeOpenAIResponses APIShape = storage.LLMAPIShapeOpenAIResponses
	APIShapeAnthropic       APIShape = storage.LLMAPIShapeAnthropic
)

// APIShapes 列出全部合法取值（展示与帮助文本用）。
var APIShapes = []APIShape{APIShapeOpenAIChat, APIShapeOpenAIResponses, APIShapeAnthropic}

// ParseAPIShape 校验并转换原始 api_shape 取值；空值是合法的「未声明」。
func ParseAPIShape(raw string) (APIShape, error) {
	shape := APIShape(strings.TrimSpace(raw))
	if shape == "" {
		return "", nil
	}
	for _, candidate := range APIShapes {
		if shape == candidate {
			return shape, nil
		}
	}
	return "", fmt.Errorf("invalid api_shape %q: want one of %s", raw, APIShapeList())
}

// APIShapeList 渲染合法取值列表，供错误信息与 --help 复用。
func APIShapeList() string {
	parts := make([]string, 0, len(APIShapes))
	for _, shape := range APIShapes {
		parts = append(parts, string(shape))
	}
	return strings.Join(parts, ", ")
}

// Protocol 返回该形态对应的接入地址归一协议族。
func (s APIShape) Protocol() (ProtocolFamily, bool) {
	switch s {
	case APIShapeOpenAIChat, APIShapeOpenAIResponses:
		return ProtocolOpenAICompatible, true
	case APIShapeAnthropic:
		return ProtocolAnthropic, true
	}
	return 0, false
}

// DescribeProtocol 为协议族给出可读名称（错误信息用）。
func DescribeProtocol(family ProtocolFamily) string {
	if family == ProtocolAnthropic {
		return "anthropic"
	}
	return "openai-compatible"
}

// baseURLSourceInferred 是 switch 输出中的地址来源标注：无显式形态地址、
// 由 BaseURL 按协议族推断。
const baseURLSourceInferred = "由 BaseURL 推断"

// familyHasExplicitShapeURL 报告目标协议族在档案上是否存在显式形态地址
// （Anthropic 族看 anthropic_base_url；OpenAI 兼容族看 chat_base_url 或
// responses_base_url 任一）。这是 switch 门禁的放行条件之一。
func familyHasExplicitShapeURL(entry *storage.LLMProviderEntry, family ProtocolFamily) bool {
	if family == ProtocolAnthropic {
		return entry.AnthropicBaseURL != ""
	}
	return entry.ChatBaseURL != "" || entry.ResponsesBaseURL != ""
}

// agentPrefersChatWire 返回 OpenAI 兼容族 agent 在当前声明形态下是否走 Chat
// Completions 线协议。与各适配器的线协议字段选择保持一致：codex 只讲
// Responses（上游已移除 chat 线协议，声明 openai-chat 且无 responses_base_url
// 的档案在 switch 门禁处拒绝）；kimi/pi/opencode 默认 chat（仅显式
// openai-responses 时走 responses）。anthropic 声明不约束本族选择。
func agentPrefersChatWire(agentID string, declaredShape APIShape) bool {
	if agentID == "codex" {
		return false
	}
	switch declaredShape {
	case APIShapeOpenAIChat:
		return true
	case APIShapeOpenAIResponses:
		return false
	}
	return true
}
