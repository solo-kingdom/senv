package agentcfg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeCordisPatch(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cordis.patch.yml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newCordisPatch(t *testing.T, src string) *CordisPatchFile {
	t.Helper()
	file, err := ReadCordisPatch(writeCordisPatch(t, src))
	if err != nil {
		t.Fatalf("ReadCordisPatch: %v", err)
	}
	return file
}

func TestDshHomeDir(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"default", "", filepath.Join("/home/u", ".dsh")},
		{"blank is unset", "   ", filepath.Join("/home/u", ".dsh")},
		{"absolute env wins", "/alt/dsh", "/alt/dsh"},
		{"tilde expansion", "~/alt", filepath.Join("/home/u", "alt")},
		{"bare tilde", "~", "/home/u"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DSH_HOME", tc.env)
			if got := DshHomeDir("/home/u"); got != tc.want {
				t.Fatalf("DshHomeDir = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDshTargetPaths(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	target, ok := Find("dsh")
	if !ok {
		t.Fatal("dsh target missing")
	}
	if target.Format != FormatCordisPatch {
		t.Fatalf("Format = %v, want FormatCordisPatch", target.Format)
	}
	// DSH ships its MCP client with the installation: no extension adapter.
	if target.Prerequisite != nil {
		t.Fatalf("dsh must not declare a Prerequisite: %+v", target.Prerequisite)
	}
	if !target.Remote.HTTP || !target.Remote.Headers || !target.Remote.TypeKey {
		t.Fatalf("dsh remote capability = %+v, want http+headers+typekey", target.Remote)
	}
	if target.Remote.SSE {
		t.Fatal("dsh must not accept sse entries: mcp-client transport is stdio | streamable-http")
	}
	if strings.TrimSpace(target.Note) == "" {
		t.Fatal("dsh target must explain how changes take effect")
	}
	// DSH has no project-level patch layer: both scopes resolve to the home
	// patch.
	user := target.ResolveConfigPath("/home/u", "user")
	if want := filepath.Join("/home/u", ".dsh", "cordis.patch.yml"); user != want {
		t.Fatalf("user path = %q, want %q", user, want)
	}
	if project := target.ResolveConfigPath("/home/u", "project"); project != user {
		t.Fatalf("project path = %q, want the user path %q", project, user)
	}
	// $DSH_HOME wins over the home directory.
	t.Setenv("DSH_HOME", "/alt/dsh")
	if got := target.ResolveConfigPath("/home/u", "user"); got != filepath.Join("/alt/dsh", "cordis.patch.yml") {
		t.Fatalf("DSH_HOME path = %q", got)
	}
	// Aliases are lookup-only; the canonical id stays the only display value.
	if alias, ok := Find("deepseek-harness"); !ok || alias.ID != "dsh" {
		t.Fatalf("alias lookup = %+v/%v", alias, ok)
	}
	for _, id := range IDs() {
		if id == "deepseek-harness" {
			t.Fatal("IDs() must not contain the dsh alias")
		}
	}
}

func TestDshServerName(t *testing.T) {
	for _, alias := range []string{"github", "web-1", "A_b"} {
		if got := DshServerName(alias); got != alias {
			t.Fatalf("DshServerName(%q) = %q, want the alias unchanged", alias, got)
		}
	}
	pattern := regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	for _, alias := range []string{
		"my.server", "with space", "有中文", "a/b", "x:y",
		strings.Repeat("a", 40), "!!!",
	} {
		got := DshServerName(alias)
		if !pattern.MatchString(got) {
			t.Fatalf("DshServerName(%q) = %q does not match serverName contract", alias, got)
		}
		if got == alias {
			t.Fatalf("DshServerName(%q) kept an invalid alias", alias)
		}
		if DshServerName(alias) != got {
			t.Fatalf("DshServerName(%q) is not deterministic", alias)
		}
	}
	// Normalized aliases must not collapse into each other or shadow a valid
	// alias with the same readable stem.
	seen := map[string]string{}
	for _, alias := range []string{"my.server", "my server", "my-server", "my_server"} {
		name := DshServerName(alias)
		if other, dup := seen[name]; dup {
			t.Fatalf("aliases %q and %q both map to serverName %q", alias, other, name)
		}
		seen[name] = alias
	}
}

func TestCordisPatchWritesStdioRow(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := filepath.Join(t.TempDir(), "cordis.patch.yml")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read missing file: %v", err)
	}
	desired := Server{
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-github"},
		Env:       map[string]string{"GITHUB_TOKEN": "secret"},
	}
	file.Set("github", desired)
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		cordisBlockStart,
		cordisBlockEnd,
		"- insert:",
		"@deepseek-ai/dsh-mcp-client",
		"serverName: github",
		"transport: stdio",
		"command: npx",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("encoded patch missing %q:\n%s", want, text)
		}
	}
	// The row must be an insert row: a top-level bare id-targeted row is
	// silently dropped by DSH's patch layer.
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- id:") {
			t.Fatalf("encoded patch has a bare id row, which DSH drops:\n%s", text)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	back, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	got, ok := back.Server("github")
	if !ok {
		t.Fatal("github row missing after round trip")
	}
	if got.Fingerprint() != desired.Fingerprint() {
		t.Fatalf("round trip server = %+v, want %+v", got, desired)
	}

	// A second export of the same profile must not change a byte.
	back.Set("github", desired)
	again, err := back.Encode()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if string(again) != text {
		t.Fatalf("re-encode is not idempotent:\nfirst:\n%s\nsecond:\n%s", text, again)
	}
}

