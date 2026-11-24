package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wii/senv/internal/ledger"
)

// 台账 MCP 面：只读 + 追加 + 单字段/状态更新。刻意不提供删除与实体删除
// （失控 agent 的最大破坏半径 = 多写几条可更正的记录）。

type ledgerListInput struct {
	Group string `json:"group,omitempty" jsonschema:"台账分组（如 personal / feg / mv）"`
	Type  string `json:"type,omitempty" jsonschema:"实体类型过滤（device / infra / service / agent / container）"`
}

type ledgerHeadView struct {
	Type        string               `json:"type"`
	ID          string               `json:"id"`
	Group       string               `json:"group"`
	Description string               `json:"description,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Fields      []ledger.Field       `json:"fields,omitempty"`
	Status      []ledger.StatusEntry `json:"status,omitempty"`
	UpdatedAt   string               `json:"updated_at"`
}

type ledgerGetInput struct {
	Entity string `json:"entity" jsonschema:"实体键 <type>.<id>"`
	Group  string `json:"group,omitempty" jsonschema:"台账分组，默认 default"`
	Limit  int    `json:"limit,omitempty" jsonschema:"附带记录条数（默认 10）"`
}

type ledgerGetOut struct {
	Head    ledgerHeadView  `json:"head"`
	Records []ledger.Record `json:"records"`
}

type ledgerAppendInput struct {
	Entity     string   `json:"entity" jsonschema:"实体键 <type>.<id>"`
	Group      string   `json:"group,omitempty" jsonschema:"台账分组，默认 default"`
	Summary    string   `json:"summary" jsonschema:"一行标题（必填）"`
	Body       string   `json:"body,omitempty" jsonschema:"正文 Markdown（做了什么/验证/回滚/关联）"`
	Kind       string   `json:"kind,omitempty" jsonschema:"记录类型 op / note / correction（默认 op）"`
	Agent      string   `json:"agent,omitempty" jsonschema:"操作 agent 名称（如 Claude Code）"`
	At         string   `json:"at,omitempty" jsonschema:"事件日期 YYYY-MM-DD（默认现在）"`
	Supersedes []string `json:"supersedes,omitempty" jsonschema:"更正指向的记录键"`
}

type ledgerUpdateInput struct {
	Entity string `json:"entity" jsonschema:"实体键 <type>.<id>"`
	Group  string `json:"group,omitempty" jsonschema:"台账分组，默认 default"`
	Field  string `json:"field,omitempty" jsonschema:"要修改的字段名（与 value 配对；value 为空删除该字段）"`
	Value  string `json:"value,omitempty"`
	Status string `json:"status,omitempty" jsonschema:"写入最近状态（保留最近 2 条）"`
	Level  string `json:"level,omitempty" jsonschema:"状态级别（ok / warn 等）"`
	Agent  string `json:"agent,omitempty" jsonschema:"操作 agent 名称"`
}

func (m *managers) ledgerHeadView(h *ledger.Head) ledgerHeadView {
	return ledgerHeadView{
		Type: h.Type, ID: h.ID, Group: h.Group, Description: h.Description, Tags: h.Tags,
		Fields: h.Fields, Status: h.Status, UpdatedAt: h.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func (m *managers) ledgerList(_ context.Context, _ *mcp.CallToolRequest, in ledgerListInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	heads, err := m.ledger.ListHeads(in.Group)
	if err != nil {
		return errResult(err)
	}
	out := make([]ledgerHeadView, 0, len(heads))
	for i := range heads {
		if in.Type != "" && heads[i].Type != in.Type {
			continue
		}
		view := m.ledgerHeadView(&heads[i])
		// 列表只带摘要，不带全部字段正文
		view.Fields = nil
		view.Status = nil
		out = append(out, view)
	}
	return textResult(out)
}

func (m *managers) ledgerGet(_ context.Context, _ *mcp.CallToolRequest, in ledgerGetInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	group, entity, err := ledgerAddr(in.Group, in.Entity)
	if err != nil {
		return errResult(err)
	}
	h, err := m.ledger.GetHead(group, entity)
	if err != nil {
		return errResult(err)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	recs, err := m.ledger.Log(group, entity, limit)
	if err != nil {
		return errResult(err)
	}
	list := make([]ledger.Record, 0, len(recs))
	for _, r := range recs {
		list = append(list, r.Record)
	}
	return textResult(ledgerGetOut{Head: m.ledgerHeadView(h), Records: list})
}

func (m *managers) ledgerAppend(_ context.Context, _ *mcp.CallToolRequest, in ledgerAppendInput) (*mcp.CallToolResult, emptyOut, error) {
	group, entity, err := ledgerAddr(in.Group, in.Entity)
	if err != nil {
		return errResult(err)
	}
	host, _ := os.Hostname()
	rec, key, err := m.ledger.Append(group, entity, ledger.RecordInput{
		Kind: in.Kind, Summary: in.Summary, Body: in.Body,
		At:         parseLedgerDate(in.At),
		By:         ledger.By{Host: host, User: os.Getenv("USER"), Agent: in.Agent},
		Supersedes: in.Supersedes,
	})
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]any{"record": key, "summary": rec.Summary, "kind": rec.Kind})
}

func (m *managers) ledgerUpdate(_ context.Context, _ *mcp.CallToolRequest, in ledgerUpdateInput) (*mcp.CallToolResult, emptyOut, error) {
	group, entity, err := ledgerAddr(in.Group, in.Entity)
	if err != nil {
		return errResult(err)
	}
	switch {
	case in.Field != "":
		if err := m.ledger.EditField(group, entity, in.Field, in.Value); err != nil {
			return errResult(err)
		}
		return textResult(map[string]string{"updated": "field", "field": in.Field})
	case in.Status != "":
		host, _ := os.Hostname()
		if err := m.ledger.SetStatus(group, entity, ledger.StatusEntry{
			Text: in.Status, Level: in.Level,
			By: ledger.By{Host: host, User: os.Getenv("USER"), Agent: in.Agent},
		}); err != nil {
			return errResult(err)
		}
		return textResult(map[string]string{"updated": "status"})
	default:
		return errResult(fmt.Errorf("需要 field 或 status 之一"))
	}
}

func ledgerAddr(group, entity string) (string, string, error) {
	if group == "" {
		group = "default"
	}
	if _, _, err := ledger.SplitHeadKey(entity); err != nil {
		return "", "", err
	}
	return group, entity, nil
}

func parseLedgerDate(raw string) (t time.Time) {
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return parsed
	}
	return time.Time{}
}
