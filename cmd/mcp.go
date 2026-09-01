package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

// mcpCmd is the parent for all MCP-related subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol (MCP) integration",
	Long: `Expose senv's secret/config capabilities to local AI agents over MCP (stdio),
or install the MCP server into an agent's configuration.

Typical flow:
  senv session start      # authenticate once (MCP servers cannot prompt)
  senv mcp install cursor # write senv into the agent's config
  # restart the agent; the senv_* tools are now available`,
}

// mcpServeCmd runs the stdio MCP server. It authenticates once at startup via
// the cached session (see resolveAuth); when no valid session exists it exits
// with a clear error instead of blocking on a password prompt, because MCP
// child processes are generally not attached to a TTY.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the senv MCP server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		envMgr, textMgr, configMgr, err := getManagersForMCP()
		if err != nil {
			return err
		}
		srv := newMCPServer(envMgr, textMgr, configMgr)
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

// managers bundles the three managers a handler needs. Handlers close over a
// pointer to this struct so the same value is shared by every tool.
type managers struct {
	env    *env.Manager
	text   *text.Manager
	config *config.Manager
}

// getManagersForMCP authenticates exactly once at server startup and returns
// the env/text/config managers. It reuses the process auth memo, so it is
// consistent with the rest of the CLI. An MCP server cannot prompt for a
// password (its stdin is the JSON-RPC transport), so a missing session surfaces
// as ErrNeedSession with guidance to run `senv session start`.
func getManagersForMCP() (*env.Manager, *text.Manager, *config.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		if errors.Is(err, ErrNeedSession) {
			return nil, nil, nil, fmt.Errorf("%w\nMCP servers run non-interactively; start a session first", err)
		}
		return nil, nil, nil, err
	}
	if auth.hasKey() {
		return env.NewManagerWithKey(auth.storage, auth.key),
			text.NewManagerWithKey(auth.storage, auth.key),
			config.NewManagerWithKey(auth.storage, auth.key),
			nil
	}
	return env.NewManager(auth.storage, auth.password),
		text.NewManager(auth.storage, auth.password),
		config.NewManager(auth.storage, auth.password),
		nil
}

// newMCPServer builds the MCP server and registers all senv tools. Each handler
// is a thin adapter around the shared managers; business logic stays in the
// internal packages (same managers the CLI uses).
func newMCPServer(envMgr *env.Manager, textMgr *text.Manager, configMgr *config.Manager) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "senv", Version: Version}, nil)
	mgrs := &managers{env: envMgr, text: textMgr, config: configMgr}
	registerMCPTools(srv, mgrs)
	return srv
}

// --- Input schemas -----------------------------------------------------------

type envKeyInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group name; if omitted the tool uses the key's group:key address or the default group"`
	Key   string `json:"key" jsonschema_description:"variable key, or a group:key address"`
}

type envSetValueInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group name; overrides any group in key"`
	Key   string `json:"key" jsonschema_description:"variable key, or a group:key address"`
	Value string `json:"value" jsonschema_description:"secret value to store"`
}

type envGetInput struct {
	Group  string `json:"group,omitempty" jsonschema_description:"optional group name"`
	Key    string `json:"key" jsonschema_description:"variable key, or a group:key address"`
	Decode bool   `json:"decode,omitempty" jsonschema_description:"resolve {{env:...}} and {{text:...}} references"`
}

type listInput struct {
	Group string `json:"group,omitempty" jsonschema_description:"optional group to restrict the listing to"`
}

type groupKindInput struct {
	Kind string `json:"kind" jsonschema_description:"group namespace; one of \"env\" or \"text\""`
	Name string `json:"name" jsonschema_description:"group name"`
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
	return textResult(map[string]string{"group": group, "key": key, "value": value})
}

func (m *managers) envSet(_ context.Context, _ *mcp.CallToolRequest, in envSetValueInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	if err := m.env.Set(group, key, in.Value); err != nil {
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
	group := orDefault(in.Group, "")
	vars, err := m.env.List(group)
	if err != nil {
		return errResult(err)
	}
	// Drop empty non-default groups to mirror the CLI listing.
	out := make(map[string]map[string]string, len(vars))
	for g, variables := range vars {
		if len(variables) == 0 {
			continue
		}
		row := make(map[string]string, len(variables))
		for k, v := range variables {
			row[k] = v
		}
		out[g] = row
	}
	return textResult(out)
}

func (m *managers) envExport(_ context.Context, _ *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, emptyOut, error) {
	exports, err := m.env.Export()
	if err != nil {
		return errResult(err)
	}
	resolved, err := resolveValueWith(exports, false, "", m.env, m.text)
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"exports": resolved})
}

func (m *managers) textGet(_ context.Context, _ *mcp.CallToolRequest, in envGetInput) (*mcp.CallToolResult, emptyOut, error) {
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
	return textResult(map[string]string{"group": group, "key": key, "value": value})
}

