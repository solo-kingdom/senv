package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	mcpserver "github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// mcpCmd is the parent for all MCP-related subcommands: senv's own MCP server
// (serve/install/list-tools) and user-owned MCP server profiles
// (add/get/edit/list/delete/export/unexport).
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol (MCP) integration",
	Long: `Expose senv's secret/config capabilities to local AI agents over MCP (stdio),
install senv's own MCP server into an agent's configuration, and manage your own
MCP server profiles so they can be exported into several agents at once.

Typical flow:
  senv session start      # authenticate once (MCP servers cannot prompt)
  senv mcp install cursor # write senv into the agent's config
  # restart the agent; the senv_* tools are now available

  senv mcp add github ... # store one of your own MCP servers
  senv mcp export --all   # export it into every supported agent's config`,
}

// mcpServeCmd runs the stdio MCP server. It validates a cached session at
// startup but retains only a non-secret fingerprint; every business request
// reloads and revalidates that exact session before constructing managers.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the senv MCP server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, dataPath := getConfigPath(), getDataPath()
		authorization, err := getMCPAuthorization(configPath, dataPath)
		if err != nil {
			return err
		}
		srv := newAuthorizedMCPServer(
			newMCPRequestAuthorizer(configPath, dataPath, authorization),
			newAutoPuller(cmd),
		)
		return srv.Run(cmd.Context(), &mcp.StdioTransport{})
	},
}

// mcpListToolsCmd prints the registered MCP tool catalogue. Useful as a smoke
// test and for users/agents to inspect the available surface without running a
// full MCP handshake.
var mcpListToolsCmd = &cobra.Command{
	Use:   "list-tools",
	Short: "List the MCP tools exposed by senv",
	RunE: func(cmd *cobra.Command, args []string) error {
		// listTools does not require auth; it only reflects the tool registry.
		catalogue := toolCatalogue()
		fmt.Printf("senv MCP tools (%d):\n", len(catalogue))
		for _, t := range catalogue {
			fmt.Printf("  %-26s %s\n", t.Name, t.Description)
		}
		return nil
	},
}

// managers exists only for the lifetime of one authorized tool request.
type managers struct {
	env        *env.Manager
	text       mcpTextManager
	backup     *backup.Manager
	config     *config.Manager
	ssh        *ssh.Manager
	llm        *llm.ProviderManager
	llmPointer string
	llmHome    string
	mcpServer  *mcpserver.Manager
	autoPull   func()
}

// mcpTextManager 在 MCP 暴露面包裹 text.Manager：值读写（Get/Set/Delete）
// 一律拒绝保留组 llm-keys——该组存放 LLM API key 明文凭据，
// llm_provider_list 的白名单视图刻意不暴露它们，senv_text_get 或
// {{text:llm-keys/...}} 引用解析不得成为绕过面（CLI/TUI 仍可全权访问）。
// 元数据（List/ListGroups）保持可用，暴露面与 llm_provider_list 的
// credential_ref 一致。
type mcpTextManager struct {
	*text.Manager
}

// errLLMKeysReserved 是 MCP 侧访问 llm-keys 保留组的统一脱敏错误
var errLLMKeysReserved = fmt.Errorf("text group %q is reserved for LLM credentials and not accessible via MCP; manage it with the senv CLI", llm.LLMKeysGroup)

func (m mcpTextManager) Get(group, key string) (string, error) {
	if group == llm.LLMKeysGroup {
		return "", errLLMKeysReserved
	}
	return m.Manager.Get(group, key)
}

func (m mcpTextManager) Set(group, key, value string) error {
	if group == llm.LLMKeysGroup {
		return errLLMKeysReserved
	}
	return m.Manager.Set(group, key, value)
}

func (m mcpTextManager) SetWithDescription(group, key, value string, description *string) error {
	if group == llm.LLMKeysGroup {
		return errLLMKeysReserved
	}
	return m.Manager.SetWithDescription(group, key, value, description)
}

func (m mcpTextManager) AddGroup(name, description string) error {
	if name == llm.LLMKeysGroup {
		return errLLMKeysReserved
	}
	return m.Manager.AddGroup(name, description)
}

