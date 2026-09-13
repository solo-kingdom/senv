package agentcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// BackupSuffix is appended to a config file before senv overwrites it. It is
// shared by `mcp install` and `mcp export` so both paths leave the same trace.
const BackupSuffix = ".bak"

// Server is the transport-agnostic profile senv writes into agent configs.
// Only the cross-agent common subset is represented: agent-specific keys are
// deliberately not passed through. A server with a non-empty URL is a remote
// entry (Transport "http"/"sse", Headers allowed, no Command/Args/Env);
// otherwise it is a stdio entry.
type Server struct {
	Transport string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Headers   map[string]string
}

// Fingerprint returns a stable digest of the server definition. The ledger
// stores it to tell "senv wrote this" from "someone else did, or it drifted".
// Canonicalization is explicit (sorted map keys, no struct tags) so a
// fingerprint never depends on encoding details of a particular format.
//
// Backward compatibility is load-bearing: transport/url/header fields only
// enter the digest when set, so fingerprints recorded before remote entries
// existed still match, and a senv upgrade never turns every existing export
// into drift.
func (s Server) Fingerprint() string {
	keys := make([]string, 0, len(s.Env))
	for key := range s.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	headerKeys := make([]string, 0, len(s.Headers))
	for key := range s.Headers {
		headerKeys = append(headerKeys, key)
	}
	sort.Strings(headerKeys)
	var b strings.Builder
	if s.Transport != "" && s.Transport != "stdio" {
		fmt.Fprintf(&b, "transport=%q\n", s.Transport)
	}
	fmt.Fprintf(&b, "command=%q\n", s.Command)
	for _, arg := range s.Args {
		fmt.Fprintf(&b, "arg=%q\n", arg)
	}
	for _, key := range keys {
		fmt.Fprintf(&b, "env=%q=%q\n", key, s.Env[key])
	}
	if s.URL != "" {
		fmt.Fprintf(&b, "url=%q\n", s.URL)
	}
	for _, key := range headerKeys {
		fmt.Fprintf(&b, "header=%q=%q\n", key, s.Headers[key])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ServerPath returns the config-file path for a target under a home directory.
func ServerPath(target Target, home, scope string) string {
	return target.ResolveConfigPath(home, scope)
}

// ---------------------------------------------------------------------------
// JSON configs
// ---------------------------------------------------------------------------

// ReadJSONRoot loads a JSON config file as a generic map so unknown keys are
// preserved verbatim. A missing file yields an empty root.
func ReadJSONRoot(path string) (map[string]any, error) {
	root := map[string]any{}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(existing) == 0 {
		return root, nil
	}
	if err := json.Unmarshal(existing, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return root, nil
}

// jsonTable resolves a dotted servers key ("mcpServers", "mcp.servers")
// against root, returning the servers map. When create is set, missing
// intermediate objects are added so the caller can write into the map;
// otherwise an absent or wrong-typed path yields nil.
func jsonTable(root map[string]any, serversKey string, create bool) map[string]any {
	current := root
	parts := strings.Split(serversKey, ".")
	for i, part := range parts {
		last := i == len(parts)-1
		child, ok := current[part].(map[string]any)
		if !ok || child == nil {
			if !create {
				return nil
			}
			child = map[string]any{}
			current[part] = child
		}
		if last {
			return child
		}
		current = child
	}
	return nil
}

// JSONServers returns the servers map at the dotted serversKey, treating an
// absent or wrong-typed entry as empty.
func JSONServers(root map[string]any, serversKey string) map[string]any {
	if servers := jsonTable(root, serversKey, false); servers != nil {
		return servers
	}
	return map[string]any{}
}

// JSONEntry renders one server into the object stored under the entry name.
// Remote entries take the documented remote key set (url, headers, plus the
// transport type key only when the target has one — targets that auto-detect
// the transport pass typeKey false and must not gain an undocumented key);
// stdio entries keep the historical command/args/env shape with no type key,
// so pre-existing entries render byte-identically.
func JSONEntry(srv Server, typeKey bool) map[string]any {
	entry := map[string]any{}
	if srv.URL != "" {
		if typeKey {
			transport := srv.Transport
			if transport == "" {
				transport = "http"
			}
			entry["type"] = transport
		}
		entry["url"] = srv.URL
		if len(srv.Headers) > 0 {
			entry["headers"] = srv.Headers
		}
		return entry
	}
	entry["command"] = srv.Command
	if len(srv.Args) > 0 {
		entry["args"] = srv.Args
	}
	if len(srv.Env) > 0 {
		entry["env"] = srv.Env
	}
	return entry
}

// SetJSONServer upserts one server entry, preserving every other key. typeKey
// comes from the target's RemoteRender capability.
func SetJSONServer(root map[string]any, serversKey, name string, srv Server, typeKey bool) {
	servers := jsonTable(root, serversKey, true)
	servers[name] = JSONEntry(srv, typeKey)
}

// DeleteJSONServer removes one server entry, reporting whether it was present.
func DeleteJSONServer(root map[string]any, serversKey, name string) bool {
	servers := jsonTable(root, serversKey, false)
	if servers == nil {
		return false
	}
	if _, ok := servers[name]; !ok {
		return false
	}
	delete(servers, name)
	return true
}

// JSONServer reads one server entry back into a Server, for drift checks.
func JSONServer(root map[string]any, serversKey, name string) (Server, bool) {
	raw, ok := JSONServers(root, serversKey)[name].(map[string]any)
	if !ok {
		return Server{}, false
	}
	return serverFromMap(raw), true
}

// serverFromMap converts a decoded JSON/TOML server object into the canonical
// subset, ignoring keys senv does not manage.
func serverFromMap(raw map[string]any) Server {
	srv := Server{Env: map[string]string{}}
	if command, ok := raw["command"].(string); ok {
		srv.Command = command
	}
	switch args := raw["args"].(type) {
	case []any:
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				srv.Args = append(srv.Args, s)
			}
		}
	case []string:
		srv.Args = append(srv.Args, args...)
	}
	switch env := raw["env"].(type) {
	case map[string]any:
		for key, value := range env {
			if s, ok := value.(string); ok {
				srv.Env[key] = s
			}
		}
	case map[string]string:
		for key, value := range env {
			srv.Env[key] = value
		}
	}
	if len(srv.Env) == 0 {
		srv.Env = nil
	}
	if transport, ok := raw["type"].(string); ok {
		srv.Transport = transport
	}
	if url, ok := raw["url"].(string); ok {
		srv.URL = url
	}
	switch headers := raw["headers"].(type) {
	case map[string]any:
		srv.Headers = map[string]string{}
		for key, value := range headers {
			if s, ok := value.(string); ok {
				srv.Headers[key] = s
			}
		}
	case map[string]string:
		srv.Headers = map[string]string{}
		for key, value := range headers {
			srv.Headers[key] = value
		}
	}
	if len(srv.Headers) == 0 {
		srv.Headers = nil
	}
	// TOML configs spell the transport key "transport"; JSON configs spell it
	// "type". Accept both so drift checks read back either family.
	if srv.Transport == "" {
		if transport, ok := raw["transport"].(string); ok {
			srv.Transport = transport
		}
	}
	return srv
}

// EncodeJSON renders a config root the way senv writes JSON configs.
func EncodeJSON(root map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ---------------------------------------------------------------------------
// TOML configs
// ---------------------------------------------------------------------------

// RenderTOMLServerBlock renders the TOML block for one server, matching Codex's
// [mcp_servers.<name>] convention. stdio entries keep the historical
// command/args/env shape; remote entries render the documented url/transport
// keys (transport only for targets with a transport type key). Custom headers
// are not rendered: only targets whose RemoteRender accepts headers get them,
// and those are JSON targets today.
func RenderTOMLServerBlock(table, name string, srv Server, typeKey bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s.%s]\n", table, name)
	if srv.URL != "" {
		if typeKey && srv.Transport != "" && srv.Transport != "stdio" {
			fmt.Fprintf(&b, "transport = %q\n", srv.Transport)
		}
		fmt.Fprintf(&b, "url = %q\n", srv.URL)
		return b.String()
	}
	fmt.Fprintf(&b, "command = %q\n", srv.Command)
	if len(srv.Args) > 0 {
		fmt.Fprintf(&b, "args = [\"%s\"]\n", strings.Join(srv.Args, "\", \""))
	}
	if len(srv.Env) > 0 {
		fmt.Fprintf(&b, "[%s.%s.env]\n", table, name)
		keys := make([]string, 0, len(srv.Env))
		for key := range srv.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "%s = %q\n", key, srv.Env[key])
		}
	}
	return b.String()
}

