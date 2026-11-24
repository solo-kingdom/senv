package cmd

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wii/senv/internal/tree"
)

// 路径树 MCP 面：只读 ls/get + 受控单文件 set。刻意不提供 rm / pull / push
// （目录级落盘与批量删除留给 CLI 与用户确认）。

type treeLsInput struct {
	Group  string `json:"group" jsonschema:"路径树分组（如 deploy / wireguard）"`
	Prefix string `json:"prefix,omitempty" jsonschema:"路径前缀过滤（如 tcbj/caddy）"`
}

type treeGetInput struct {
	Group string `json:"group" jsonschema:"路径树分组"`
	Path  string `json:"path" jsonschema:"相对路径（如 tcbj/caddy/Caddyfile）"`
}

type treeSetInput struct {
	Group       string `json:"group" jsonschema:"路径树分组"`
	Path        string `json:"path" jsonschema:"相对路径"`
	Content     string `json:"content" jsonschema:"文件完整内容（UTF-8 文本）"`
	Exec        bool   `json:"exec,omitempty" jsonschema:"是否标记可执行位"`
	Description string `json:"description,omitempty" jsonschema:"文件说明（省略则保留原说明）"`
}

func (m *managers) treeLs(_ context.Context, _ *mcp.CallToolRequest, in treeLsInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	metas, err := m.tree.List(in.Group, in.Prefix)
	if err != nil {
		return errResult(err)
	}
	return textResult(metas)
}

func (m *managers) treeGet(_ context.Context, _ *mcp.CallToolRequest, in treeGetInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	e, err := m.tree.Get(in.Group, in.Path)
	if err != nil {
		return errResult(err)
	}
	raw, err := e.DecodeBytes()
	if err != nil {
		return errResult(err)
	}
	if e.Encoding == "base64" {
		return errResult(fmt.Errorf("%s:%s 是二进制文件，MCP 不返回内容；请用 CLI senv tree get -o", in.Group, in.Path))
	}
	return textResult(map[string]any{
		"path": in.Path, "mode": fmt.Sprintf("%04o", e.Mode), "sha256": e.SHA256,
		"description": e.Description, "content": string(raw),
	})
}

func (m *managers) treeSet(_ context.Context, _ *mcp.CallToolRequest, in treeSetInput) (*mcp.CallToolResult, emptyOut, error) {
	if tree.IsSensitiveMCPPath(in.Path) {
		return errResult(fmt.Errorf("拒绝经 MCP 写入敏感路径 %q（.env / *.key / *.pem 等请走 senv env/text 或 CLI）", in.Path))
	}
	mode := uint32(0o644)
	if in.Exec {
		mode = 0o755
	}
	e, err := m.tree.Set(in.Group, in.Path, []byte(in.Content), mode, in.Description, in.Description == "")
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]any{"path": in.Path, "mode": fmt.Sprintf("%04o", e.Mode), "sha256": e.SHA256})
}