func (m mcpTextManager) GetWithMeta(group, key string) (string, string, error) {
	if group == llm.LLMKeysGroup {
		return "", "", errLLMKeysReserved
	}
	return m.Manager.GetWithMeta(group, key)
}

func (m mcpTextManager) Delete(group, key string) error {
	if group == llm.LLMKeysGroup {
		return errLLMKeysReserved
	}
	return m.Manager.Delete(group, key)
}

type mcpRequestAuthorizer func() (*managers, func(), error)

// getMCPAuthorization validates startup without retaining a password, key, or
// business manager. Loading metadata first preserves KDF fail-fast behavior.
func getMCPAuthorization(configPath, dataPath string) (*session.MCPAuthorization, error) {
	store := storage.NewManager(configPath, dataPath)
	if !store.IsInitialized() {
		return nil, errNotInitialized
	}
	if _, err := store.LoadMetadata(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}
	sessionManager := session.NewManager(configPath, dataPath)
	defer sessionManager.Close()
	authorization, err := sessionManager.AuthorizeMCPStartup()
	if err != nil {
		if errors.Is(err, session.ErrNoSession) || errors.Is(err, session.ErrSessionExpired) {
			return nil, fmt.Errorf("%w\nMCP servers run non-interactively; start a session first", ErrNeedSession)
		}
		return nil, err
	}
	return authorization, nil
}

func newMCPRequestAuthorizer(configPath, dataPath string, authorization *session.MCPAuthorization) mcpRequestAuthorizer {
	return func() (*managers, func(), error) {
		sessionManager := session.NewManager(configPath, dataPath)
		key, err := sessionManager.AuthorizeMCPRequest(authorization)
		_ = sessionManager.Close()
		if err != nil {
			return nil, nil, err
		}

		// Business managers are constructed only after the centralized guard has
		// validated the exact startup session.
		store := storage.NewManager(configPath, dataPath)
		// 指针文件路径随 configPath 解析；agent 配置根为用户 home（本机状态，
		// 读取不经 vault，但工具统一走 guard 鉴权）。
		home, err := agentHomeDir()
		if err != nil {
			return nil, nil, err
		}
		requestManagers := &managers{
			env:        env.NewManagerWithKey(store, key),
			text:       mcpTextManager{text.NewManagerWithKey(store, key)},
			backup:     backup.NewManagerWithKey(store, key),
			config:     config.NewManagerWithKey(store, key),
			ssh:        ssh.NewManagerWithKey(store, key),
			llm:        llm.NewProviderManagerWithKey(store, key),
			llmPointer: filepath.Join(configPath, "agent-pointers.json"),
			llmHome:    home,
			mcpServer:  mcpserver.NewManagerWithKey(store, key),
		}
		release := func() {
			session.ZeroKey(key)
			requestManagers.env = nil
			requestManagers.text = mcpTextManager{}
			requestManagers.backup = nil
			requestManagers.config = nil
			requestManagers.ssh = nil
			requestManagers.llm = nil
			requestManagers.mcpServer = nil
		}
		return requestManagers, release, nil
	}
}

func newAuthorizedMCPServer(authorize mcpRequestAuthorizer, autoPull func()) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "senv", Version: Version}, nil)
	registerMCPTools(srv, authorize, autoPull)
	return srv
}

// guardMCPTool 是每个 MCP 工具的统一包裹层：会话鉴权 → 执行 → 审计。
// 审计只记录工具名与结果（target "mcp:<tool>"），绝不包含任何输入值。
func guardMCPTool[Input any](toolName string, authorize mcpRequestAuthorizer, autoPull func(), handler func(*managers, context.Context, *mcp.CallToolRequest, Input) (*mcp.CallToolResult, emptyOut, error)) func(context.Context, *mcp.CallToolRequest, Input) (*mcp.CallToolResult, emptyOut, error) {
	return func(ctx context.Context, request *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, emptyOut, error) {
		requestManagers, release, err := authorize()
		if err != nil {
			// 鉴权拒绝由 mcp_auth 的吊销审计覆盖，这里不重复记录
			return errResult(err)
		}
		defer release()
		requestManagers.autoPull = autoPull
		result, out, err := handler(requestManagers, ctx, request, input)
		success := err == nil && (result == nil || !result.IsError)
		auditOp(session.AuditOpMCPTool, "mcp:"+toolName, success, "")
		return result, out, err
	}
}

