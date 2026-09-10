package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/agentcfg"
)

// The agent registry and its format merge primitives live in
// internal/agentcfg so that `senv mcp install` (senv's own MCP server) and
// `senv mcp export` (user profiles) share exactly one write path. These
// aliases keep the install-side code and tests reading naturally.
type (
	agentFormat = agentcfg.Format
	agentTarget = agentcfg.Target
)

const (
	formatJSON = agentcfg.FormatJSON
	formatTOML = agentcfg.FormatTOML
)

// mcpServerSpec is the canonical description of the senv MCP server that gets
// embedded in every target agent's config. command is resolved to an absolute
// path at install time so the agent can spawn it regardless of its own PATH.
type mcpServerSpec struct {
	Command string
	Args    []string
	Env     map[string]string
}

// defaultServerSpec builds the spec, resolving the senv binary to an absolute
// path when possible. When resolve is false (e.g. --print), the command stays
// as "senv" for readability in pasted snippets.
func defaultServerSpec(resolve bool) mcpServerSpec {
	cmd := "senv"
	if resolve {
		if abs, err := absExecutable(); err == nil && abs != "" {
			cmd = abs
		}
	}
	return mcpServerSpec{Command: cmd, Args: []string{"mcp", "serve"}}
}

// server converts the install-time spec into the shared write shape.
func (s mcpServerSpec) server() agentcfg.Server {
	return agentcfg.Server{Command: s.Command, Args: s.Args, Env: s.Env}
}

// absExecutable returns the absolute path to the running senv binary, or an
// error if it cannot be determined.
func absExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	return exe, nil
}

// supportedAgents is the registry of installable agents, keyed by id.
func supportedAgents() []agentTarget {
	return agentcfg.Supported()
}

// findAgent looks up an agent by id (case-insensitive).
func findAgent(id string) (agentTarget, bool) {
	return agentcfg.Find(id)
}

// supportedAgentIDs returns the list of agent ids, for listing/errors.
func supportedAgentIDs() []string {
	return agentcfg.IDs()
}

// formatAgentList renders the agent table for `senv mcp install` with no args.
func formatAgentList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Supported agents:\n")
	for _, a := range supportedAgents() {
		fmt.Fprintf(&b, "  %-16s %s\n", a.ID, a.Name)
	}
	fmt.Fprintf(&b, "\nRun: senv mcp install <agent> [--scope user|project] [--print]")
	return b.String()
}
