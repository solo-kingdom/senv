package agentcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// legacyFingerprint replicates the pre-remote canonicalization so the
// backward-compatibility contract is explicit: fingerprints recorded before
// remote entries existed must never change.
func legacyFingerprint(s Server) string {
	keys := make([]string, 0, len(s.Env))
	for key := range s.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "command=%q\n", s.Command)
	for _, arg := range s.Args {
		fmt.Fprintf(&b, "arg=%q\n", arg)
	}
	for _, key := range keys {
		fmt.Fprintf(&b, "env=%q=%q\n", key, s.Env[key])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestFingerprintStdioBackwardCompatible(t *testing.T) {
	stdio := Server{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-github"},
		Env:     map[string]string{"GITHUB_TOKEN": "x"},
	}
	if stdio.Fingerprint() != legacyFingerprint(stdio) {
		t.Fatal("stdio fingerprint changed; existing ledger records would all read as drift")
	}
	// An explicit stdio transport must not change the digest either, so
	// profiles saved before and after the remote feature stay comparable.
	withTransport := stdio
	withTransport.Transport = "stdio"
	if withTransport.Fingerprint() != stdio.Fingerprint() {
		t.Fatal("explicit stdio transport changed the fingerprint")
	}
}

func TestFingerprintRemote(t *testing.T) {
	http := Server{Transport: "http", URL: "https://api.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer x"}}
	sse := Server{Transport: "sse", URL: "https://api.example.com/sse", Headers: map[string]string{"Authorization": "Bearer x"}}
	if http.Fingerprint() == sse.Fingerprint() {
		t.Fatal("http and sse entries with the same url must have different fingerprints")
	}
	noHeaders := http
	noHeaders.Headers = nil
	if noHeaders.Fingerprint() == http.Fingerprint() {
		t.Fatal("dropping headers must change the fingerprint")
	}
}

func TestSetJSONServerRemoteShape(t *testing.T) {
	root := map[string]any{"other": "kept"}
	SetJSONServer(root, "mcpServers", "web", Server{
		Transport: "http",
		URL:       "https://api.example.com/mcp",
		Headers:   map[string]string{"Authorization": "Bearer x"},
	}, true)
	entry, ok := JSONServers(root, "mcpServers")["web"].(map[string]any)
	if !ok {
		t.Fatal("remote entry not written")
	}
	if entry["type"] != "http" || entry["url"] != "https://api.example.com/mcp" {
		t.Fatalf("remote entry = %v, want type/url keys", entry)
	}
	if entry["headers"] == nil || len(entry["headers"].(map[string]string)) != 1 {
		t.Fatalf("remote entry missing headers: %v", entry)
	}
	if _, ok := entry["command"]; ok {
		t.Fatalf("remote entry must not carry command: %v", entry)
	}
	if root["other"] != "kept" {
		t.Fatalf("sibling key lost: %v", root)
	}

	// Empty headers are omitted, and an empty transport defaults to http.
	SetJSONServer(root, "mcpServers", "bare", Server{URL: "https://api.example.com/mcp"}, true)
	bare := JSONServers(root, "mcpServers")["bare"].(map[string]any)
	if _, ok := bare["headers"]; ok {
		t.Fatalf("empty headers must be omitted: %v", bare)
	}
	if bare["type"] != "http" {
		t.Fatalf("type = %v, want defaulted http", bare["type"])
	}

	// stdio entries keep the historical shape with no type key.
	SetJSONServer(root, "mcpServers", "gh", Server{Command: "npx", Args: []string{"-y"}, Env: map[string]string{"K": "v"}}, true)
	stdio := JSONServers(root, "mcpServers")["gh"].(map[string]any)
	if _, ok := stdio["type"]; ok {
		t.Fatalf("stdio entry must not gain a type key: %v", stdio)
	}
	if stdio["command"] != "npx" {
		t.Fatalf("stdio entry = %v", stdio)
	}

	// A target with no transport type key (pi, kimi) must not gain the key.
	SetJSONServer(root, "mcpServers", "typeless", Server{Transport: "sse", URL: "https://api.example.com/sse"}, false)
	typeless := JSONServers(root, "mcpServers")["typeless"].(map[string]any)
	if _, ok := typeless["type"]; ok {
		t.Fatalf("typeless target must not gain a type key: %v", typeless)
	}
	if typeless["url"] != "https://api.example.com/sse" {
		t.Fatalf("typeless entry = %v", typeless)
	}
}