func (m *managers) pullBeforeRead() {
	if m.autoPull != nil {
		m.autoPull()
	}
}

// --- Input schemas -----------------------------------------------------------

type envKeyInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group name; if omitted the tool uses the key's group:key address or the default group"`
	Key   string `json:"key" jsonschema_description:"variable key, or a group:key address"`
}

type envSetValueInput struct {
	Group       string  `json:"group,omitempty" jsonschema_description:"optional group name; overrides any group in key"`
	Key         string  `json:"key" jsonschema_description:"variable key, or a group:key address"`
	Value       string  `json:"value" jsonschema_description:"secret value to store"`
	Description *string `json:"description,omitempty" jsonschema_description:"optional note; omit to keep the existing description"`
}

type envGetInput struct {
	Group  string `json:"group,omitempty" jsonschema_description:"optional group name"`
	Key    string `json:"key" jsonschema_description:"variable key, or a group:key address"`
	Decode bool   `json:"decode,omitempty" jsonschema_description:"resolve {{env:...}} and {{text:...}} references"`
}

type backupGetInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group name"`
	Key   string `json:"key" jsonschema_description:"backup key, or a group:key address"`
}

type listInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group to restrict the listing to"`
}

type groupKindInput struct {
	Kind        string `json:"kind" jsonschema_description:"group namespace; one of \"env\", \"text\", or \"backup\""`
	Name        string `json:"name" jsonschema_description:"group name"`
	Description string `json:"description" jsonschema_description:"required non-empty note describing the group"`
}

type groupNameInput struct {
	Name string `json:"name" jsonschema_description:"group name"`
}

type configNameInput struct {
	Name string `json:"name" jsonschema_description:"config file name"`
}

// --- Handlers ---------------------------------------------------------------
//
// Each handler returns a *CallToolResult carrying a JSON text payload plus an
// empty typed output. JSON keeps list/get results structured for the model
// while avoiding bespoke output struct definitions.

// emptyOut is the zero-cost typed output used by ToolHandlerFor handlers that
// only emit text content via CallToolResult.
type emptyOut struct{}

// empty is the single shared value of emptyOut returned by all handlers.
var empty = emptyOut{}

// textResult builds a CallToolResult whose content is a single JSON text block.
func textResult(payload any) (*mcp.CallToolResult, emptyOut, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, empty, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, empty, nil
}

// errResult reports a tool-level error (IsError=true) so the model can see and
// self-correct, per the MCP spec.
func errResult(err error) (*mcp.CallToolResult, emptyOut, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, empty, nil
}

func (m *managers) envGet(_ context.Context, _ *mcp.CallToolRequest, in envGetInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	value, err := m.env.Get(group, key)
	if err != nil {
		return errResult(err)
	}
	if in.Decode {
		resolved, err := resolveValueWith(value, false, group, m.env, m.text)
		if err != nil {
			return errResult(err)
		}
		value = resolved
	}
	_, desc, metaErr := m.env.GetWithMeta(group, key)
	if metaErr != nil {
		return errResult(metaErr)
	}
	out := map[string]string{"group": group, "key": key, "value": value}
	if desc != "" {
		out["description"] = desc
	}
	return textResult(out)
}

func (m *managers) envSet(_ context.Context, _ *mcp.CallToolRequest, in envSetValueInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	var err error
	if in.Description != nil {
		err = m.env.SetWithDescription(group, key, in.Value, in.Description)
	} else {
		err = m.env.Set(group, key, in.Value)
	}
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "ok", "group": group, "key": key})
}

func (m *managers) envDelete(_ context.Context, _ *mcp.CallToolRequest, in envKeyInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	if err := m.env.Delete(group, key); err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "deleted", "group": group, "key": key})
}

