package ssh

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wii/senv/internal/storage"
)

// RenderFilter selects the render scope. Host and Group are mutually
// exclusive (enforced by the CLI layer); both empty means every host.
type RenderFilter struct {
	Host  string
	Group string
}

// RenderResult is the pure, side-effect-free output of Render: per-group
// fragments plus everything Apply needs to materialize keys and warn.
type RenderResult struct {
	// Fragments maps the vault group name ("" = ungrouped) to its rendered
	// OpenSSH config fragment. A Host-filtered render returns a single entry
	// keyed by that host's group and containing only the host plus its
	// transitive ProxyJump closure.
	Fragments map[string]string
	// Order is the sorted group keys of Fragments, "" first.
	Order []string
	// Warnings lists each IdentityKey reference whose target keypair is not
	// present in the local vault (per ADR-0020 D4, e.g. the host archive has
	// synced but the keypair has not); dangling references do not abort the
	// render.
	Warnings []string
	// Referenced holds every in-vault keypair referenced by a rendered host,
	// keyed by name. Apply materializes these.
	Referenced map[string]*storage.KeyPairEntry
}

// Render renders OpenSSH config fragments per group in a deterministic order.
// It performs no filesystem writes; Apply adds the side effects. Unlike the
// old Export, Render decrypts each referenced keypair to learn its group —
// the grouped IdentityFile layout (ADR-0023) derives paths from it.
func (m *Manager) Render(filter RenderFilter) (*RenderResult, error) {
	hosts, err := m.ListHosts()
	if err != nil {
		return nil, err
	}
	byAlias := make(map[string]*storage.HostEntry, len(hosts))
	for _, host := range hosts {
		byAlias[host.Alias] = host
	}

	selected, err := selectHosts(hosts, byAlias, filter)
	if err != nil {
		return nil, err
	}
	for _, host := range selected {
		if host.ProxyJump != "" {
			if _, ok := byAlias[host.ProxyJump]; !ok {
				return nil, fmt.Errorf("host %q has dangling proxyJump %q", host.Alias, host.ProxyJump)
			}
		}
	}

	blocks := groupHostsWithClosure(selected, byAlias)

	res := &RenderResult{
		Fragments:  make(map[string]string, len(blocks)),
		Referenced: map[string]*storage.KeyPairEntry{},
	}
	identityPaths := make(map[string]string)
	referencedGroups := make(map[string]bool)
	ungroupedPlaceholder := false
	for _, list := range blocks {
		for _, host := range list {
			if host.IdentityKey == "" {
				continue
			}
			entry, err := m.loadKeyPair(host.IdentityKey)
			switch {
			case err == nil:
				res.Referenced[entry.Name] = entry
				referencedGroups[entry.Group] = true
				path, pathErr := MaterializePath(entry.Group, entry.Name)
				if pathErr != nil {
					return nil, pathErr
				}
				identityPaths[host.Alias] = path
			case errors.Is(err, os.ErrNotExist):
				// 分组未知时按未分组占位渲染；keypair 同步到位后的下一次
				// 导出会重渲染为真实分组路径。
				res.Warnings = append(res.Warnings, fmt.Sprintf("host %s 引用的 keypair %s 不在本机 vault（可能尚未同步）",
					host.Alias, host.IdentityKey))
				ungroupedPlaceholder = true
				path, pathErr := MaterializePath("", host.IdentityKey)
				if pathErr != nil {
					return nil, pathErr
				}
				identityPaths[host.Alias] = path
			default:
				return nil, err
			}
		}
	}
	if err := checkUngroupedCollision(blocks, referencedGroups, ungroupedPlaceholder); err != nil {
		return nil, err
	}

	groups := make([]string, 0, len(blocks))
	for group := range blocks {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i] == "" {
			return true
		}
		if groups[j] == "" {
			return false
		}
		return groups[i] < groups[j]
	})
	for _, group := range groups {
		res.Fragments[group] = renderFragment(group, blocks[group], identityPaths)
	}
	res.Order = groups
	return res, nil
}

// selectHosts applies the filter. A Host filter selects exactly that host; a
// Group filter selects every host of the group (unknown group is an error so
// typos fail fast instead of silently writing an empty fragment).
func selectHosts(hosts []*storage.HostEntry, byAlias map[string]*storage.HostEntry, filter RenderFilter) ([]*storage.HostEntry, error) {
	switch {
	case filter.Host != "":
		host, ok := byAlias[filter.Host]
		if !ok {
			return nil, fmt.Errorf("host %q not found", filter.Host)
		}
		return []*storage.HostEntry{host}, nil
	case filter.Group != "":
		var selected []*storage.HostEntry
		for _, host := range hosts {
			if host.Group == filter.Group {
				selected = append(selected, host)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("group %q not found (no hosts)", filter.Group)
		}
		return selected, nil
	default:
		return hosts, nil
	}
}

// groupHostsWithClosure buckets the selected hosts by group and appends the
// transitive ProxyJump closure of each fragment to the initiating fragment —
// a grouped export stays self-contained for connectivity (ADR-0023 D3).
func groupHostsWithClosure(selected []*storage.HostEntry, byAlias map[string]*storage.HostEntry) map[string][]*storage.HostEntry {
	blocks := make(map[string][]*storage.HostEntry)
	for _, host := range selected {
		blocks[host.Group] = append(blocks[host.Group], host)
	}
	for group := range blocks {
		inFragment := make(map[string]bool, len(blocks[group]))
		for _, host := range blocks[group] {
			inFragment[host.Alias] = true
		}
		for i := 0; i < len(blocks[group]); i++ {
			jump := blocks[group][i].ProxyJump
			if jump == "" || inFragment[jump] {
				continue
			}
			target := byAlias[jump]
			inFragment[jump] = true
			blocks[group] = append(blocks[group], target)
		}
		sort.Slice(blocks[group], func(i, j int) bool { return blocks[group][i].Alias < blocks[group][j].Alias })
	}
	return blocks
}

// checkUngroupedCollision fails fast when a real group claims the reserved
// name while the ungrouped slot is also in use — the two would map to the
// same groups/_ungrouped.conf and keys/_ungrouped/ targets.
func checkUngroupedCollision(blocks map[string][]*storage.HostEntry, referencedGroups map[string]bool, ungroupedPlaceholder bool) error {
	_, realUngrouped := blocks[ungroupedGroup]
	if !realUngrouped && !referencedGroups[ungroupedGroup] {
		return nil
	}
	usesUngroupedSlot := ungroupedPlaceholder || referencedGroups[""]
	for group := range blocks {
		if group == "" {
			usesUngroupedSlot = true
			break
		}
	}
	if !usesUngroupedSlot {
		return nil
	}
	return fmt.Errorf("group name %q is reserved for ungrouped hosts/keypairs; rename the real group %q first", ungroupedGroup, ungroupedGroup)
}

func renderFragment(group string, hosts []*storage.HostEntry, identityPaths map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# generated by senv host export — do not edit\n")
	fmt.Fprintf(&b, "# group: %s\n", displayGroup(group))
	for _, host := range hosts {
		renderHost(&b, host, identityPaths[host.Alias])
	}
	return b.String()
}

func displayGroup(group string) string {
	if group == "" {
		return ungroupedGroup
	}
	return group
}