func TestJSONServerDottedKey(t *testing.T) {
	root := map[string]any{"mcp": map[string]any{"other": 1}}
	SetJSONServer(root, "mcp.servers", "web", Server{Transport: "sse", URL: "https://api.example.com/sse"}, true)
	got, ok := JSONServer(root, "mcp.servers", "web")
	if !ok {
		t.Fatal("entry not found under dotted key")
	}
	if got.Transport != "sse" || got.URL != "https://api.example.com/sse" {
		t.Fatalf("round trip = %+v", got)
	}
	if root["mcp"].(map[string]any)["other"] != 1 {
		t.Fatal("sibling key under the dotted path was lost")
	}
	// Creates missing intermediates.
	SetJSONServer(root, "a.b.c", "x", Server{Command: "npx"}, true)
	if _, ok := JSONServer(root, "a.b.c", "x"); !ok {
		t.Fatal("entry not created under a fresh dotted path")
	}
	if !DeleteJSONServer(root, "mcp.servers", "web") {
		t.Fatal("delete reported absent for a present entry")
	}
	if _, ok := JSONServer(root, "mcp.servers", "web"); ok {
		t.Fatal("entry survived delete")
	}
	if DeleteJSONServer(root, "mcp.servers", "web") {
		t.Fatal("delete of an absent entry must report false")
	}
}

func TestRenderTOMLServerBlockRemote(t *testing.T) {
	block := RenderTOMLServerBlock("mcp_servers", "web", Server{Transport: "streamable-http", URL: "https://api.example.com/mcp"}, true, "headers")
	for _, want := range []string{`[mcp_servers.web]`, `transport = "streamable-http"`, `url = "https://api.example.com/mcp"`} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "command") || strings.Contains(block, "headers") {
		t.Fatalf("remote TOML block must not carry command/headers:\n%s", block)
	}
}

func TestTOMLServersRoundTrip(t *testing.T) {
	src := "model = \"gpt\"\n" + RenderTOMLServerBlock("mcp_servers", "web", Server{Transport: "http", URL: "https://api.example.com/mcp"}, true, "headers") + "\n" + RenderTOMLServerBlock("mcp_servers", "gh", Server{Command: "npx"}, true, "headers")
	servers, err := TOMLServers(src, "mcp_servers")
	if err != nil {
		t.Fatalf("TOMLServers: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %v, want 2 entries", servers)
	}
	web, ok := TOMLServer(src, "mcp_servers", "web")
	if !ok || web.URL != "https://api.example.com/mcp" || web.Transport != "http" {
		t.Fatalf("remote round trip = %+v, ok=%v", web, ok)
	}
	gh, ok := TOMLServer(src, "mcp_servers", "gh")
	if !ok || gh.Command != "npx" {
		t.Fatalf("stdio round trip = %+v, ok=%v", gh, ok)
	}
	if _, err := TOMLServers("not [ valid toml", "mcp_servers"); err == nil {
		t.Fatal("malformed TOML must report an error")
	}
	if servers, err := TOMLServers("model = \"gpt\"\n", "mcp_servers"); err != nil || len(servers) != 0 {
		t.Fatalf("missing table = %v, %v; want empty map", servers, err)
	}
}