func (m *managers) envList(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	group := orDefault(in.Group, "")
	vars, err := m.env.ListVarInfo(group)
	if err != nil {
		return errResult(err)
	}
	type item struct {
		Value       string `json:"value"`
		Description string `json:"description,omitempty"`
	}
	out := make(map[string]map[string]item, len(vars))
	for g, variables := range vars {
		if len(variables) == 0 {
			continue
		}
		row := make(map[string]item, len(variables))
		for k, info := range variables {
			row[k] = item{Value: info.Value, Description: info.Description}
		}
		out[g] = row
	}
	return textResult(out)
}

func (m *managers) envExport(_ context.Context, _ *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	resolved, warnings, err := resolveExportShell(m.env, m.text, "default")
	if err != nil {
		return errResult(err)
	}
	out := map[string]any{"exports": resolved}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return textResult(out)
}

func (m *managers) textGet(_ context.Context, _ *mcp.CallToolRequest, in envGetInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	value, err := m.text.Get(group, key)
	if err != nil {
		return errResult(err)
	}
	if in.Decode {
		resolved, err := resolveValueWith(value, false, group, m.env, m.text)
		if err != nil {
			return errResult(err)
		}
		value = resolved
	}
	_, desc, metaErr := m.text.GetWithMeta(group, key)
	if metaErr != nil {
		return errResult(metaErr)
	}
	out := map[string]string{"group": group, "key": key, "value": value}
	if desc != "" {
		out["description"] = desc
	}
	return textResult(out)
}

func (m *managers) textSet(_ context.Context, _ *mcp.CallToolRequest, in envSetValueInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	var err error
	if in.Description != nil {
		err = m.text.SetWithDescription(group, key, in.Value, in.Description)
	} else {
		err = m.text.Set(group, key, in.Value)
	}
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "ok", "group": group, "key": key})
}

func (m *managers) textDelete(_ context.Context, _ *mcp.CallToolRequest, in envKeyInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	if err := m.text.Delete(group, key); err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "deleted", "group": group, "key": key})
}

func (m *managers) textList(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	type entry struct {
		Group       string `json:"group"`
		Key         string `json:"key"`
		Size        int    `json:"size"`
		Description string `json:"description,omitempty"`
	}
	var out []entry
	addGroup := func(group string) error {
		infos, err := m.text.List(group)
		if err != nil {
			return err
		}
		for _, info := range infos {
			out = append(out, entry{Group: group, Key: info.Key, Size: info.Size, Description: info.Description})
		}
		return nil
	}
	if in.Group != "" {
		if err := addGroup(in.Group); err != nil {
			return errResult(err)
		}
	} else {
		groups, err := m.text.ListGroups()
		if err != nil {
			return errResult(err)
		}
		for _, gr := range groups {
			if err := addGroup(gr.Name); err != nil {
				return errResult(err)
			}
		}
	}
	return textResult(out)
}

func (m *managers) backupGet(_ context.Context, _ *mcp.CallToolRequest, in backupGetInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	if err := m.ensureBackup(); err != nil {
		return errResult(err)
	}
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	value, err := m.backup.Get(group, key)
	if err != nil {
		return errResult(err)
	}
	_, desc, metaErr := m.backup.GetWithMeta(group, key)
	if metaErr != nil {
		return errResult(metaErr)
	}
	out := map[string]string{"group": group, "key": key, "value": value}
	if desc != "" {
		out["description"] = desc
	}
	return textResult(out)
}

func (m *managers) backupSet(_ context.Context, _ *mcp.CallToolRequest, in envSetValueInput) (*mcp.CallToolResult, emptyOut, error) {
	if err := m.ensureBackup(); err != nil {
		return errResult(err)
	}
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	var err error
	if in.Description != nil {
		err = m.backup.SetWithDescription(group, key, in.Value, in.Description)
	} else {
		err = m.backup.Set(group, key, in.Value)
	}
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "ok", "group": group, "key": key})
}

func (m *managers) backupDelete(_ context.Context, _ *mcp.CallToolRequest, in envKeyInput) (*mcp.CallToolResult, emptyOut, error) {
	if err := m.ensureBackup(); err != nil {
		return errResult(err)
	}
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	if err := m.backup.Delete(group, key); err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "deleted", "group": group, "key": key})
}