func TestCordisPatchWritesRemoteRow(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := filepath.Join(t.TempDir(), "cordis.patch.yml")
	desired := Server{
		Transport: "http",
		URL:       "https://api.example.com/mcp",
		Headers:   map[string]string{"Authorization": "Bearer token"},
	}
	file, _ := ReadCordisPatch(path)
	file.Set("web", desired)
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "transport: streamable-http") {
		t.Fatalf("remote row must use DSH's streamable-http transport:\n%s", text)
	}
	if strings.Contains(text, "command:") {
		t.Fatalf("remote row must not carry a command:\n%s", text)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	got, ok := back.Server("web")
	if !ok {
		t.Fatal("web row missing")
	}
	if got.Transport != "http" || got.URL != desired.URL || got.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("round trip server = %+v", got)
	}
}

func TestCordisPatchPreservesUserContent(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	user := `# my patch layer
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      nativex:
        apiKeyEnv: !!js process.env.NATIVEX_API_KEY
- insert:
    - id: user-mcp
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: user
        transport: stdio
        command: uvx
`
	path := writeCordisPatch(t, user)
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	if !strings.HasPrefix(text, user) {
		t.Fatalf("bytes outside the managed block changed:\n%s", text)
	}
	if strings.Count(text, cordisBlockStart) != 1 || strings.Count(text, cordisBlockEnd) != 1 {
		t.Fatalf("managed block markers are not unique:\n%s", text)
	}
	// The user's own mcp-client entry is not senv's and must survive untouched.
	back, err := ReadCordisPatch(writeCordisPatch(t, text))
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if _, ok := back.Server("user"); ok {
		t.Fatal("senv must not adopt a hand-written entry")
	}
}

func TestCordisPatchReplacesEmptyFlowList(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# Your patch layer\n[]\n")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# Your patch layer\n") {
		t.Fatalf("header comment lost:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if cordisEmptyListLine.MatchString(strings.TrimSpace(line)) {
			t.Fatalf("empty flow list survived next to the managed block:\n%s", text)
		}
	}
	rows, err := validateCordisPatchList(text)
	if err != nil {
		t.Fatalf("composed file is not a DSH patch list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("composed rows = %d, want 1", len(rows))
	}
}

func TestCordisPatchRemovingLastEntryKeepsValidLayer(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# Your patch layer\n[]\n")
	file, _ := ReadCordisPatch(path)
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	first, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	reloaded, err := ReadCordisPatch(writeCordisPatch(t, string(first)))
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if !reloaded.Delete("github") {
		t.Fatal("Delete reported the managed entry as absent")
	}
	data, err := reloaded.Encode()
	if err != nil {
		t.Fatalf("encode after delete: %v", err)
	}
	text := string(data)
	if strings.Contains(text, cordisBlockStart) {
		t.Fatalf("empty managed block left behind:\n%s", text)
	}
	// DSH fails boot on an empty/comments-only patch file: it must keep an
	// explicit empty list.
	if !strings.Contains(text, "[]") {
		t.Fatalf("empty patch layer must be disabled with []:\n%s", text)
	}
	if _, err := validateCordisPatchList(text); err != nil {
		t.Fatalf("composed file is not a DSH patch list: %v", err)
	}
}