// UpsertTOMLServer replaces the [<table>.<name>] table (and its nested
// subtables) in src with newBlock, or appends newBlock if absent. Other tables
// and free-form content are preserved verbatim. This is deliberately simple:
// it splits on top-level table headers ([...] at column 0) and treats any
// [<table>.<name>...] header as part of the block.
func UpsertTOMLServer(src, table, name, newBlock string) (string, error) {
	tableHeader := fmt.Sprintf("[%s.%s]", table, name)
	subtablePrefix := fmt.Sprintf("[%s.%s.", table, name)
	out, replaced := replaceTOMLBlock(src, tableHeader, subtablePrefix, &newBlock)
	result := strings.Join(out, "\n")
	if !replaced {
		if result != "" && !strings.HasSuffix(result, "\n\n") {
			if strings.HasSuffix(result, "\n") {
				result += "\n"
			} else {
				result += "\n\n"
			}
		}
		result += newBlock
	}
	return strings.TrimRight(result, "\n") + "\n", nil
}

// RemoveTOMLServer drops the [<table>.<name>] table and its subtables,
// reporting whether anything was removed. The rest of the file is untouched.
func RemoveTOMLServer(src, table, name string) (string, bool) {
	tableHeader := fmt.Sprintf("[%s.%s]", table, name)
	subtablePrefix := fmt.Sprintf("[%s.%s.", table, name)
	out, removed := replaceTOMLBlock(src, tableHeader, subtablePrefix, nil)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", removed
}