func TestRemoteErrorMatrix(t *testing.T) {
	httpPlain := Server{Transport: "http", URL: "https://api.example.com/mcp"}
	httpHeaders := httpPlain
	httpHeaders.Headers = map[string]string{"Authorization": "Bearer x"}
	sse := Server{Transport: "sse", URL: "https://api.example.com/sse"}
	stdio := Server{Command: "npx"}

	cases := []struct {
		agent      string
		srv        Server
		wantReject bool
	}{
		{"claude-code", httpPlain, false},
		{"claude-code", httpHeaders, false},
		{"claude-code", sse, false},
		{"claude-desktop", httpPlain, true},
		{"claude-desktop", sse, true},
		{"cursor", httpHeaders, false},
		{"cursor", sse, false},
		{"codex", httpPlain, false},
		{"codex", sse, false},
		{"codex", httpHeaders, false},
		{"zcode", httpHeaders, false},
		{"zcode", sse, true},
		{"kimi", httpHeaders, false},
		{"kimi", sse, true},
		{"pi", httpPlain, false},
		{"pi", httpHeaders, false},
		{"pi", sse, false},
	}
	for _, tc := range cases {
		target, ok := Find(tc.agent)
		if !ok {
			t.Fatalf("unknown agent %q", tc.agent)
		}
		err := target.RemoteError(tc.srv)
		if tc.wantReject && err == nil {
			t.Fatalf("%s/%+v: expected rejection, got nil", tc.agent, tc.srv)
		}
		if !tc.wantReject && err != nil {
			t.Fatalf("%s/%+v: unexpected rejection: %v", tc.agent, tc.srv, err)
		}
	}
	// stdio always passes, even on targets without remote support.
	for _, agent := range IDs() {
		target, _ := Find(agent)
		if err := target.RemoteError(stdio); err != nil {
			t.Fatalf("%s: stdio rejected: %v", agent, err)
		}
	}
	// Unsupported targets must always carry a human-readable reason.
	for _, target := range Supported() {
		if target.Remote.HTTP || target.Remote.SSE {
			continue
		}
		if strings.TrimSpace(target.Remote.Reason) == "" {
			t.Fatalf("%s: remote unsupported but no reason given", target.ID)
		}
	}
}

func TestRenderTOMLServerBlockTypeKey(t *testing.T) {
	srv := Server{Transport: "http", URL: "https://api.example.com/mcp"}
	if block := RenderTOMLServerBlock("mcp_servers", "web", srv, true, "headers"); !strings.Contains(block, `transport = "http"`) {
		t.Fatalf("typeKey=true block = %q", block)
	}
	if block := RenderTOMLServerBlock("mcp_servers", "web", srv, false, "headers"); strings.Contains(block, "transport") {
		t.Fatalf("typeKey=false block = %q", block)
	}
}

func TestNormalizeDropsUnstorableTransport(t *testing.T) {
	http := Server{Transport: "http", URL: "https://api.example.com/mcp", Headers: map[string]string{"A": "b"}}
	sse := Server{Transport: "sse", URL: "https://api.example.com/mcp", Headers: map[string]string{"A": "b"}}
	stdio := Server{Command: "npx"}

	typed, ok := Find("cursor")
	if !ok || !typed.Remote.TypeKey {
		t.Fatal("cursor must be a transport-type-key target for this test")
	}
	if got := typed.Normalize(sse); got.Transport != "sse" {
		t.Fatalf("typed target Normalize = %+v; must keep transport", got)
	}

	typeless, ok := Find("pi")
	if !ok || typeless.Remote.TypeKey {
		t.Fatal("pi must be a typeless target for this test")
	}
	// A typeless target stores http and sse identically, so their comparable
	// form (and therefore fingerprint) must match or every plan flips to drift.
	if typeless.Normalize(http).Fingerprint() != typeless.Normalize(sse).Fingerprint() {
		t.Fatal("typeless target must treat http and sse as the same stored entry")
	}
	if got := typeless.Normalize(stdio); got.Transport != "" || got.Command != "npx" {
		t.Fatalf("stdio normalization must be a no-op: %+v", got)
	}
}

