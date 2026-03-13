package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/git"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// interactiveSession holds the state for interactive mode
type interactiveSession struct {
	reader         *bufio.Reader
	storageManager *storage.Manager
	envManager     *env.Manager
	configManager  *config.Manager
	gitManager     *git.Manager
	sessionManager *session.Manager
	password       string
}

// prompt reads user input with a prompt
func (is *interactiveSession) prompt(prompt string) string {
	fmt.Print(prompt)
	input, _ := is.reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// promptWithDefault reads user input with a default value
func (is *interactiveSession) promptWithDefault(prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Print(prompt)
	}
	input, _ := is.reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}