// replaceTOMLBlock drops every line of the matching table (and subtables) and
// splices replacement in its place when one is given.
func replaceTOMLBlock(src, tableHeader, subtablePrefix string, replacement *string) ([]string, bool) {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false
	replaced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[")
		if isHeader {
			if trimmed == tableHeader || strings.HasPrefix(trimmed, subtablePrefix) {
				inBlock = true
				if !replaced && replacement != nil {
					out = append(out, strings.TrimRight(*replacement, "\n"))
					replaced = true
				}
				replaced = true
				continue
			}
			inBlock = false
		}
		if inBlock {
			continue
		}
		out = append(out, line)
	}
	return out, replaced
}

// TOMLServer parses one [<table>.<name>] entry back into a Server, for drift
// checks. A missing or malformed entry reports false.
func TOMLServer(src, table, name string) (Server, bool) {
	servers, err := TOMLServers(src, table)
	if err != nil {
		return Server{}, false
	}
	raw, ok := servers[name].(map[string]any)
	if !ok {
		return Server{}, false
	}
	return serverFromMap(raw), true
}

// TOMLServers parses every [<table>.<name>] entry of a TOML config, keyed by
// name, for the bulk import path. A malformed file reports an error; a file
// without the table yields an empty map.
func TOMLServers(src, table string) (map[string]any, error) {
	var root map[string]any
	if err := toml.Unmarshal([]byte(src), &root); err != nil {
		return nil, fmt.Errorf("parse TOML: %w", err)
	}
	tables, ok := root[table].(map[string]any)
	if !ok || tables == nil {
		return map[string]any{}, nil
	}
	return tables, nil
}

// ---------------------------------------------------------------------------
// Shared file I/O
// ---------------------------------------------------------------------------

// WriteWithBackup writes data to path, creating parent dirs as needed and
// backing up any existing file to path+BackupSuffix first.
func WriteWithBackup(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+BackupSuffix, existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