func TestZCodeTargetPaths(t *testing.T) {
	target, ok := Find("zcode")
	if !ok {
		t.Fatal("zcode target missing")
	}
	// Verified against a live ZCode install: servers live under mcp.servers
	// in ~/.zcode/cli/config.json, not ~/.zcode/config.json.
	if target.JSONServersKey != "mcp.servers" {
		t.Fatalf("JSONServersKey = %q, want mcp.servers", target.JSONServersKey)
	}
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/.zcode/cli/config.json" {
		t.Fatalf("config path = %q", path)
	}
}

func TestKimiTargetPaths(t *testing.T) {
	target, ok := Find("kimi")
	if !ok {
		t.Fatal("kimi target missing")
	}
	// Verified against a live install: Kimi Code CLI reads
	// ~/.kimi-code/mcp.json; the retired ~/.kimi/mcp.json is never read.
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/.kimi-code/mcp.json" {
		t.Fatalf("config path = %q", path)
	}
}

func TestPiTargetPaths(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	target, ok := Find("pi")
	if !ok {
		t.Fatal("pi target missing")
	}
	// PI has no built-in MCP: the Pi global MCP override is read by the
	// pi-mcp-adapter extension, which resolves the agent dir the same way as
	// pi ($PI_CODING_AGENT_DIR, default ~/.pi/agent).
	if target.JSONServersKey != "mcpServers" {
		t.Fatalf("JSONServersKey = %q, want mcpServers", target.JSONServersKey)
	}
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/.pi/agent/mcp.json" {
		t.Fatalf("config path = %q", path)
	}
	if target.Prerequisite == nil || target.Prerequisite.Display == "" {
		t.Fatal("pi must declare the pi-mcp-adapter prerequisite")
	}

	t.Setenv("PI_CODING_AGENT_DIR", "/opt/pi-agent")
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/opt/pi-agent/mcp.json" {
		t.Fatalf("absolute PI_CODING_AGENT_DIR path = %q", path)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "~/custom/pi")
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/custom/pi/mcp.json" {
		t.Fatalf("~ PI_CODING_AGENT_DIR path = %q", path)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "~")
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/mcp.json" {
		t.Fatalf("bare ~ PI_CODING_AGENT_DIR path = %q", path)
	}
}

func TestAdapterTargetsDeclarePrerequisite(t *testing.T) {
	// Targets whose config is only read by an external extension must carry a
	// visible, installable prerequisite; native readers must not.
	for _, target := range Supported() {
		if target.ID == "pi" {
			if target.Prerequisite == nil {
				t.Fatalf("%s: missing Prerequisite", target.ID)
			}
			if target.Prerequisite.Package == "" || len(target.Prerequisite.Args) == 0 {
				t.Fatalf("%s: prerequisite is not installable: %+v", target.ID, target.Prerequisite)
			}
			continue
		}
		if target.Prerequisite != nil {
			t.Fatalf("%s: unexpected Prerequisite %+v", target.ID, target.Prerequisite)
		}
	}
}

func TestOmpTargetPaths(t *testing.T) {
	t.Setenv("OMP_PROFILE", "")
	t.Setenv("PI_PROFILE", "")
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	target, ok := Find("omp")
	if !ok {
		t.Fatal("omp target missing")
	}
	if target.JSONServersKey != "mcpServers" {
		t.Fatalf("JSONServersKey = %q, want mcpServers", target.JSONServersKey)
	}
	// OMP has built-in MCP support: no extension prerequisite, unlike pi.
	if target.Prerequisite != nil {
		t.Fatalf("omp must not declare a Prerequisite: %+v", target.Prerequisite)
	}
	if !target.Remote.TypeKey || !target.Remote.HTTP || !target.Remote.SSE || !target.Remote.Headers {
		t.Fatalf("omp remote capability = %+v, want http+sse+headers+typekey", target.Remote)
	}
	if path := target.ResolveConfigPath("/home/u", "user"); path != "/home/u/.omp/agent/mcp.json" {
		t.Fatalf("user config path = %q", path)
	}
	if path := target.ResolveConfigPath("/home/u", "project"); path != ".omp/mcp.json" {
		t.Fatalf("project config path = %q", path)
	}
}

