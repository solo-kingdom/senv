package search

import (
	"fmt"
	"strings"
)

// Kind identifies a vault inventory type searchable by metadata-search.
type Kind string

const (
	KindEnv    Kind = "env"
	KindText   Kind = "text"
	KindBackup Kind = "backup"
	KindConfig Kind = "config"
	KindSSH    Kind = "ssh"
	KindLLM    Kind = "llm"
	KindMCP    Kind = "mcp"
)

// AllKinds is the stable default search scope (aligned with TUI global search).
var AllKinds = []Kind{
	KindEnv, KindText, KindBackup, KindConfig, KindSSH, KindLLM, KindMCP,
}

func (k Kind) valid() bool {
	switch k {
	case KindEnv, KindText, KindBackup, KindConfig, KindSSH, KindLLM, KindMCP:
		return true
	default:
		return false
	}
}

// ParseKinds parses kind filters from repeated flags and/or comma-separated
// values. Empty input means all kinds. Unknown kinds are errors.
func ParseKinds(raw []string) ([]Kind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[Kind]struct{})
	var out []Kind
	for _, piece := range raw {
		for _, part := range strings.Split(piece, ",") {
			name := strings.ToLower(strings.TrimSpace(part))
			if name == "" {
				continue
			}
			k := Kind(name)
			if !k.valid() {
				return nil, fmt.Errorf("unknown kind %q (want env|text|backup|config|ssh|llm|mcp)", name)
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid kinds in %q", raw)
	}
	return out, nil
}

// FilterKinds keeps items whose kind is in kinds. Nil/empty kinds keep all.
func FilterKinds(items []Item, kinds []Kind) []Item {
	if len(kinds) == 0 {
		return items
	}
	allow := make(map[Kind]struct{}, len(kinds))
	for _, k := range kinds {
		allow[k] = struct{}{}
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if _, ok := allow[it.Kind]; ok {
			out = append(out, it)
		}
	}
	return out
}