func (m *managers) backupList(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	if err := m.ensureBackup(); err != nil {
		return errResult(err)
	}
	type entry struct {
		Group       string `json:"group"`
		Key         string `json:"key"`
		Size        int    `json:"size"`
		Description string `json:"description,omitempty"`
	}
	var out []entry
	addGroup := func(group string) error {
		infos, err := m.backup.List(group)
		if err != nil {
			return err
		}
		for _, info := range infos {
			out = append(out, entry{Group: group, Key: info.Key, Size: info.Size, Description: info.Description})
		}
		return nil
	}
	if in.Group != "" {
		if err := addGroup(in.Group); err != nil {
			return errResult(err)
		}
	} else {
		groups, err := m.backup.ListGroups()
		if err != nil {
			return errResult(err)
		}
		for _, gr := range groups {
			if err := addGroup(gr.Name); err != nil {
				return errResult(err)
			}
		}
	}
	return textResult(out)
}

func (m *managers) ensureBackup() error {
	if m.backup == nil {
		return fmt.Errorf("backup manager unavailable")
	}
	return m.backup.EnsureDefault()
}

func (m *managers) configList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	infos, err := m.config.List("")
	if err != nil {
		return errResult(err)
	}
	return textResult(infos)
}

func (m *managers) configGet(_ context.Context, _ *mcp.CallToolRequest, in configNameInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	info, err := m.config.Get(in.Name)
	if err != nil {
		return errResult(err)
	}
	return textResult(info)
}

func (m *managers) configExport(_ context.Context, _ *mcp.CallToolRequest, in configNameInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	// Decrypt in memory only: plaintext never lands in a temp file. Callers
	// that need a file on disk can write the returned content themselves.
	content, err := m.config.Content(in.Name)
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"name": in.Name, "content": string(content)})
}

func (m *managers) groupList(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	switch orDefault(in.Group, "") {
	// We overload the otherwise-unused Group field with the namespace to avoid a
	// bespoke input type. Accept "text"/"backup"; otherwise env groups.
	case "text":
		groups, err := m.text.ListGroups()
		if err != nil {
			return errResult(err)
		}
		type g struct {
			Name        string `json:"name"`
			KeyCount    int    `json:"keyCount"`
			Description string `json:"description,omitempty"`
		}
		out := make([]g, 0, len(groups))
		for _, gr := range groups {
			out = append(out, g{Name: gr.Name, KeyCount: gr.KeyCount, Description: gr.Description})
		}
		return textResult(out)
	case "backup":
		if err := m.ensureBackup(); err != nil {
			return errResult(err)
		}
		groups, err := m.backup.ListGroups()
		if err != nil {
			return errResult(err)
		}
		type g struct {
			Name        string `json:"name"`
			KeyCount    int    `json:"keyCount"`
			Description string `json:"description,omitempty"`
		}
		out := make([]g, 0, len(groups))
		for _, gr := range groups {
			out = append(out, g{Name: gr.Name, KeyCount: gr.KeyCount, Description: gr.Description})
		}
		return textResult(out)
	default:
		groups, err := m.env.ListGroups()
		if err != nil {
			return errResult(err)
		}
		type g struct {
			Name        string `json:"name"`
			IsActive    bool   `json:"isActive"`
			VarCount    int    `json:"varCount"`
			IsDefault   bool   `json:"isDefault"`
			Description string `json:"description,omitempty"`
		}
		out := make([]g, 0, len(groups))
		for _, gr := range groups {
			out = append(out, g{Name: gr.Name, IsActive: gr.IsActive, VarCount: gr.VarCount, IsDefault: gr.IsDefault, Description: gr.Description})
		}
		return textResult(out)
	}
}

func (m *managers) groupAdd(_ context.Context, _ *mcp.CallToolRequest, in groupKindInput) (*mcp.CallToolResult, emptyOut, error) {
	if in.Kind != "env" && in.Kind != "text" && in.Kind != "backup" {
		return errResult(fmt.Errorf("invalid kind %q: must be \"env\", \"text\", or \"backup\"", in.Kind))
	}
	switch in.Kind {
	case "text":
		if err := m.text.AddGroup(in.Name, in.Description); err != nil {
			return errResult(err)
		}
	case "backup":
		if err := m.ensureBackup(); err != nil {
			return errResult(err)
		}
		if err := m.backup.AddGroup(in.Name, in.Description); err != nil {
			return errResult(err)
		}
	default: // env
		if err := m.env.AddGroup(in.Name, in.Description); err != nil {
			return errResult(err)
		}
	}
	return textResult(map[string]string{"status": "created", "kind": in.Kind, "name": in.Name})
}

