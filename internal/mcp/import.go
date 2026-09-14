package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/storage"
)

// ParseImportFile loads MCP server entries from an agent config. TOML is
// detected by extension; everything else is parsed as JSON with an
// "mcpServers" object. Non-object entries become empty maps so callers can
// report a per-entry build failure instead of aborting the whole file.
func ParseImportFile(path string) (map[string]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var servers map[string]any
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		servers, err = agentcfg.TOMLServers(string(data), "mcp_servers")
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else {
		root, err := agentcfg.ReadJSONRoot(path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		servers = agentcfg.JSONServers(root, "mcpServers")
	}
	out := make(map[string]map[string]any, len(servers))
	for alias, raw := range servers {
		typed, _ := raw.(map[string]any)
		if typed == nil {
			typed = map[string]any{}
		}
		out[alias] = typed
	}
	return out, nil
}

// BuildImportEntry converts one raw config entry into a vault profile,
// detecting the transport. Values are kept raw: templates must survive.
func BuildImportEntry(alias string, raw map[string]any) (*storage.MCPServerEntry, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	transport, _ := raw["type"].(string)
	if transport == "" {
		// TOML configs (codex) spell the key "transport".
		transport, _ = raw["transport"].(string)
	}
	if transport == "streamable-http" {
		transport = storage.MCPTransportHTTP
	}
	url, hasURL := raw["url"].(string)
	command, hasCommand := raw["command"].(string)

	switch {
	case transport == storage.MCPTransportHTTP || transport == storage.MCPTransportSSE:
		if !hasURL || url == "" {
			return nil, fmt.Errorf("transport %s but no url", transport)
		}
		headers, err := stringMap(raw, "headers")
		if err != nil {
			return nil, err
		}
		return &storage.MCPServerEntry{Alias: alias, Transport: transport, URL: url, Headers: headers}, nil
	case transport == storage.MCPTransportStdio:
		if !hasCommand || command == "" {
			return nil, fmt.Errorf("transport stdio but no command")
		}
	case transport != "":
		return nil, fmt.Errorf("unsupported transport %q", transport)
	case hasURL && url != "":
		// The de-facto remote shape carries just a url; import it as http.
		headers, err := stringMap(raw, "headers")
		if err != nil {
			return nil, err
		}
		return &storage.MCPServerEntry{Alias: alias, Transport: storage.MCPTransportHTTP, URL: url, Headers: headers}, nil
	case hasCommand && command != "":
	default:
		return nil, fmt.Errorf("entry has neither url nor command")
	}

	env, err := stringMap(raw, "env")
	if err != nil {
		return nil, err
	}
	args, err := stringSlice(raw, "args")
	if err != nil {
		return nil, err
	}
	return &storage.MCPServerEntry{Alias: alias, Transport: storage.MCPTransportStdio, Command: command, Args: args, Env: env}, nil
}

// stringMap reads raw[field] as a map of strings, rejecting non-string values
// instead of silently dropping them: a dropped credential would export a
// server that cannot authenticate.
func stringMap(raw map[string]any, field string) (map[string]string, error) {
	switch value := raw[field].(type) {
	case nil:
		return nil, nil
	case map[string]string:
		return value, nil
	case map[string]any:
		out := make(map[string]string, len(value))
		for key, item := range value {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%q] is not a string", field, key)
			}
			out[key] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s is not a table", field)
	}
}

// stringSlice reads raw[field] as a list of strings, preserving order.
func stringSlice(raw map[string]any, field string) ([]string, error) {
	switch value := raw[field].(type) {
	case nil:
		return nil, nil
	case []string:
		return value, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s contains a non-string item", field)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s is not a list", field)
	}
}
