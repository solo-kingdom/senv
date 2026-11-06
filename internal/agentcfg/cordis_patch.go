package agentcfg

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeepSeek Harness (DSH) reads MCP servers as cordis loader patch rows: a YAML
// list whose entries are either id-targeted overrides of rows already mounted
// by a bundle, or `insert` rows adding new entries. An id-targeted row for an
// id that no bundle mounted is warned about and skipped, so an MCP server MUST
// be inserted — a bare id/name/config row is silently dropped.
//
// senv writes one marked block into DSH's machine-local home patch,
// $DSH_HOME/cordis.patch.yml. DSH composes that layer after every profile's
// bundle layers, and dsh-hmr watches it (including before the file exists), so
// one user-scope export reaches the Desktop, web, headless and ACP surfaces
// and hot-reloads without a restart.
//
// Everything outside the marked block is preserved byte for byte. DSH parses
// patch files with js-yaml JSON_SCHEMA plus a `!!js` scalar type and entry
// order is semantic (a later patch for the same id replaces the whole config),
// so a whole-file parse/re-encode would lose comments, scalar styles and
// potentially change meaning.
//
// Verified against DSH Desktop 0.2.2-rc.1:
//   - home patch path: dsh-home-paths resolveDshHome ($DSH_HOME, default
//     ~/.dsh) + app-boot PROFILE_PATCH_FILENAME
//   - layer order and home patch: app-boot readProfilePatches
//   - insert semantics: app-boot applyPatches
//   - plugin config: @deepseek-ai/dsh-mcp-client Config (stdio |
//     streamable-http, serverName [A-Za-z0-9_-]{1,32})
//   - hot reload: dsh-hmr watches the profile and home patch files
const (
	// DSHMCPPlugin is the plugin package an MCP row inserts. It ships with the
	// DSH installation (a direct dependency of @deepseek-ai/dsh), so patch rows
	// resolve it without a profile-local install.
	DSHMCPPlugin = "@deepseek-ai/dsh-mcp-client"

	// cordisBlockStart / cordisBlockEnd bracket the only part of the file senv
	// reads or rewrites. They are ordinary YAML comments, so a user who copies
	// the block elsewhere loses nothing but senv's ability to manage it.
	cordisBlockStart = "# >>> senv-managed MCP servers (rewritten by senv mcp export) >>>"
	cordisBlockEnd   = "# <<< senv-managed MCP servers <<<"

	// dshPatchFileName is the patch-file name at the Harness home root.
	dshPatchFileName = "cordis.patch.yml"

	// dshEntryIDPrefix marks insert rows senv owns. DSH assigns a random id to
	// an inserted entry without one, which would make write-back unstable, so
	// senv always writes a deterministic id.
	dshEntryIDPrefix = "senv-mcp-"
)

var (
	// dshServerNamePattern is the mcp-client serverName contract; the name is
	// the tool namespace (mcp__<serverName>__<tool>).
	dshServerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

	// cordisEmptyListLine matches an explicit empty flow sequence — the way DSH
	// documents an empty-but-valid patch layer ("[]" instead of an empty file:
	// an empty or comments-only file fails boot).
	cordisEmptyListLine = regexp.MustCompile(`^\[\s*\]\s*(#.*)?$`)
)

// DshHomeDir resolves DSH's data root the way @deepseek-ai/dsh-home-paths does:
// an explicit $DSH_HOME wins when non-blank, otherwise <home>/.dsh. Blank
// values are ignored (a blank override must never resolve to the working
// directory). DSH_PROFILE is deliberately NOT consulted: DSH does not read it,
// and the profile name is irrelevant to the home patch.
func DshHomeDir(home string) string {
	if raw := strings.TrimSpace(os.Getenv("DSH_HOME")); raw != "" {
		return expandDshHome(raw, home)
	}
	return filepath.Join(home, ".dsh")
}

// cordisPatchPath is the DSH home-level patch layer path.
func cordisPatchPath(home string) string {
	return filepath.Join(DshHomeDir(home), dshPatchFileName)
}

