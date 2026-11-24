package agentcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/securefs"
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
	existing, _, err := exportfile.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
	return JSONServerFor(root, serversKey, name, "headers")
}

// JSONServerFor is JSONServer with an explicit header key, so a target that
// spells custom headers differently reads back the same Server it wrote.
func JSONServerFor(root map[string]any, serversKey, name, headersKey string) (Server, bool) {
	raw, ok := JSONServers(root, serversKey)[name].(map[string]any)
	if !ok {
		return Server{}, false
	}
	return serverFromMap(raw, "", headersKey), true
}

// serverFromMap converts a decoded JSON/TOML server object into the canonical
// subset, ignoring keys senv does not manage. transportKey/headersKey are the
// target's native spellings ("type"/"transport", "headers"/"http_headers");
// the cross-agent defaults are always accepted too so a profile read back from
// any target compares equal to the one senv would write there.
func serverFromMap(raw map[string]any, transportKey, headersKey string) Server {
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
	for _, key := range dedupeKeys(headersKey, "headers") {
		switch headers := raw[key].(type) {
		case map[string]any:
			srv.Headers = map[string]string{}
			for headerKey, value := range headers {
				if s, ok := value.(string); ok {
					srv.Headers[headerKey] = s
				}
			}
		case map[string]string:
			srv.Headers = map[string]string{}
			for headerKey, value := range headers {
				srv.Headers[headerKey] = value
			}
		}
		if len(srv.Headers) > 0 {
			break
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

// dedupeKeys returns the given keys, dropping empties and later duplicates so
// callers can try a target's native spelling and the shared default in turn.
func dedupeKeys(keys ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
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

// formatTOMLString encodes s as a TOML v1.0 basic string, escaping all control
// characters so untrusted args/headers/env values cannot break out of a string
// literal or inject new TOML keys.
func formatTOMLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// formatTOMLKey emits a bare key when safe ([A-Za-z0-9_-]+) and quotes
// everything else as a TOML basic string.
func formatTOMLKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return formatTOMLString(k)
		}
	}
	return k
}

// RenderTOMLServerBlock renders the TOML block for one server, matching Codex's
// [mcp_servers.<name>] convention. stdio entries keep the historical
// command/args/env shape; remote entries render the documented url/transport
// keys (transport only for targets with a transport type key). Custom headers
// render as an inline table under the target's header key, in sorted order so
// repeated renders of the same profile stay byte-identical.
func RenderTOMLServerBlock(table, name string, srv Server, typeKey bool, headersKey string) string {
	var b strings.Builder
	tKey := formatTOMLKey(table)
	nKey := formatTOMLKey(name)
	fmt.Fprintf(&b, "[%s.%s]\n", tKey, nKey)
	if srv.URL != "" {
		if typeKey && srv.Transport != "" && srv.Transport != "stdio" {
			fmt.Fprintf(&b, "transport = %s\n", formatTOMLString(srv.Transport))
		}
		fmt.Fprintf(&b, "url = %s\n", formatTOMLString(srv.URL))
		if headersKey != "" && len(srv.Headers) > 0 {
			keys := make([]string, 0, len(srv.Headers))
			for key := range srv.Headers {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			pairs := make([]string, 0, len(keys))
			for _, key := range keys {
				pairs = append(pairs, fmt.Sprintf("%s = %s", formatTOMLKey(key), formatTOMLString(srv.Headers[key])))
			}
			fmt.Fprintf(&b, "%s = { %s }\n", formatTOMLKey(headersKey), strings.Join(pairs, ", "))
		}
		return b.String()
	}
	fmt.Fprintf(&b, "command = %s\n", formatTOMLString(srv.Command))
	if len(srv.Args) > 0 {
		args := make([]string, len(srv.Args))
		for i, arg := range srv.Args {
			args[i] = formatTOMLString(arg)
		}
		fmt.Fprintf(&b, "args = [%s]\n", strings.Join(args, ", "))
	}
	if len(srv.Env) > 0 {
		fmt.Fprintf(&b, "[%s.%s.env]\n", tKey, nKey)
		keys := make([]string, 0, len(srv.Env))
		for key := range srv.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "%s = %s\n", formatTOMLKey(key), formatTOMLString(srv.Env[key]))
		}
	}
	return b.String()
}

