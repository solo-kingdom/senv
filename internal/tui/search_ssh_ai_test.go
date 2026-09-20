package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

// searchFor runs the overlay's filtering logic against a fixed inventory.
func searchFor(all []searchResult, needle string) []searchResult {
	s := &searchTab{gathered: all, input: needle}
	s.refilter()
	return s.results
}

func TestSearchCoversSSHHostsAndProviders(t *testing.T) {
	mgrs := newFullManagers(t)
	if err := mgrs.SSH.AddHost(&storage.HostEntry{
		Alias: "web", Hostname: "10.0.0.9", User: "deploy", Port: 2222,
	}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	if _, err := mgrs.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: "sekret-value-xyz",
		Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := mgrs.MCP.Add(&storage.MCPServerEntry{
		Alias: "github", Transport: storage.MCPTransportStdio, Command: "npx",
	}); err != nil {
		t.Fatalf("add mcp: %v", err)
	}

	all := gatherAll(t, mgrs)

	var sawHost, sawProvider, sawMCP bool
	for _, r := range all {
		switch {
		case r.resultType == typeSSH && r.key == "web":
			sawHost = true
			if r.preview != "deploy@10.0.0.9:2222" {
				t.Fatalf("unexpected host preview: %q", r.preview)
			}
		case r.resultType == typeLLM && r.key == "main":
			sawProvider = true
		case r.resultType == typeMCP && r.key == "github":
			sawMCP = true
			if r.extra != "npx" || r.preview != "npx" {
				t.Fatalf("unexpected MCP preview: %+v", r)
			}
		}
	}
	if !sawHost {
		t.Fatal("SSH host not gathered into search inventory")
	}
	if !sawProvider {
		t.Fatal("LLM provider not gathered into search inventory")
	}
	if !sawMCP {
		t.Fatal("MCP profile not gathered into search inventory")
	}

	// Alias and hostname are both matchable identifiers.
	if got := searchFor(all, "web"); len(got) == 0 {
		t.Fatal("host alias did not match")
	}
	if got := searchFor(all, "10.0.0.9"); len(got) == 0 {
		t.Fatal("host hostname did not match")
	}
	if got := searchFor(all, "mai"); len(got) == 0 {
		t.Fatal("provider alias did not match")
	}
	if got := searchFor(all, "github"); len(got) == 0 {
		t.Fatal("MCP alias did not match")
	}
	if got := searchFor(all, "npx"); len(got) == 0 {
		t.Fatal("MCP command did not match")
	}
}

func TestSearchNeverMatchesSecrets(t *testing.T) {
	mgrs := newFullManagers(t)

	// Plant a real private key (import validates the key format) and a provider
	// credential, then prove neither is reachable through search.
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(private, "senv-test")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	body := pemBodySample(pemBytes)
	if _, err := mgrs.SSH.ImportKeyPair("web-key", keyPath, false); err != nil {
		t.Fatalf("import keypair: %v", err)
	}
	const apiKey = "sekret-value-xyz"
	if _, err := mgrs.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com", APIKey: apiKey,
		Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	const mcpToken = "mcp-env-secret-token"
	if err := mgrs.MCP.Add(&storage.MCPServerEntry{
		Alias:     "github",
		Transport: storage.MCPTransportStdio,
		Command:   "npx",
		Env:       map[string]string{"GITHUB_TOKEN": mcpToken},
	}); err != nil {
		t.Fatalf("add mcp: %v", err)
	}

	all := gatherAll(t, mgrs)
	for _, needle := range []string{body, apiKey, mcpToken} {
		if got := searchFor(all, needle); len(got) != 0 {
			t.Fatalf("search matched %q for secret %q", got, needle)
		}
		for _, r := range all {
			for _, field := range []string{r.key, r.extra, r.preview} {
				if strings.Contains(field, needle) {
					t.Fatalf("search inventory leaked %q in %q", needle, field)
				}
			}
		}
	}
}

// pemBodySample returns a distinctive slice of a PEM body so the test can look
// for the private key material itself rather than the PEM armour.
func pemBodySample(pemBytes []byte) string {
	for _, line := range strings.Split(string(pemBytes), "\n") {
		if strings.HasPrefix(line, "-----") || len(line) < 24 {
			continue
		}
		return line[:24]
	}
	return string(pemBytes)
}
