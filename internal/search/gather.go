package search

import (
	"fmt"
	"strings"

	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/text"
)

// Sources holds optional domain managers. Nil managers are skipped.
type Sources struct {
	Env    *env.Manager
	Text   *text.Manager
	Backup *backup.Manager
	Config *config.Manager
	SSH    *ssh.Manager
	LLM    *llm.ProviderManager
	MCP    *mcp.Manager
}

// Gather builds a metadata-only inventory across all present sources.
// Failures for a single kind are skipped so one bad group does not abort search.
func Gather(src Sources) []Item {
	var out []Item
	out = append(out, gatherEnv(src.Env)...)
	out = append(out, gatherText(src.Text)...)
	out = append(out, gatherBackup(src.Backup)...)
	out = append(out, gatherConfig(src.Config)...)
	out = append(out, gatherSSH(src.SSH)...)
	out = append(out, gatherLLM(src.LLM)...)
	out = append(out, gatherMCP(src.MCP)...)
	return out
}

func gatherEnv(m *env.Manager) []Item {
	if m == nil {
		return nil
	}
	groupDesc := map[string]string{}
	if groups, err := m.ListGroups(); err == nil {
		for _, g := range groups {
			groupDesc[g.Name] = g.Description
		}
	}
	vars, err := m.ListVarInfo("")
	if err != nil {
		return nil
	}
	var out []Item
	for group, keys := range vars {
		gDesc := groupDesc[group]
		for key, info := range keys {
			// Discard info.Value intentionally — only description is kept.
			out = append(out, Item{
				Kind:        KindEnv,
				Group:       group,
				Key:         key,
				Description: info.Description,
				Preview:     "***",
				match:       JoinMatch(group, key, info.Description, gDesc),
			})
		}
	}
	return out
}

func gatherText(m *text.Manager) []Item {
	if m == nil {
		return nil
	}
	groups, err := m.ListGroups()
	if err != nil {
		return nil
	}
	var out []Item
	for _, g := range groups {
		infos, err := m.List(g.Name)
		if err != nil {
			continue
		}
		for _, ti := range infos {
			size := ti.Size
			out = append(out, Item{
				Kind:        KindText,
				Group:       g.Name,
				Key:         ti.Key,
				Description: ti.Description,
				Preview:     fmt.Sprintf("%db", ti.Size),
				Size:        &size,
				match:       JoinMatch(g.Name, ti.Key, ti.Description, g.Description),
			})
		}
	}
	return out
}

func gatherBackup(m *backup.Manager) []Item {
	if m == nil {
		return nil
	}
	groups, err := m.ListGroups()
	if err != nil {
		return nil
	}
	var out []Item
	for _, g := range groups {
		infos, err := m.List(g.Name)
		if err != nil {
			continue
		}
		for _, bi := range infos {
			size := bi.Size
			out = append(out, Item{
				Kind:        KindBackup,
				Group:       g.Name,
				Key:         bi.Key,
				Description: bi.Description,
				Preview:     fmt.Sprintf("%db", bi.Size),
				Size:        &size,
				match:       JoinMatch(g.Name, bi.Key, bi.Description, g.Description),
			})
		}
	}
	return out
}

func gatherConfig(m *config.Manager) []Item {
	if m == nil {
		return nil
	}
	cfgs, err := m.List("")
	if err != nil {
		return nil
	}
	var out []Item
	for _, c := range cfgs {
		out = append(out, Item{
			Kind:        KindConfig,
			Group:       c.Group,
			Key:         c.Name,
			Description: c.Description,
			Preview:     c.TargetPath,
			match:       JoinMatch(c.Group, c.Name, c.Description, c.TargetPath),
		})
	}
	return out
}

func gatherSSH(m *ssh.Manager) []Item {
	if m == nil {
		return nil
	}
	hosts, err := m.ListHosts()
	if err != nil {
		return nil
	}
	var out []Item
	for _, h := range hosts {
		tags := strings.Join(h.Tags, " ")
		out = append(out, Item{
			Kind:        KindSSH,
			Group:       h.Group,
			Key:         h.Alias,
			Description: h.Description,
			Preview:     sshPreview(h.User, h.Hostname, h.Port),
			match:       JoinMatch(h.Alias, h.Hostname, h.Group, tags, h.Description),
		})
	}
	return out
}

func gatherLLM(m *llm.ProviderManager) []Item {
	if m == nil {
		return nil
	}
	providers, err := m.ListProviders()
	if err != nil {
		return nil
	}
	var out []Item
	for _, p := range providers {
		out = append(out, Item{
			Kind:        KindLLM,
			Key:         p.Alias,
			Description: p.Description,
			Preview:     fmt.Sprintf("%d models", len(p.Models)),
			match:       JoinMatch(p.Alias, p.Description),
		})
	}
	return out
}

func gatherMCP(m *mcp.Manager) []Item {
	if m == nil {
		return nil
	}
	servers, err := m.List()
	if err != nil {
		return nil
	}
	var out []Item
	for _, s := range servers {
		out = append(out, Item{
			Kind:        KindMCP,
			Key:         s.Alias,
			Description: s.Description,
			Preview:     s.Command,
			match:       JoinMatch(s.Alias, s.Command, s.Description),
		})
	}
	return out
}

func sshPreview(user, hostname string, port int) string {
	host := hostname
	if user != "" {
		host = user + "@" + host
	}
	if port != 0 {
		host += fmt.Sprintf(":%d", port)
	}
	return host
}
