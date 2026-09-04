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
	if !strings.Contains(sessionStartCmd.Long, "--insecure-cache") {
		t.Fatal("session start help does not document --insecure-cache")
	}
	if session.InsecureCacheWarning == "" {
		t.Fatal("insecure cache warning is empty")
	}
}
