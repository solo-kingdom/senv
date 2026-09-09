package cmd

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wii/senv/internal/storage"
)

// sshHostView is the explicit MCP response whitelist. It deliberately has no
// field capable of carrying private-key material.
type sshHostView struct {
	Alias       string            `json:"alias"`
	Hostname    string            `json:"hostname,omitempty"`
	User        string            `json:"user,omitempty"`
	Port        int               `json:"port,omitempty"`
	ProxyJump   string            `json:"proxy_jump,omitempty"`
	IdentityKey string            `json:"identity_key,omitempty"`
	Fingerprint string            `json:"identity_fingerprint,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

func sshHostViewFrom(host *storage.HostEntry, fingerprint string) sshHostView {
	return sshHostView{
		Alias:       host.Alias,
		Hostname:    host.Hostname,
		User:        host.User,
		Port:        host.Port,
		ProxyJump:   host.ProxyJump,
		IdentityKey: host.IdentityKey,
		Fingerprint: fingerprint,
		Tags:        host.Tags,
		Extra:       host.Extra,
	}
}

func (m *managers) sshHostList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	hosts, err := m.ssh.ListHosts()
	if err != nil {
		return errResult(err)
	}
	out := make([]sshHostView, 0, len(hosts))
	for _, host := range hosts {
		fingerprint, err := m.sshHostFingerprint(host.IdentityKey)
		if err != nil {
			return errResult(err)
		}
		out = append(out, sshHostViewFrom(host, fingerprint))
	}
	return textResult(out)
}

func (m *managers) sshHostGet(_ context.Context, _ *mcp.CallToolRequest, in configNameInput) (*mcp.CallToolResult, emptyOut, error) {
	m.pullBeforeRead()
	host, err := m.ssh.GetHost(in.Name)
	if err != nil {
		return errResult(err)
	}
	fingerprint, err := m.sshHostFingerprint(host.IdentityKey)
	if err != nil {
		return errResult(err)
	}
	return textResult(sshHostViewFrom(host, fingerprint))
}

func (m *managers) sshHostFingerprint(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	summary, err := m.ssh.GetKeyPairSummary(name)
	if err != nil {
		return "", err
	}
	return summary.Fingerprint, nil
}
