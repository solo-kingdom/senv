package cmd

import (
	"bufio"
	"fmt"
	"os"
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

// promptPassword reads a password without echo
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := readPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(password), err
}