func TestOmpAgentDirResolution(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, "/home/u/.omp/agent"},
		{"omp profile", map[string]string{"OMP_PROFILE": "work"}, "/home/u/.omp/profiles/work/agent"},
		{"pi profile fallback", map[string]string{"PI_PROFILE": "work"}, "/home/u/.omp/profiles/work/agent"},
		{"omp wins over pi", map[string]string{"OMP_PROFILE": "work", "PI_PROFILE": "other"}, "/home/u/.omp/profiles/work/agent"},
		{"empty omp disables pi", map[string]string{"OMP_PROFILE": "", "PI_PROFILE": "work"}, "/home/u/.omp/agent"},
		{"default sentinel", map[string]string{"OMP_PROFILE": "default"}, "/home/u/.omp/agent"},
		{"invalid profile uppercase", map[string]string{"OMP_PROFILE": "Work"}, "/home/u/.omp/agent"},
		{"invalid profile trailing dot", map[string]string{"OMP_PROFILE": "work."}, "/home/u/.omp/agent"},
		{"invalid profile windows reserved", map[string]string{"OMP_PROFILE": "CON"}, "/home/u/.omp/agent"},
		{"config dir", map[string]string{"PI_CONFIG_DIR": ".omx"}, "/home/u/.omx/agent"},
		{"agent dir override", map[string]string{"PI_CODING_AGENT_DIR": "/opt/omp-agent"}, "/opt/omp-agent"},
		{"agent dir override ignores tilde", map[string]string{"PI_CODING_AGENT_DIR": "~/custom"}, "/home/u/current/~/custom"},
		{"profile ignores agent dir override", map[string]string{"OMP_PROFILE": "work", "PI_CODING_AGENT_DIR": "/opt/omp-agent"}, "/home/u/.omp/profiles/work/agent"},
	}
	unsetenv := func(t *testing.T, key string) {
		t.Helper()
		old, had := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				os.Setenv(key, old)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 未设置的变量必须真正 unset：OMP_PROFILE 定义为空会压制
			// PI_PROFILE，Setenv(key, "") 无法表达这个区别。
			for _, key := range []string{"OMP_PROFILE", "PI_PROFILE", "PI_CONFIG_DIR", "PI_CODING_AGENT_DIR"} {
				unsetenv(t, key)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd() error = %v", err)
			}
			// 相对 PI_CODING_AGENT_DIR 锚定进程 cwd（镜像 omp 的 path.resolve）。
			want := tc.want
			if tc.name == "agent dir override ignores tilde" {
				want = filepath.Join(cwd, "~/custom")
			}
			if got := OmpAgentDir("/home/u"); got != want {
				t.Fatalf("OmpAgentDir = %q, want %q", got, want)
			}
		})
	}
}

func TestFindAcceptsAliasButIDsStayCanonical(t *testing.T) {
	target, ok := Find("oh-my-pi")
	if !ok || target.ID != "omp" {
		t.Fatalf("Find(oh-my-pi) = %+v, %v; want omp", target, ok)
	}
	target, ok = Find("OH-MY-PI")
	if !ok || target.ID != "omp" {
		t.Fatalf("Find(OH-MY-PI) = %+v, %v; want omp", target, ok)
	}
	for _, id := range IDs() {
		if id == "oh-my-pi" {
			t.Fatal("IDs() must not contain alias oh-my-pi")
		}
	}
}
