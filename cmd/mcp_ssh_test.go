package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPSSHHostToolsReturnWhitelistedMetadataOnly(t *testing.T) {
	newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, t.TempDir(), "id_test", "mcp@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile = "" })
	runSSHCommand(t, keypairImportCmd.RunE(nil, []string{"mcp-key", "--file", keyPath}))
	hostAddHostname = "web.example"
	hostAddKeypair = "mcp-key"
	hostAddAttrs = []string{"ForwardAgent=yes"}
	t.Cleanup(func() { hostAddHostname, hostAddKeypair, hostAddAttrs = "", "", nil })
	runSSHCommand(t, hostAddCmd.RunE(nil, []string{"web"}))

	sshManager, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	requestManagers := &managers{ssh: sshManager, autoPull: func() {}}
	get, _, err := requestManagers.sshHostGet(context.Background(), nil, configNameInput{Name: "web"})
	if err != nil || get.IsError {
		t.Fatalf("ssh_host_get = %v, %v", get, err)
	}
	getText := textOf(t, get)
	var host map[string]any
	if err := json.Unmarshal([]byte(getText), &host); err != nil {
		t.Fatalf("decode host response: %v", err)
	}
	allowed := map[string]bool{
		"alias": true, "hostname": true, "user": true, "port": true,
		"proxy_jump": true, "identity_key": true, "identity_fingerprint": true,
		"tags": true, "extra": true,
	}
	for key := range host {
		if !allowed[key] {
			t.Fatalf("unexpected response field %q", key)
		}
	}
	if host["identity_fingerprint"] == "" {
		t.Fatal("host response did not include identity fingerprint")
	}

	list, _, err := requestManagers.sshHostList(context.Background(), nil, struct{}{})
	if err != nil || list.IsError {
		t.Fatalf("ssh_host_list = %v, %v", list, err)
	}
	listText := textOf(t, list)
	if strings.Contains(listText, "BEGIN") || strings.Contains(listText, "PRIVATE KEY") {
		t.Fatalf("MCP response leaked key material: %s", listText)
	}
}

func TestMCPToolCatalogueHasNoPrivateKeyTool(t *testing.T) {
	names := make(map[string]bool)
	for _, tool := range toolCatalogue() {
		names[tool.Name] = true
	}
	if !names["ssh_host_list"] || !names["ssh_host_get"] {
		t.Fatalf("read-only SSH tools missing from catalogue: %v", names)
	}
	for _, tool := range toolCatalogue() {
		lower := strings.ToLower(tool.Name + " " + tool.Description)
		if strings.Contains(lower, "keypair get") || strings.Contains(lower, "private key content") {
			t.Fatalf("catalogue exposes private-key content tool: %+v", tool)
		}
	}
}