func tomlBlockMatchers(table, name string) (headers, subPrefixes []string) {
	tKey := formatTOMLKey(table)
	nKey := formatTOMLKey(name)
	headers = []string{fmt.Sprintf("[%s.%s]", tKey, nKey)}
	subPrefixes = []string{fmt.Sprintf("[%s.%s.", tKey, nKey)}
	rawHeader := fmt.Sprintf("[%s.%s]", table, name)
	if rawHeader != headers[0] {
		headers = append(headers, rawHeader)
		subPrefixes = append(subPrefixes, fmt.Sprintf("[%s.%s.", table, name))
	}
	return headers, subPrefixes
}

// UpsertTOMLServer replaces the [<table>.<name>] table (and its nested
// subtables) in src with newBlock, or appends newBlock if absent. Other tables
// and free-form content are preserved verbatim. This is deliberately simple:
// it splits on top-level table headers ([...] at column 0) and treats any
// [<table>.<name>...] header as part of the block.
func UpsertTOMLServer(src, table, name, newBlock string) (string, error) {
	var check map[string]any
	if err := toml.Unmarshal([]byte(newBlock), &check); err != nil {
		return "", fmt.Errorf("invalid rendered TOML block for %s.%s: %w", table, name, err)
	}
	headers, subPrefixes := tomlBlockMatchers(table, name)
	out, replaced := replaceTOMLBlock(src, headers, subPrefixes, &newBlock)
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
	headers, subPrefixes := tomlBlockMatchers(table, name)
	out, removed := replaceTOMLBlock(src, headers, subPrefixes, nil)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", removed
}

// replaceTOMLBlock drops every line of the matching table (and subtables) and
// splices replacement in its place when one is given.
func replaceTOMLBlock(src string, tableHeaders, subtablePrefixes []string, replacement *string) ([]string, bool) {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false
	replaced := false
	matchesHeader := func(trimmed string) bool {
		for _, h := range tableHeaders {
			if trimmed == h {
				return true
			}
		}
		for _, p := range subtablePrefixes {
			if strings.HasPrefix(trimmed, p) {
				return true
			}
		}
		return false
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[")
		if isHeader {
			if matchesHeader(trimmed) {
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
	return TOMLServerFor(src, table, name, "headers")
}

// TOMLServerFor is TOMLServer with an explicit header key (codex spells it
// "http_headers"), so drift checks see the same Server senv would render.
func TOMLServerFor(src, table, name, headersKey string) (Server, bool) {
	servers, err := TOMLServers(src, table)
	if err != nil {
		return Server{}, false
	}
	raw, ok := servers[name].(map[string]any)
	if !ok {
		return Server{}, false
	}
	return serverFromMap(raw, "", headersKey), true
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

// WriteWithBackup writes data to path without following symlinks, creating
// missing parent dirs privately and backing up any existing file to
// path+BackupSuffix (0600).
func WriteWithBackup(path string, data []byte) error {
	return writeAtomicConfig(path, data, 0o755, true)
}

// WriteSensitive atomically writes plaintext-credential configs (0600) without
// following symlinks and removes any temporary or pre-existing .bak copy once
// the new file is in place so resolved secrets never linger in backup files.
func WriteSensitive(path string, data []byte, dirPerm os.FileMode) error {
	return writeAtomicConfig(path, data, dirPerm, false)
}

func writeAtomicConfig(path string, data []byte, dirPerm os.FileMode, keepBackup bool) error {
	resolved, err := exportfile.ResolvePath(path)
	if err != nil {
		return err
	}
	root, err := securefs.OpenRoot(resolved.Root)
	if err != nil {
		return fmt.Errorf("open config root: %w", err)
	}
	defer root.Close()

	if len(resolved.ParentSegments) > 0 {
		if err := root.EnsurePrivateParents(resolved.ParentSegments, dirPerm); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}

	backupSegments := append(append([]string(nil), resolved.ParentSegments...), resolved.Base+BackupSuffix)
	existing, err := root.Read(resolved.Segments...)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing config: %w", err)
	}
	if err == nil && len(existing) > 0 {
		if err := root.AtomicWrite(backupSegments, existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	if err := root.AtomicWrite(resolved.Segments, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if !keepBackup {
		_ = root.Remove(backupSegments...)
	}
	return nil
}
