package tui

import "strings"

const defaultGroup = "default"

// parseKeyAddress splits new-key input into group and key.
// Plain "key" uses fallbackGroup; "group:key" overrides the group.
// A leading ":key" selects the default group, matching CLI shorthand.
func parseKeyAddress(input, fallbackGroup string) (group, key string) {
	if i := strings.IndexByte(input, ':'); i >= 0 {
		group = input[:i]
		key = input[i+1:]
		if group == "" {
			group = defaultGroup
		}
		return group, key
	}
	return fallbackGroup, input
}
