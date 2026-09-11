package cmd

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/session"
)

func TestSessionStartInsecureCacheFlag(t *testing.T) {
	flag := sessionStartCmd.Flags().Lookup("insecure-cache")
	if flag == nil {
		t.Fatal("--insecure-cache flag is not registered")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--insecure-cache default = %q, want false", flag.DefValue)
	}
	if strings.Contains(sessionCmd.Long, "Keychain") || strings.Contains(sessionStartCmd.Long, "Keychain") {
		t.Fatal("session help still mentions Keychain")
	}
	if !strings.Contains(sessionStartCmd.Long, "--insecure-cache") {
		t.Fatal("session start help does not document --insecure-cache")
	}
	if session.InsecureCacheWarning == "" {
		t.Fatal("insecure cache warning is empty")
	}
}

func TestSessionRefreshCommandRegistered(t *testing.T) {
	cmd, _, err := sessionCmd.Find([]string{"refresh"})
	if err != nil || cmd == nil || cmd.Name() != "refresh" {
		t.Fatalf("session refresh is not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("timeout") == nil {
		t.Fatal("session refresh does not expose --timeout")
	}
	if !strings.Contains(cmd.Long, "never prompts for a password") {
		t.Fatal("session refresh help does not state it never prompts")
	}
}

func TestSessionClearAllFlagRegistered(t *testing.T) {
	flag := sessionClearCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatal("session clear does not expose --all")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--all default = %q, want false", flag.DefValue)
	}
}

func TestSessionStatusDocumentsUnverifiable(t *testing.T) {
	if !strings.Contains(sessionStatusCmd.Long, "reason") {
		t.Fatal("session status help does not mention reasons")
	}
}
