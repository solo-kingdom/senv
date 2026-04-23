package ref

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DefaultMaxDepth is the default maximum recursion depth for reference resolution
const DefaultMaxDepth = 10

// refRegex matches {{env:...}} or {{text:...}} patterns
var refRegex = regexp.MustCompile(`\{\{(env|text):([^}]+)\}\}`)

// escapedRefRegex matches \{{...}} patterns (escaped references)
var escapedRefRegex = regexp.MustCompile(`\\\{\{[^}]+\}\}`)

// ValueGetter provides the ability to retrieve actual values for references
type ValueGetter interface {
	GetEnvValue(group, key string) (string, error)
	GetTextValue(group, key string) (string, error)
}

// ResolveOptions controls how references are resolved
type ResolveOptions struct {
	Loose        bool   // If true, unresolved references are kept as-is instead of causing errors
	MaxDepth     int    // Maximum recursion depth (0 = use DefaultMaxDepth)
	CurrentGroup string // Current group for implicit group resolution
}

// ResolveResult contains the resolved value and any warnings
type ResolveResult struct {
	Value    string
	Warnings []string
}

// RefError represents a reference resolution error with chain information
type RefError struct {
	Chain []string
	Err   error
}

func (e *RefError) Error() string {
	return fmt.Sprintf("circular reference detected: %s", strings.Join(e.Chain, " → "))
}

// UnresolvedRefError represents an unresolved reference error
type UnresolvedRefError struct {
	Ref   string
	Cause string
}

func (e *UnresolvedRefError) Error() string {
	return fmt.Sprintf("unresolved reference {{%s}}: %s", e.Ref, e.Cause)
}

// Resolve recursively resolves all references in a value
func Resolve(value string, getter ValueGetter, opts ResolveOptions) (string, error) {
	if opts.MaxDepth == 0 {
		opts.MaxDepth = DefaultMaxDepth
	}

	visited := make(map[string]bool)
	warnings := []string{}

	result, err := resolveRecursive(value, getter, opts, visited, 0, &warnings)
	if err != nil {
		return "", err
	}

	return result, nil
}

// resolveRecursive performs the actual recursive resolution
func resolveRecursive(
	value string,
	getter ValueGetter,
	opts ResolveOptions,
	visited map[string]bool,
	depth int,
	warnings *[]string,
) (string, error) {
	// Check max depth
	if depth >= opts.MaxDepth {
		return "", fmt.Errorf("maximum reference depth exceeded (%d)", opts.MaxDepth)
	}

	// First handle escaped references: \{{...}} → {{...}}
	// We do this by replacing escaped patterns temporarily
	escaped := make(map[string]string)
	escapedIdx := 0
	processed := escapedRefRegex.ReplaceAllStringFunc(value, func(match string) string {
		// Remove the leading backslash
		placeholder := fmt.Sprintf("\x00ESCAPED_%d\x00", escapedIdx)
		escaped[placeholder] = strings.TrimPrefix(match, "\\")
		escapedIdx++
		return placeholder
	})

	// Find all references
	matches := refRegex.FindAllStringSubmatchIndex(processed, -1)
	if len(matches) == 0 {
		// No more references, restore escaped patterns
		return restoreEscaped(processed, escaped), nil
	}

	// Process references in reverse order to maintain correct indices
	result := processed
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		fullMatch := processed[match[0]:match[1]]
		refType := processed[match[2]:match[3]]
		refPath := processed[match[4]:match[5]]

		// Parse the reference
		group, key := parseRefPath(refPath, opts.CurrentGroup)

		// Build reference key for cycle detection
		refKey := fmt.Sprintf("%s:%s:%s", refType, group, key)

		// Cycle detection
		if visited[refKey] {
			chain := make([]string, 0, len(visited)+1)
			for k := range visited {
				chain = append(chain, "{{"+k+"}}")
			}
			chain = append(chain, "{{"+refKey+"}}")
			return "", &RefError{Chain: chain}
		}

		// Get the actual value
		resolved, err := getValue(refType, group, key, getter)
		if err != nil {
			if opts.Loose {
				// In loose mode, keep the reference as-is and add warning
				*warnings = append(*warnings, fmt.Sprintf("warning: unresolved reference %s: %v", fullMatch, err))
				continue
			}
			return "", &UnresolvedRefError{
				Ref:   refType + ":" + refPath,
				Cause: err.Error(),
			}
		}

		// Mark as visited
		visited[refKey] = true

		// Recursively resolve the retrieved value
		resolved, err = resolveRecursive(resolved, getter, opts, visited, depth+1, warnings)
		if err != nil {
			return "", err
		}

		// Unmark (backtrack for other branches)
		delete(visited, refKey)

		// Replace in result
		result = result[:match[0]] + resolved + result[match[1]:]
	}

	// Restore escaped patterns
	return restoreEscaped(result, escaped), nil
}

// parseRefPath parses "key" or "group:key" format
func parseRefPath(path, currentGroup string) (group, key string) {
	parts := strings.SplitN(path, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	// No group specified, use current group
	return currentGroup, parts[0]
}

// getValue retrieves the actual value from the getter
func getValue(refType, group, key string, getter ValueGetter) (string, error) {
	switch refType {
	case "env":
		val, err := getter.GetEnvValue(group, key)
		if err != nil {
			return "", fmt.Errorf("env group '%s' key '%s' not found", group, key)
		}
		return val, nil
	case "text":
		val, err := getter.GetTextValue(group, key)
		if err != nil {
			return "", fmt.Errorf("text group '%s' key '%s' not found", group, key)
		}
		return val, nil
	default:
		return "", fmt.Errorf("unknown reference type '%s'", refType)
	}
}

// restoreEscaped replaces placeholder strings back with the original escaped content
func restoreEscaped(s string, escaped map[string]string) string {
	for placeholder, original := range escaped {
		s = strings.Replace(s, placeholder, original, -1)
	}
	return s
}

// HasReferences checks if a value contains any references
func HasReferences(value string) bool {
	return refRegex.MatchString(value)
}

// GetReferences extracts all references from a value without resolving them
func GetReferences(value string) []string {
	matches := refRegex.FindAllStringSubmatch(value, -1)
	var refs []string
	for _, match := range matches {
		if len(match) >= 3 {
			refs = append(refs, match[1]+":"+match[2])
		}
	}
	return refs
}

// FormatRefChain formats a reference chain for error display
func FormatRefChain(chain []string) string {
	return strings.Join(chain, " → ")
}

// ResolveWithWarnings resolves references and returns both the result and any warnings
func ResolveWithWarnings(value string, getter ValueGetter, opts ResolveOptions) (string, []string, error) {
	if opts.MaxDepth == 0 {
		opts.MaxDepth = DefaultMaxDepth
	}

	visited := make(map[string]bool)
	warnings := []string{}

	result, err := resolveRecursive(value, getter, opts, visited, 0, &warnings)
	if err != nil {
		return "", warnings, err
	}

	return result, warnings, nil
}

// PrintWarnings prints warnings to stderr
func PrintWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}
}