func (m *managers) groupActivate(_ context.Context, _ *mcp.CallToolRequest, in groupNameInput) (*mcp.CallToolResult, emptyOut, error) {
	if err := m.env.ActivateGroup(in.Name); err != nil {
		return errResult(err)
	}
	out := map[string]any{"status": "activated", "name": in.Name}
	if warnings, err := m.env.KeyCollisionWarnings(); err != nil {
		return errResult(err)
	} else if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return textResult(out)
}

func (m *managers) groupDeactivate(_ context.Context, _ *mcp.CallToolRequest, in groupNameInput) (*mcp.CallToolResult, emptyOut, error) {
	if err := m.env.DeactivateGroup(in.Name); err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "deactivated", "name": in.Name})
}

// --- Registration -----------------------------------------------------------

// toolDef pairs an MCP Tool descriptor with enough info to also list it offline.
type toolDef struct {
	Name        string
	Description string
}

// registerMCPTools attaches every senv tool to the server. Keep this list in
// sync with toolCatalogue below.
func registerMCPTools(s *mcp.Server, authorize mcpRequestAuthorizer, autoPull func()) {
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_get", Description: "Get an environment variable (secret). Set decode=true to resolve {{env:...}}/{{text:...}} references."}, guardMCPTool("senv_env_get", authorize, autoPull, (*managers).envGet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_set", Description: "Set (store) an environment variable secret. Optional description is a vault note; omit to keep the existing note."}, guardMCPTool("senv_env_set", authorize, autoPull, (*managers).envSet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_delete", Description: "Delete an environment variable."}, guardMCPTool("senv_env_delete", authorize, autoPull, (*managers).envDelete))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_list", Description: "List environment variables with values and descriptions, optionally restricted to a group."}, guardMCPTool("senv_env_list", authorize, autoPull, (*managers).envList))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_export", Description: "Export active-group environment variables as shell export statements, with references resolved."}, guardMCPTool("senv_env_export", authorize, autoPull, (*managers).envExport))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_get", Description: "Get a text block (key/cert/template). decode=true resolves references."}, guardMCPTool("senv_text_get", authorize, autoPull, (*managers).textGet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_set", Description: "Set a text block. Optional description is a vault note; omit to keep the existing note."}, guardMCPTool("senv_text_set", authorize, autoPull, (*managers).textSet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_delete", Description: "Delete a text block."}, guardMCPTool("senv_text_delete", authorize, autoPull, (*managers).textDelete))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_list", Description: "List text blocks, optionally restricted to a group."}, guardMCPTool("senv_text_list", authorize, autoPull, (*managers).textList))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_backup_get", Description: "Get a backup block. References are never resolved."}, guardMCPTool("senv_backup_get", authorize, autoPull, (*managers).backupGet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_backup_set", Description: "Set a backup block. Optional description is a vault note; omit to keep the existing note."}, guardMCPTool("senv_backup_set", authorize, autoPull, (*managers).backupSet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_backup_delete", Description: "Delete a backup block."}, guardMCPTool("senv_backup_delete", authorize, autoPull, (*managers).backupDelete))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_backup_list", Description: "List backup blocks (no values), optionally restricted to a group."}, guardMCPTool("senv_backup_list", authorize, autoPull, (*managers).backupList))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_list", Description: "List stored config files."}, guardMCPTool("senv_config_list", authorize, autoPull, (*managers).configList))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_get", Description: "Get metadata for a stored config file."}, guardMCPTool("senv_config_get", authorize, autoPull, (*managers).configGet))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_export", Description: "Export a stored config file and return its content."}, guardMCPTool("senv_config_export", authorize, autoPull, (*managers).configExport))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_list", Description: "List groups. Pass group=\"text\" or group=\"backup\"; otherwise env groups."}, guardMCPTool("senv_group_list", authorize, autoPull, (*managers).groupList))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_add", Description: "Create a group (kind=env|text|backup). description is required."}, guardMCPTool("senv_group_add", authorize, autoPull, (*managers).groupAdd))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_activate", Description: "Activate an env group (included in env export)."}, guardMCPTool("senv_group_activate", authorize, autoPull, (*managers).groupActivate))
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_deactivate", Description: "Deactivate an env group."}, guardMCPTool("senv_group_deactivate", authorize, autoPull, (*managers).groupDeactivate))
	mcp.AddTool(s, &mcp.Tool{Name: "ssh_host_list", Description: "List SSH host connection metadata (read-only; no private keys)."}, guardMCPTool("ssh_host_list", authorize, autoPull, (*managers).sshHostList))
	mcp.AddTool(s, &mcp.Tool{Name: "ssh_host_get", Description: "Get one SSH host connection metadata record (read-only; no private keys)."}, guardMCPTool("ssh_host_get", authorize, autoPull, (*managers).sshHostGet))
	mcp.AddTool(s, &mcp.Tool{Name: "llm_provider_list", Description: "List saved LLM provider profiles (read-only; credential references only, no secrets)."}, guardMCPTool("llm_provider_list", authorize, autoPull, (*managers).llmProviderList))
	mcp.AddTool(s, &mcp.Tool{Name: "llm_agent_status", Description: "Show each coding agent's current provider/model pointer (read-only, local state)."}, guardMCPTool("llm_agent_status", authorize, autoPull, (*managers).llmAgentStatus))
	mcp.AddTool(s, &mcp.Tool{Name: "mcp_server_list", Description: "List stored MCP server profiles (read-only; alias/transport/description only, no env values)."}, guardMCPTool("mcp_server_list", authorize, autoPull, (*managers).mcpServerList))
}

