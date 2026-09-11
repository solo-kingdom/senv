package agentcfg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	})
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
	SetJSONServer(root, "mcpServers", "bare", Server{URL: "https://api.example.com/mcp"})
	bare := JSONServers(root, "mcpServers")["bare"].(map[string]any)
	if _, ok := bare["headers"]; ok {
		t.Fatalf("empty headers must be omitted: %v", bare)
	}
	if bare["type"] != "http" {
		t.Fatalf("type = %v, want defaulted http", bare["type"])
	}

	// stdio entries keep the historical shape with no type key.
	SetJSONServer(root, "mcpServers", "gh", Server{Command: "npx", Args: []string{"-y"}, Env: map[string]string{"K": "v"}})
	stdio := JSONServers(root, "mcpServers")["gh"].(map[string]any)
	if _, ok := stdio["type"]; ok {
		t.Fatalf("stdio entry must not gain a type key: %v", stdio)
	}
	if stdio["command"] != "npx" {
		t.Fatalf("stdio entry = %v", stdio)
	}
}

func TestJSONServerDottedKey(t *testing.T) {
	root := map[string]any{"mcp": map[string]any{"other": 1}}
	SetJSONServer(root, "mcp.servers", "web", Server{Transport: "sse", URL: "https://api.example.com/sse"})
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
	SetJSONServer(root, "a.b.c", "x", Server{Command: "npx"})
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
	block := RenderTOMLServerBlock("mcp_servers", "web", Server{Transport: "streamable-http", URL: "https://api.example.com/mcp"})
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
	src := "model = \"gpt\"\n" + RenderTOMLServerBlock("mcp_servers", "web", Server{Transport: "http", URL: "https://api.example.com/mcp"}) + "\n" + RenderTOMLServerBlock("mcp_servers", "gh", Server{Command: "npx"})
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
		{"codex", httpHeaders, true},
		{"zcode", httpHeaders, false},
		{"zcode", sse, true},
		{"kimi", httpHeaders, false},
		{"kimi", sse, true},
		{"pi", httpPlain, true},
		{"pi", sse, true},
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
