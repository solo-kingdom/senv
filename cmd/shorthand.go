package cmd

import (
	"fmt"
	"os"
	"strings"
)

const (
	shorthandDefaultGroup = "default"
	shorthandDefaultKey   = "__default"
)

// parseAddress parses a "group:key" address string.
// Returns ok=false if arg contains no ':'.
// Empty group defaults to "default", empty key defaults to "__default".
func parseAddress(arg string) (group, key string, ok bool) {
	if !strings.Contains(arg, ":") {
		return "", "", false
	}
	parts := strings.SplitN(arg, ":", 2)
	group = parts[0]
	if group == "" {
		group = shorthandDefaultGroup
	}
	key = parts[1]
	if key == "" {
		key = shorthandDefaultKey
	}
	return group, key, true
}

// runTextShorthand performs a text set via the group:key shorthand.
// file overrides stdin/editor when non-empty. valueArgs are remaining positional args.
func runTextShorthand(group, key, file string, valueArgs []string) error {
	textManager, err := getTextManager()
	if err != nil {
		return err
	}

	if file != "" {
		return textManager.SetFromFile(group, key, file)
	}

	if isPipe() {
		return textManager.SetFromReader(group, key, os.Stdin)
	}

	if len(valueArgs) >= 1 {
		return textManager.Set(group, key, valueArgs[0])
	}

	return textManager.SetViaEditor(group, key)
}

// runEnvShorthand performs an env set via the group:key shorthand.
// valueArgs contains any remaining positional args after the address.
func runEnvShorthand(group, key string, valueArgs []string) error {
	if len(valueArgs) == 0 {
		return fmt.Errorf("env set requires a value: senv env %s:%s <value>", group, key)
	}

	envManager, err := getEnvManager()
	if err != nil {
		return err
	}

	return envManager.Set(group, key, valueArgs[0])
}