func (m *managers) textSet(_ context.Context, _ *mcp.CallToolRequest, in envSetValueInput) (*mcp.CallToolResult, emptyOut, error) {
	group, key := resolveAddressKey(in.Key, orDefault(in.Group, "default"))
	if err := m.text.Set(group, key, in.Value); err != nil {
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
	type entry struct {
		Group string `json:"group"`
		Key   string `json:"key"`
		Size  int    `json:"size"`
	}
	var out []entry
	addGroup := func(group string) error {
		infos, err := m.text.List(group)
		if err != nil {
			return err
		}
		for _, info := range infos {
			out = append(out, entry{Group: group, Key: info.Key, Size: info.Size})
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

func (m *managers) configList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	infos, err := m.config.List("")
	if err != nil {
		return errResult(err)
	}
	return textResult(infos)
}

func (m *managers) configGet(_ context.Context, _ *mcp.CallToolRequest, in configNameInput) (*mcp.CallToolResult, emptyOut, error) {
	info, err := m.config.Get(in.Name)
	if err != nil {
		return errResult(err)
	}
	return textResult(info)
}

func (m *managers) configExport(_ context.Context, _ *mcp.CallToolRequest, in configNameInput) (*mcp.CallToolResult, emptyOut, error) {
	// Export to a temp file then read it back, so we never write to a
	// user-specified path from within an MCP tool. The content is returned as
	// text; callers that need a file can write it themselves.
	tmp, err := os.CreateTemp("", "senv-mcp-config-*")
	if err != nil {
		return errResult(err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := m.config.Export(in.Name, tmpPath); err != nil {
		return errResult(err)
	}
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"name": in.Name, "content": string(content)})
}

func (m *managers) groupList(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, emptyOut, error) {
	switch orDefault(in.Group, "") {
	// We overload the otherwise-unused Group field with the namespace to avoid a
	// bespoke input type. Accept "text" (and empty) => text groups; otherwise env.
	case "text":
		groups, err := m.text.ListGroups()
		if err != nil {
			return errResult(err)
		}
		type g struct {
			Name     string `json:"name"`
			KeyCount int    `json:"keyCount"`
		}
		out := make([]g, 0, len(groups))
		for _, gr := range groups {
			out = append(out, g{Name: gr.Name, KeyCount: gr.KeyCount})
		}
		return textResult(out)
	default:
		groups, err := m.env.ListGroups()
		if err != nil {
			return errResult(err)
		}
		type g struct {
			Name      string `json:"name"`
			IsActive  bool   `json:"isActive"`
			VarCount  int    `json:"varCount"`
			IsDefault bool   `json:"isDefault"`
		}
		out := make([]g, 0, len(groups))
		for _, gr := range groups {
			out = append(out, g{Name: gr.Name, IsActive: gr.IsActive, VarCount: gr.VarCount, IsDefault: gr.IsDefault})
		}
		return textResult(out)
	}
}

func (m *managers) groupAdd(_ context.Context, _ *mcp.CallToolRequest, in groupKindInput) (*mcp.CallToolResult, emptyOut, error) {
	if in.Kind != "env" && in.Kind != "text" {
		return errResult(fmt.Errorf("invalid kind %q: must be \"env\" or \"text\"", in.Kind))
	}
	switch in.Kind {
	case "text":
		if err := m.text.AddGroup(in.Name); err != nil {
			return errResult(err)
		}
	default: // env
		if err := m.env.AddGroup(in.Name); err != nil {
			return errResult(err)
		}
	}
	return textResult(map[string]string{"status": "created", "kind": in.Kind, "name": in.Name})
}

func (m *managers) groupActivate(_ context.Context, _ *mcp.CallToolRequest, in groupNameInput) (*mcp.CallToolResult, emptyOut, error) {
	if err := m.env.ActivateGroup(in.Name); err != nil {
		return errResult(err)
	}
	return textResult(map[string]string{"status": "activated", "name": in.Name})
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
func registerMCPTools(s *mcp.Server, m *managers) {
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_get", Description: "Get an environment variable (secret). Set decode=true to resolve {{env:...}}/{{text:...}} references."}, m.envGet)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_set", Description: "Set (store) an environment variable secret."}, m.envSet)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_delete", Description: "Delete an environment variable."}, m.envDelete)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_list", Description: "List environment variables, optionally restricted to a group."}, m.envList)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_env_export", Description: "Export active-group environment variables as shell export statements, with references resolved."}, m.envExport)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_get", Description: "Get a text block (key/cert/template). decode=true resolves references."}, m.textGet)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_set", Description: "Set a text block."}, m.textSet)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_delete", Description: "Delete a text block."}, m.textDelete)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_text_list", Description: "List text blocks, optionally restricted to a group."}, m.textList)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_list", Description: "List stored config files."}, m.configList)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_get", Description: "Get metadata for a stored config file."}, m.configGet)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_config_export", Description: "Export a stored config file and return its content."}, m.configExport)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_list", Description: "List groups. Pass group=\"text\" for text groups; otherwise env groups."}, m.groupList)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_add", Description: "Create a group (kind=env|text)."}, m.groupAdd)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_activate", Description: "Activate an env group (included in env export)."}, m.groupActivate)
	mcp.AddTool(s, &mcp.Tool{Name: "senv_group_deactivate", Description: "Deactivate an env group."}, m.groupDeactivate)
}

// toolCatalogue mirrors registerMCPTools for offline listing (list-tools).
func toolCatalogue() []toolDef {
	return []toolDef{
		{"senv_env_get", "Get an environment variable (secret). decode=true resolves references."},
		{"senv_env_set", "Set (store) an environment variable secret."},
		{"senv_env_delete", "Delete an environment variable."},
		{"senv_env_list", "List environment variables, optionally by group."},
		{"senv_env_export", "Export active-group env vars as shell statements (references resolved)."},
		{"senv_text_get", "Get a text block. decode=true resolves references."},
		{"senv_text_set", "Set a text block."},
		{"senv_text_delete", "Delete a text block."},
		{"senv_text_list", "List text blocks, optionally by group."},
		{"senv_config_list", "List stored config files."},
		{"senv_config_get", "Get metadata for a stored config file."},
		{"senv_config_export", "Export a stored config file and return its content."},
		{"senv_group_list", "List groups (group=text for text groups)."},
		{"senv_group_add", "Create a group (kind=env|text)."},
		{"senv_group_activate", "Activate an env group."},
		{"senv_group_deactivate", "Deactivate an env group."},
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
