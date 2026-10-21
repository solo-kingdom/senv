package search

import (
	"fmt"
	"sort"
	"strings"
)

const (
	DefaultLimit = 20

	rankExact  = 0
	rankPrefix = 1
	rankSub    = 2
)

// Item is one metadata hit. It intentionally has no Value field so secrets
// cannot be serialized by accident.
type Item struct {
	Kind        Kind   `json:"kind"`
	Group       string `json:"group,omitempty"`
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
	Size        *int   `json:"size,omitempty"`

	// match holds the non-secret haystack used for matching. Unexported so it
	// never appears in JSON output.
	match string
}

// MatchText returns the identifier/description haystack for this item.
func (i Item) MatchText() string { return i.match }

// Result is the CLI/MCP search envelope.
type Result struct {
	Query     string `json:"query"`
	Total     int    `json:"total"`
	Truncated bool   `json:"truncated"`
	Hits      []Item `json:"hits"`
}

// ValidateQuery requires a non-empty trimmed query (CLI/MCP).
func ValidateQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// Tokens splits a query into lowercase AND tokens.
func Tokens(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	return fields
}

// Matches reports whether haystack contains every token (case-insensitive).
// An empty/whitespace query matches everything (TUI empty-input browse).
func Matches(haystack, query string) bool {
	tokens := Tokens(query)
	if len(tokens) == 0 {
		return true
	}
	hay := strings.ToLower(haystack)
	for _, tok := range tokens {
		if !strings.Contains(hay, tok) {
			return false
		}
	}
	return true
}

// Run filters, matches, ranks and limits items. query must be non-empty.
func Run(items []Item, query string, kinds []Kind, limit int) (*Result, error) {
	if err := ValidateQuery(query); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, fmt.Errorf("limit must be >= 1")
	}
	q := strings.TrimSpace(query)
	filtered := FilterKinds(items, kinds)
	hits := matchAndRank(filtered, q)
	total := len(hits)
	truncated := total > limit
	if truncated {
		hits = hits[:limit]
	}
	return &Result{
		Query:     q,
		Total:     total,
		Truncated: truncated,
		Hits:      hits,
	}, nil
}

func matchAndRank(items []Item, query string) []Item {
	tokens := Tokens(query)
	type scored struct {
		item Item
		rank int
	}
	var scoredItems []scored
	for _, it := range items {
		if !Matches(it.match, query) {
			continue
		}
		scoredItems = append(scoredItems, scored{item: it, rank: keyRank(it.Key, tokens, query)})
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		a, b := scoredItems[i], scoredItems[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.item.Kind != b.item.Kind {
			return a.item.Kind < b.item.Kind
		}
		if a.item.Group != b.item.Group {
			return a.item.Group < b.item.Group
		}
		return a.item.Key < b.item.Key
	})
	out := make([]Item, len(scoredItems))
	for i, s := range scoredItems {
		out[i] = s.item
	}
	return out
}

func keyRank(key string, tokens []string, query string) int {
	k := strings.ToLower(key)
	full := strings.ToLower(strings.TrimSpace(query))
	if k == full {
		return rankExact
	}
	if strings.HasPrefix(k, full) {
		return rankPrefix
	}
	// Single-token exact/prefix against key elevates ranking.
	if len(tokens) == 1 {
		if k == tokens[0] {
			return rankExact
		}
		if strings.HasPrefix(k, tokens[0]) {
			return rankPrefix
		}
	}
	return rankSub
}

// JoinMatch concatenates non-empty identifier fragments for matching.
func JoinMatch(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return b.String()
}