// toolCatalogue mirrors registerMCPTools for offline listing (list-tools).
func toolCatalogue() []toolDef {
	return []toolDef{
		{"senv_env_get", "Get an environment variable (secret). decode=true resolves references."},
		{"senv_env_set", "Set (store) an environment variable secret. Optional description is a vault note; omit to keep the existing note."},
		{"senv_env_delete", "Delete an environment variable."},
		{"senv_env_list", "List environment variables with values and descriptions, optionally by group."},
		{"senv_env_export", "Export active-group env vars as shell statements (references resolved)."},
		{"senv_text_get", "Get a text block. decode=true resolves references."},
		{"senv_text_set", "Set a text block. Optional description is a vault note; omit to keep the existing note."},
		{"senv_text_delete", "Delete a text block."},
		{"senv_text_list", "List text blocks, optionally by group."},
		{"senv_backup_get", "Get a backup block. References are never resolved."},
		{"senv_backup_set", "Set a backup block. Optional description is a vault note; omit to keep the existing note."},
		{"senv_backup_delete", "Delete a backup block."},
		{"senv_backup_list", "List backup blocks (no values), optionally by group."},
		{"senv_config_list", "List stored config files."},
		{"senv_config_get", "Get metadata for a stored config file."},
		{"senv_config_export", "Export a stored config file and return its content."},
		{"senv_group_list", "List groups. Pass group=\"text\" or group=\"backup\"; otherwise env groups."},
		{"senv_group_add", "Create a group (kind=env|text|backup). description is required."},
		{"senv_group_activate", "Activate an env group."},
		{"senv_group_deactivate", "Deactivate an env group."},
		{"ssh_host_list", "List SSH host connection metadata (read-only; no private keys)."},
		{"ssh_host_get", "Get one SSH host metadata record (read-only; no private keys)."},
		{"llm_provider_list", "List saved LLM provider profiles (read-only; credential references only)."},
		{"llm_agent_status", "Show each coding agent's current provider/model pointer (read-only)."},
		{"mcp_server_list", "List stored MCP server profiles (read-only; no env values)."},
	}
}

// --- helpers ----------------------------------------------------------------

// orDefault returns v when non-empty, else dflt.
func orDefault(v, dflt string) string {
	if v != "" {
		return v
	}
	return dflt
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpServeCmd)
	mcpCmd.AddCommand(mcpListToolsCmd)
}
