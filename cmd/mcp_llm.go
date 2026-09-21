package cmd

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

// llmProviderShapeURLs 是 llm_provider_list 响应中的形态地址视图（键各自
// omitempty；全空时整个对象省略）。地址与 base_url 同级按非密钥处理。
type llmProviderShapeURLs struct {
	Chat      string `json:"chat,omitempty"`
	Responses string `json:"responses,omitempty"`
	Anthropic string `json:"anthropic,omitempty"`
}

// llmProviderView 是 MCP 响应的显式白名单：档案本身不存凭据明文，这里
// 再裁剪一层，从结构上排除任何密钥字段。
type llmProviderView struct {
	Alias         string                `json:"alias"`
	BaseURL       string                `json:"base_url"`
	ShapeURLs     *llmProviderShapeURLs `json:"shape_urls,omitempty"`
	APIShape      string                `json:"api_shape,omitempty"`
	CredentialRef string                `json:"credential_ref"`
	Catalog       string                `json:"catalog_provider,omitempty"`
	DefaultModel  string                `json:"default_model,omitempty"`
	// BackgroundModel 是档案声明的后台模型（ADR-0029），空表示未声明
	// （切换时回退默认模型）。
	BackgroundModel string                          `json:"background_model,omitempty"`
	Description     string                          `json:"description,omitempty"`
	Models          []string                        `json:"models"`
	ModelInfo       map[string]storage.LLMModelInfo `json:"model_info,omitempty"`
	CreatedAt       string                          `json:"created_at"`
	UpdatedAt       string                          `json:"updated_at"`
}

func llmProviderViewFrom(e *storage.LLMProviderEntry) llmProviderView {
	view := llmProviderView{
		Alias:           e.Alias,
		BaseURL:         e.BaseURL,
		APIShape:        e.APIShape,
		CredentialRef:   e.CredentialRef,
		Catalog:         e.CatalogProvider,
		DefaultModel:    e.DefaultModel,
		BackgroundModel: e.BackgroundModel,
		Description:     e.Description,
		Models:          e.Models,
		ModelInfo:       e.ModelInfo,
		CreatedAt:       e.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ChatBaseURL != "" || e.ResponsesBaseURL != "" || e.AnthropicBaseURL != "" {
		view.ShapeURLs = &llmProviderShapeURLs{
			Chat:      e.ChatBaseURL,
			Responses: e.ResponsesBaseURL,
			Anthropic: e.AnthropicBaseURL,
		}
	}
	return view
}

// llmAgentStatusView 描述单个 agent 的当前指向；pointer 未切换时为 null。
type llmAgentStatusView struct {
	Agent      string            `json:"agent"`
	Name       string            `json:"name"`
	Pointer    *llm.AgentPointer `json:"pointer"`
	ConfigPath string            `json:"config_path"`
}

func (m *managers) llmProviderList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	providers, err := m.llm.ListProviders()
	if err != nil {
		return errResult(err)
	}
	out := make([]llmProviderView, 0, len(providers))
	for _, p := range providers {
		out = append(out, llmProviderViewFrom(p))
	}
	return textResult(out)
}

func (m *managers) llmAgentStatus(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	sm := llm.NewSwitchManager(m.llm, m.llmPointer, m.llmHome)
	rows, warning, err := sm.Status()
	if err != nil {
		return errResult(err)
	}
	out := make([]llmAgentStatusView, 0, len(rows))
	for _, r := range rows {
		view := llmAgentStatusView{
			Agent:      r.AgentID,
			Name:       r.AgentName,
			Pointer:    r.Pointer,
			ConfigPath: r.ConfigPath,
		}
		out = append(out, view)
	}
	payload := struct {
		Agents  []llmAgentStatusView `json:"agents"`
		Warning string               `json:"warning,omitempty"`
	}{Agents: out, Warning: warning}
	return textResult(payload)
}
