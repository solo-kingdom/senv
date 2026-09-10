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