func TestCordisPatchRejectsUnsafeBlocks(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "incomplete markers",
			src:  "# header\n" + cordisBlockStart + "\n- insert: []\n",
			want: "incomplete",
		},
		{
			name: "foreign entry inside block",
			src: cordisBlockStart + "\n" +
				"- insert:\n" +
				"    - id: senv-mcp-other\n" +
				"      name: '@modelcontextprotocol/other'\n" +
				"      config:\n" +
				"        serverName: other\n" +
				cordisBlockEnd + "\n",
			want: "senv only manages",
		},
		{
			name: "bare row inside block",
			src: cordisBlockStart + "\n" +
				"- id: senv-mcp-x\n" +
				"  name: '@deepseek-ai/dsh-mcp-client'\n" +
				cordisBlockEnd + "\n",
			want: "row without an insert list",
		},
		{
			name: "senv id outside block",
			src: "- insert:\n" +
				"    - id: senv-mcp-github\n" +
				"      name: '@deepseek-ai/dsh-mcp-client'\n" +
				"      config:\n" +
				"        serverName: github\n" +
				"        transport: stdio\n" +
				"        command: npx\n",
			want: "outside the senv-managed block",
		},
		{
			name: "not a patch list",
			src:  "just-a-scalar\n",
			want: "top-level YAML array",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadCordisPatch(writeCordisPatch(t, tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestCordisPatchRejectsDuplicateServerName(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	user := `- insert:
    - id: user-mcp
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: github
        transport: stdio
        command: uvx
`
	path := writeCordisPatch(t, user)
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	if _, err := file.Encode(); err == nil || !strings.Contains(err.Error(), "already used by another entry") {
		t.Fatalf("error = %v, want a duplicate serverName rejection", err)
	}
}

func TestRenderCordisPatchRow(t *testing.T) {
	row, err := RenderCordisPatchRow("github", Server{
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "server"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{cordisBlockStart, cordisBlockEnd, "serverName: github", "transport: stdio"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row missing %q:\n%s", want, row)
		}
	}
	if _, err := validateCordisPatchList(row); err != nil {
		t.Fatalf("rendered row is not a DSH patch list: %v", err)
	}
}

func TestCordisPatchSpecialCharactersRoundTrip(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := filepath.Join(t.TempDir(), "cordis.patch.yml")
	desired := Server{
		Transport: "stdio",
		Command:   "npx",
		Args: []string{
			"-y", "server with space", "a:b", `quote"inside`, "line\nbreak",
			"-flag", "unicode-中文",
		},
		Env: map[string]string{
			"PLAIN":        "value",
			"WITH_SPACE":   "a b",
			"WITH_COLON":   "a: b",
			"WITH_QUOTE":   `say "hi"`,
			"WITH_NEWLINE": "line1\nline2",
			"YAMLISH":      "true",
			"NUMERIC":      "123",
		},
	}
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Set("tricky", desired)
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	got, ok := back.Server("tricky")
	if !ok {
		t.Fatal("tricky row missing")
	}
	if got.Fingerprint() != desired.Fingerprint() {
		t.Fatalf("special-character round trip changed the entry:\ngot  %+v\nwant %+v\nrendered:\n%s", got, desired, data)
	}
	back.Set("tricky", desired)
	again, err := back.Encode()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if string(again) != string(data) {
		t.Fatalf("special-character entry is not byte-stable on re-encode:\nfirst:\n%s\nsecond:\n%s", data, again)
	}
}

func TestCordisPatchMultipleEntriesShareOneBlock(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# layer\n[]\n")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Set("zeta", Server{Transport: "stdio", Command: "npx"})
	file.Set("alpha", Server{Transport: "stdio", Command: "uvx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Count(string(data), "- insert:") != 1 {
		t.Fatalf("managed entries must share one insert row:\n%s", data)
	}
	// Entries are sorted by id so repeated renders stay byte-identical.
	if strings.Index(string(data), "senv-mcp-alpha") > strings.Index(string(data), "senv-mcp-zeta") {
		t.Fatalf("managed entries are not sorted by id:\n%s", data)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"alpha", "zeta"} {
		if _, ok := back.Server(alias); !ok {
			t.Fatalf("%s row missing after round trip", alias)
		}
	}
	if !back.Delete("alpha") {
		t.Fatal("Delete(alpha) reported absent")
	}
	remaining, err := back.Encode()
	if err != nil {
		t.Fatalf("encode after delete: %v", err)
	}
	if strings.Contains(string(remaining), "senv-mcp-alpha") || !strings.Contains(string(remaining), "senv-mcp-zeta") {
		t.Fatalf("delete removed the wrong managed entry:\n%s", remaining)
	}
}

func TestCordisPatchPreservesBracketedLinesInUserContent(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	// Both a block scalar line and a nested flow sequence render as "[]".
	user := `# user layer
- id: user-mcp
  name: '@deepseek-ai/dsh-mcp-client'
  config:
    serverName: user
    transport: stdio
    command: uvx
    env:
      EMPTY_LIST_EXAMPLE: |
        []
    args: []
`
	path := writeCordisPatch(t, user)
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "      EMPTY_LIST_EXAMPLE: |\n        []\n") {
		t.Fatalf("a \"[]\" line inside a block scalar was dropped (P2 regression):\n%s", text)
	}
	if !strings.Contains(text, "    args: []") {
		t.Fatalf("a nested flow sequence was dropped (P2 regression):\n%s", text)
	}
	if _, err := validateCordisPatchList(text); err != nil {
		t.Fatalf("composed file is not a DSH patch list: %v", err)
	}
}

func TestCordisPatchReplacesOnlyTheRootEmptyList(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# layer\n[]   # disable the layer\n")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# layer\n") {
		t.Fatalf("comment header lost:\n%s", text)
	}
	if strings.Contains(text, "disable the layer") {
		t.Fatalf("root empty-list line (with its trailing comment) must be replaced:\n%s", text)
	}
	rows, err := validateCordisPatchList(text)
	if err != nil || len(rows) != 1 {
		t.Fatalf("composed rows = %d (err %v), want 1:\n%s", len(rows), err, text)
	}
}

func TestCordisPatchRejectsStraySenvIDWithBlock(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# layer\n[]\n")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	stray := strings.TrimRight(string(data), "\n") + `
- insert:
    - id: senv-mcp-stray
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: stray
        transport: stdio
        command: npx
`
	_, err = ReadCordisPatch(writeCordisPatch(t, stray))
	if err == nil || !strings.Contains(err.Error(), "outside the senv-managed block") {
		t.Fatalf("error = %v, want a stray-entry rejection", err)
	}
}

func TestCordisPatchAliasCollisionFailsClosed(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	colliding := DshServerName("my.server")
	file, err := ReadCordisPatch(filepath.Join(t.TempDir(), "cordis.patch.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("my.server", Server{Transport: "stdio", Command: "one"})
	file.Set(colliding, Server{Transport: "stdio", Command: "two"})
	if _, err := file.Encode(); err == nil || !strings.Contains(err.Error(), "both map to DSH entry id") {
		t.Fatalf("error = %v, want an alias-collision rejection", err)
	}
}

func TestCordisPatchLeadingNewlineScalarRoundTrip(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := filepath.Join(t.TempDir(), "cordis.patch.yml")
	desired := Server{
		Transport: "stdio",
		Command:   "npx",
		Env:       map[string]string{"LEAD": "\nline", "ONLY_NEWLINE": "\n"},
	}
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Set("lead", desired)
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	got, ok := back.Server("lead")
	if !ok {
		t.Fatal("lead row missing")
	}
	if got.Fingerprint() != desired.Fingerprint() {
		t.Fatalf("leading-newline values changed:\ngot  %+v\nwant %+v\nrendered:\n%s", got, desired, data)
	}
}

func TestCordisPatchInvalidTransportReadsAsDrift(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	for _, transport := range []string{"", "http", "sse"} {
		line := ""
		if transport != "" {
			line = "        transport: " + transport + "\n"
		}
		src := cordisBlockStart + "\n" +
			"- insert:\n" +
			"    - id: senv-mcp-web\n" +
			"      name: '@deepseek-ai/dsh-mcp-client'\n" +
			"      config:\n" +
			"        serverName: web\n" + line +
			"        url: https://api.example.com/mcp\n" +
			cordisBlockEnd + "\n"
		file, err := ReadCordisPatch(writeCordisPatch(t, src))
		if err != nil {
			t.Fatalf("transport %q: read: %v", transport, err)
		}
		got, ok := file.Server("web")
		if !ok {
			t.Fatalf("transport %q: row missing", transport)
		}
		want := Server{Transport: "http", URL: "https://api.example.com/mcp"}
		if got.Fingerprint() == want.Fingerprint() {
			t.Fatalf("transport %q reads as up to date; a hand-edited broken entry would never be repaired", transport)
		}
	}
}

func TestWriteWithBackupTightensExistingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	// A pre-existing world-readable backup must be tightened too.
	if err := os.WriteFile(path+BackupSuffix, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path+BackupSuffix, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteWithBackup(path, []byte("new\n")); err != nil {
		t.Fatalf("WriteWithBackup: %v", err)
	}
	for _, target := range []string{path, path + BackupSuffix} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600 after overwriting a world-readable file", filepath.Base(target), info.Mode().Perm())
		}
	}
}

func TestCordisPatchRejectsIDTargetedOverrideOutsideBlock(t *testing.T) {
	t.Setenv("DSH_HOME", "")
	path := writeCordisPatch(t, "# layer\n[]\n")
	file, err := ReadCordisPatch(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	file.Set("github", Server{Transport: "stdio", Command: "npx"})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// DSH applies an id-targeted row after the block's insert has created the
	// entry, so this hand-written row would silently override a senv-managed
	// server without senv seeing drift.
	override := strings.TrimRight(string(data), "\n") + `
- id: senv-mcp-github
  config:
    command: evil
`
	_, err = ReadCordisPatch(writeCordisPatch(t, override))
	if err == nil || !strings.Contains(err.Error(), "id-targeted patch rows") {
		t.Fatalf("error = %v, want an id-targeted override rejection", err)
	}
}