// expandDshHome mirrors DSH's expandHomePath: only bare and current-user "~"
// forms are expanded, then the value is normalized to an absolute path.
func expandDshHome(raw, home string) string {
	switch {
	case raw == "~":
		return home
	case strings.HasPrefix(raw, "~/"), strings.HasPrefix(raw, `~\`):
		return filepath.Join(home, raw[2:])
	}
	if abs, err := filepath.Abs(raw); err == nil {
		return abs
	}
	return filepath.Clean(raw)
}

// DshServerName maps a senv MCP alias onto DSH's serverName contract
// ([A-Za-z0-9_-]{1,32}). A conforming alias is used verbatim so the
// model-facing tool namespace stays readable; anything else is normalized and
// gets a short alias-hash suffix, so distinct aliases can never collide into
// one namespace.
func DshServerName(alias string) string {
	if dshServerNamePattern.MatchString(alias) {
		return alias
	}
	var b strings.Builder
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	// 32 bytes minus the 7-byte "-xxxxxx" suffix; the id prefix is not part of
	// the serverName budget.
	const maxBase = 32 - 7
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	if base == "" {
		base = "senv"
	}
	sum := sha256.Sum256([]byte(alias))
	return fmt.Sprintf("%s-%x", base, sum[:3])
}

// DshEntryID returns the loader id of the insert row senv owns for an alias.
func DshEntryID(alias string) string {
	return dshEntryIDPrefix + DshServerName(alias)
}

// cordisEntry is one senv-managed row as stored: its model-facing serverName
// plus the resolved profile definition.
type cordisEntry struct {
	serverName string
	server     Server
}

// CordisPatchFile is a DSH cordis patch file held as text plus the senv-managed
// block parsed out of it. Only the marked block is read or rewritten; all other
// bytes are preserved as-is. A missing file reads as an empty patch layer.
type CordisPatchFile struct {
	src     string
	entries map[string]cordisEntry // keyed by loader entry id
	// aliases records which source alias claimed a loader id during this
	// session, so two aliases that normalize onto the same id fail closed
	// instead of silently overwriting each other's serverName.
	aliases map[string]string
	// idClash reports the id two different aliases claimed, if any.
	idClash string
	// blockFrom/blockTo are the line indexes of the start/end marker lines.
	blockFrom int
	blockTo   int
	block     bool
}

// ReadCordisPatch loads a DSH home patch. A missing file is a valid empty
// layer; a file whose patch list is malformed, or whose senv block is incomplete
// or contains entries senv does not own, reports an error so the exporter can
// fail closed instead of clobbering hand-written content.
func ReadCordisPatch(path string) (*CordisPatchFile, error) {
	file := &CordisPatchFile{entries: map[string]cordisEntry{}, aliases: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	file.src = string(data)
	if err := file.parse(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// parse validates the patch list and extracts the senv-managed block.
func (f *CordisPatchFile) parse() error {
	lines := strings.Split(f.src, "\n")
	start, end := -1, -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case cordisBlockStart:
			if start >= 0 {
				return fmt.Errorf("found more than one %q marker", cordisBlockStart)
			}
			start = i
		case cordisBlockEnd:
			if end >= 0 {
				return fmt.Errorf("found more than one %q marker", cordisBlockEnd)
			}
			end = i
		}
	}
	if start < 0 && end < 0 {
		rows, err := validateCordisPatchList(f.src)
		if err != nil {
			return err
		}
		return rejectStrayCordisEntries(rows, f.entries)
	}
	if start < 0 || end < 0 || end < start {
		return fmt.Errorf("senv-managed block is incomplete; delete the marker comment or repair the block by hand")
	}
	f.block, f.blockFrom, f.blockTo = true, start, end

	blockRows, err := validateCordisPatchList(strings.Join(lines[start+1:end], "\n"))
	if err != nil {
		return fmt.Errorf("senv-managed block: %w", err)
	}
	for _, row := range blockRows {
		inserts, ok := row["insert"].([]any)
		if !ok || len(inserts) == 0 {
			return fmt.Errorf("senv-managed block contains a row without an insert list; senv only manages mcp-client insert rows")
		}
		for _, raw := range inserts {
			entry, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("senv-managed block contains a non-mapping insert entry")
			}
			id, _ := entry["id"].(string)
			name, _ := entry["name"].(string)
			if !strings.HasPrefix(id, dshEntryIDPrefix) {
				return fmt.Errorf("senv-managed block contains entry %q without senv's id prefix; move it outside the block", id)
			}
			if name != DSHMCPPlugin {
				return fmt.Errorf("senv-managed block contains entry %q for %q; senv only manages %q entries", id, name, DSHMCPPlugin)
			}
			cfg, _ := entry["config"].(map[string]any)
			serverName, _ := cfg["serverName"].(string)
			if _, dup := f.entries[id]; dup {
				return fmt.Errorf("senv-managed block repeats entry id %q", id)
			}
			f.entries[id] = cordisEntry{serverName: serverName, server: cordisServerFromMap(cfg)}
		}
	}
	// Entries with senv's id prefix outside the marked block would collide with
	// the block senv rewrites (or are strays senv does not own); reject them
	// regardless of whether a block already exists.
	rows, err := validateCordisPatchList(f.src)
	if err != nil {
		return err
	}
	return rejectStrayCordisEntries(rows, f.entries)
}

// rejectStrayCordisEntries rejects senv-id-prefixed entries that the managed
// block does not contain, so a write can never silently duplicate or adopt
// them.
func rejectStrayCordisEntries(rows []map[string]any, managed map[string]cordisEntry) error {
	strays := map[string]bool{}
	for _, id := range cordisEntryIDs(rows) {
		if strings.HasPrefix(id, dshEntryIDPrefix) {
			strays[id] = true
		}
	}
	for id := range managed {
		delete(strays, id)
	}
	// An id-targeted row (no insert) outside the block is worse than a stray
	// insert: once the block's insert has created the entry, DSH applies such a
	// row as an override, so a hand-written `- id: senv-mcp-*` row could change
	// a senv-managed server without senv seeing drift. These are never the
	// block's own rows, so they are not filtered by the managed set.
	for _, row := range rows {
		if _, isInsert := row["insert"].([]any); isInsert {
			continue
		}
		id, _ := row["id"].(string)
		if strings.HasPrefix(id, dshEntryIDPrefix) {
			strays[id] = true
		}
	}
	if len(strays) == 0 {
		return nil
	}
	ids := make([]string, 0, len(strays))
	for id := range strays {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return fmt.Errorf("entries %v use senv's id prefix outside the senv-managed block (as inserts or id-targeted patch rows); move them into the block or remove them", ids)
}

// Server reads one alias back in the target's comparable form, for drift
// checks.
func (f *CordisPatchFile) Server(alias string) (Server, bool) {
	entry, ok := f.entries[DshEntryID(alias)]
	if !ok {
		return Server{}, false
	}
	return entry.server, true
}

// Set upserts one alias's managed row. Two different aliases that normalize
// onto the same loader id fail closed at Encode time: silently keeping one row
// would drop the other alias without a trace.
func (f *CordisPatchFile) Set(alias string, srv Server) {
	id := DshEntryID(alias)
	if f.aliases == nil {
		f.aliases = map[string]string{}
	}
	if previous, seen := f.aliases[id]; seen && previous != alias && f.idClash == "" {
		f.idClash = fmt.Sprintf("aliases %q and %q both map to DSH entry id %q", previous, alias, id)
	}
	f.aliases[id] = alias
	f.entries[id] = cordisEntry{serverName: DshServerName(alias), server: srv}
}

// Delete removes one alias's managed row, reporting whether it was present.
func (f *CordisPatchFile) Delete(alias string) bool {
	id := DshEntryID(alias)
	if _, ok := f.entries[id]; !ok {
		return false
	}
	delete(f.entries, id)
	delete(f.aliases, id)
	if f.idClash != "" {
		f.idClash = ""
	}
	return true
}

// Encode renders the file with the managed block updated. Unchanged bytes
// outside the block are preserved. Every render is validated as a DSH patch
// list before it is handed to the writer.
func (f *CordisPatchFile) Encode() ([]byte, error) {
	if f.idClash != "" {
		return nil, fmt.Errorf("refusing to write DSH patch: %s", f.idClash)
	}
	block, err := f.renderBlock()
	if err != nil {
		return nil, err
	}
	if !f.block && len(block) == 0 {
		return []byte(f.src), nil
	}
	lines := strings.Split(f.src, "\n")
	if f.block {
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:f.blockFrom]...)
		out = append(out, block...)
		out = append(out, lines[f.blockTo+1:]...)
		text := strings.Join(out, "\n")
		if len(block) == 0 {
			text = ensureEmptyCordisList(text)
		}
		return f.finish(text)
	}
	return f.finish(insertCordisBlock(f.src, block))
}

// renderBlock renders the marked block lines. No entries means no block.
func (f *CordisPatchFile) renderBlock() ([]string, error) {
	if len(f.entries) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(f.entries))
	for id := range f.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := []cordisPatchRowYAML{{Insert: make([]cordisPatchEntryYAML, 0, len(ids))}}
	for _, id := range ids {
		entry := f.entries[id]
		rows[0].Insert = append(rows[0].Insert, cordisPatchEntryYAML{
			ID:     id,
			Name:   DSHMCPPlugin,
			Config: cordisConfigFor(entry.serverName, entry.server),
		})
	}
	data, err := yaml.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("render dsh patch block: %w", err)
	}
	body := strings.TrimRight(string(data), "\n")
	lines := append([]string{cordisBlockStart}, strings.Split(body, "\n")...)
	return append(lines, cordisBlockEnd), nil
}

// finish validates the composed file and rejects ambiguous duplicates.
func (f *CordisPatchFile) finish(text string) ([]byte, error) {
	rows, err := validateCordisPatchList(text)
	if err != nil {
		return nil, fmt.Errorf("refusing to write DSH patch: %w", err)
	}
	if err := checkCordisConflicts(rows, f.entries); err != nil {
		return nil, fmt.Errorf("refusing to write DSH patch: %w", err)
	}
	return []byte(text), nil
}

// RenderCordisPatchRow renders the marked block senv would write for one alias,
// for `--print`.
func RenderCordisPatchRow(alias string, srv Server) (string, error) {
	file := &CordisPatchFile{entries: map[string]cordisEntry{}, aliases: map[string]string{}}
	file.Set(alias, srv)
	block, err := file.renderBlock()
	if err != nil {
		return "", err
	}
	return strings.Join(block, "\n") + "\n", nil
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// cordisPatchRowYAML is one top-level patch row. `insert` is the only row
// shape that can add a new entry (app-boot applyPatches).
type cordisPatchRowYAML struct {
	Insert []cordisPatchEntryYAML `yaml:"insert"`
}

// cordisPatchEntryYAML is one inserted loader entry. The id is what senv uses
// to find and rewrite its own rows later.
type cordisPatchEntryYAML struct {
	ID     string           `yaml:"id"`
	Name   string           `yaml:"name"`
	Config cordisConfigYAML `yaml:"config"`
}

// cordisConfigYAML is the @deepseek-ai/dsh-mcp-client Config subset senv
// manages. Transport is required by the plugin schema; other plugin knobs
// (cwd, timeouts, reconnect) are deliberately not written so DSH defaults
// apply.
type cordisConfigYAML struct {
	ServerName string                  `yaml:"serverName"`
	Transport  string                  `yaml:"transport"`
	Command    cordisScalar            `yaml:"command,omitempty"`
	Args       []cordisScalar          `yaml:"args,omitempty"`
	Env        map[string]cordisScalar `yaml:"env,omitempty"`
	URL        cordisScalar            `yaml:"url,omitempty"`
	Headers    map[string]cordisScalar `yaml:"headers,omitempty"`
}

// cordisScalar renders a string scalar. yaml.v3 emits multi-line strings as
// block scalars, which drops a leading newline on read-back (yaml.v3 and
// js-yaml agree); forcing a double-quoted scalar for values starting with a
// line break keeps them byte-exact.
type cordisScalar string

func (v cordisScalar) MarshalYAML() (any, error) {
	// !!str is load-bearing: a bare scalar node would let "123"/"true" render
	// unquoted and read back as a number/bool, silently dropping the value.
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(v)}
	if str := string(v); str != "" && (str[0] == '\n' || str[0] == '\r') {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node, nil
}

func cordisScalarMap(values map[string]string) map[string]cordisScalar {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]cordisScalar, len(values))
	for key, value := range values {
		out[key] = cordisScalar(value)
	}
	return out
}

func cordisScalarSlice(values []string) []cordisScalar {
	if len(values) == 0 {
		return nil
	}
	out := make([]cordisScalar, len(values))
	for i, value := range values {
		out[i] = cordisScalar(value)
	}
	return out
}

func cordisConfigFor(serverName string, srv Server) cordisConfigYAML {
	cfg := cordisConfigYAML{ServerName: serverName, Transport: "stdio"}
	if srv.URL != "" {
		// The plugin has exactly two transport values; sse is rejected before
		// rendering, so every remote entry is streamable HTTP.
		cfg.Transport = "streamable-http"
		cfg.URL = cordisScalar(srv.URL)
		cfg.Headers = cordisScalarMap(srv.Headers)
		return cfg
	}
	cfg.Command = cordisScalar(srv.Command)
	cfg.Args = cordisScalarSlice(srv.Args)
	cfg.Env = cordisScalarMap(srv.Env)
	return cfg
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// validateCordisPatchList decodes src as the top-level YAML array of patch
// mappings DSH requires (app-boot parsePatchList). A comments-only file decodes
// to an empty list; DSH would reject it at boot, but senv heals it by writing
// entries into it.
func validateCordisPatchList(src string) ([]map[string]any, error) {
	var rows []map[string]any
	if err := yaml.Unmarshal([]byte(src), &rows); err != nil {
		return nil, fmt.Errorf("must be a top-level YAML array of loader patch entries: %w", err)
	}
	return rows, nil
}

// cordisEntryIDs lists the ids of every inserted entry, for the ownership check
// outside the managed block.
func cordisEntryIDs(rows []map[string]any) []string {
	var ids []string
	for _, row := range rows {
		inserts, ok := row["insert"].([]any)
		if !ok {
			continue
		}
		for _, raw := range inserts {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := entry["id"].(string)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// cordisInvalidTransport marks a stored transport senv would never write:
// missing, or a spelling the mcp-client schema rejects. Keeping it distinct
// from both canonical values makes a hand-edited-broken managed entry read as
// drift, so --force can repair it instead of the export silently skipping a
// config DSH cannot load.
const cordisInvalidTransport = "\x00invalid"

// cordisServerFromMap converts a decoded mcp-client config into the canonical
// cross-agent Server subset. `!!js` expressions arrive as their literal text;
// that is a local edit from senv's point of view and therefore drifts.
func cordisServerFromMap(cfg map[string]any) Server {
	srv := Server{}
	switch transport, _ := cfg["transport"].(string); transport {
	case "streamable-http":
		srv.Transport = "http"
	case "stdio":
		srv.Transport = "stdio"
	default:
		srv.Transport = cordisInvalidTransport
	}
	srv.Command, _ = cfg["command"].(string)
	for _, arg := range stringSliceValue(cfg["args"]) {
		srv.Args = append(srv.Args, arg)
	}
	srv.Env = stringMapValue(cfg["env"])
	if url, _ := cfg["url"].(string); url != "" {
		srv.URL = url
	}
	srv.Headers = stringMapValue(cfg["headers"])
	return srv
}

func stringSliceValue(raw any) []string {
	switch values := raw.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), values...)
	}
	return nil
}

func stringMapValue(raw any) map[string]string {
	switch values := raw.(type) {
	case map[string]any:
		if len(values) == 0 {
			return nil
		}
		out := make(map[string]string, len(values))
		for key, value := range values {
			if s, ok := value.(string); ok {
				out[key] = s
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]string:
		if len(values) == 0 {
			return nil
		}
		return values
	}
	return nil
}

// ---------------------------------------------------------------------------
// File composition
// ---------------------------------------------------------------------------

// insertCordisBlock appends the managed block to src. An explicit empty flow
// sequence that makes up the whole layer ("[]") is dropped first: keeping it
// would make the appended rows a second YAML node. Comments around it are
// preserved, and the file keeps a single trailing newline.
//
// The empty list is located through the parsed document root, never by text
// matching: a line that happens to read "[]" inside a block scalar or a nested
// flow sequence is user content and MUST survive.
func insertCordisBlock(src string, block []string) string {
	lines := strings.Split(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if idx := emptyCordisListLine(src); idx >= 0 && idx < len(lines) && cordisEmptyListLine.MatchString(strings.TrimSpace(lines[idx])) {
		lines = append(lines[:idx], lines[idx+1:]...)
	}
	lines = append(lines, block...)
	return strings.Join(lines, "\n") + "\n"
}

// emptyCordisListLine returns the 0-based line index of an explicit empty flow
// sequence that is the whole patch layer ("[]" or "[] # note"), or -1. A
// comments-only or empty file has no such node.
func emptyCordisListLine(src string) int {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return -1
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return -1
	}
	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode || len(root.Content) != 0 || root.Style&yaml.FlowStyle == 0 {
		return -1
	}
	if root.Line <= 0 {
		return -1
	}
	return root.Line - 1
}

// ensureEmptyCordisList keeps a patched file bootable after its last managed
// entry is removed: DSH treats an empty or comments-only patch file as a boot
// error, so the layer must either keep other rows or say `[]` explicitly.
func ensureEmptyCordisList(text string) string {
	rows, err := validateCordisPatchList(text)
	if err != nil || len(rows) > 0 {
		return text
	}
	if emptyCordisListLine(text) >= 0 {
		return text
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "[]\n"
}

// checkCordisConflicts rejects writes that would break DSH's entry contract:
// every managed id and serverName must appear exactly once in the composed file.
// A duplicate serverName makes the later mcp-client instance fail to load, so
// fail closed instead of writing it.
func checkCordisConflicts(rows []map[string]any, managed map[string]cordisEntry) error {
	idCount := map[string]int{}
	serverNameCount := map[string]int{}
	byID := map[string]string{}
	for _, row := range rows {
		inserts, ok := row["insert"].([]any)
		if !ok {
			continue
		}
		for _, raw := range inserts {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := entry["id"].(string)
			if id != "" {
				idCount[id]++
			}
			cfg, _ := entry["config"].(map[string]any)
			serverName, _ := cfg["serverName"].(string)
			if serverName != "" {
				serverNameCount[serverName]++
				if byID[id] == "" {
					byID[id] = serverName
				}
			}
		}
	}
	for id, entry := range managed {
		if idCount[id] != 1 {
			return fmt.Errorf("entry id %q appears %d times", id, idCount[id])
		}
		serverName := byID[id]
		if serverName == "" {
			serverName = entry.serverName
		}
		if !dshServerNamePattern.MatchString(serverName) {
			return fmt.Errorf("serverName %q for %q does not match [A-Za-z0-9_-]{1,32}", serverName, id)
		}
		if serverNameCount[serverName] != 1 {
			return fmt.Errorf("serverName %q is already used by another entry; DSH fails the later mcp-client instance", serverName)
		}
	}
	return nil
}

// WriteCordisPatch writes a composed patch through the shared backup writer,
// creating the DSH home with 0700 first. DSH itself creates ~/.dsh as a private
// directory; the generic writer's 0755 would leave a Harness home that senv
// created ahead of DSH open to traversal by other local users.
func WriteCordisPatch(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create DSH home: %w", err)
		}
	}
	return WriteWithBackup(path, data)
}
