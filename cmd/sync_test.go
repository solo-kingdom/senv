package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/provider"
)

func TestSyncConflictResolverAvailability(t *testing.T) {
	originalNoInteractive := syncNoInteractive
	originalStdin := stdinIsTerminal
	originalStdout := stdoutIsTerminal
	t.Cleanup(func() {
		syncNoInteractive = originalNoInteractive
		stdinIsTerminal = originalStdin
		stdoutIsTerminal = originalStdout
	})

	cases := []struct {
		name          string
		noInteractive bool
		stdin         bool
		stdout        bool
		want          bool
	}{
		{name: "tty", noInteractive: false, stdin: true, stdout: true, want: true},
		{name: "stdin not tty", noInteractive: false, stdin: false, stdout: true, want: false},
		{name: "stdout not tty", noInteractive: false, stdin: true, stdout: false, want: false},
		{name: "explicit no interactive", noInteractive: true, stdin: true, stdout: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			syncNoInteractive = tc.noInteractive
			stdinIsTerminal = func() bool { return tc.stdin }
			stdoutIsTerminal = func() bool { return tc.stdout }
			if got := syncConflictResolverAvailable(); got != tc.want {
				t.Fatalf("availability = %v, want %v", got, tc.want)
			}
		})
	}

	if flag := syncCmd.Flags().Lookup("no-interactive"); flag == nil {
		t.Fatal("sync command must expose --no-interactive")
	}
}

func TestWriteSyncConflictReport(t *testing.T) {
	updated := time.Date(2026, 9, 4, 2, 15, 0, 0, time.UTC)
	conflict := &provider.SyncConflictError{
		Conflicts: []provider.Conflict{{
			Kind: "config_index", CurrentRevision: 690,
			Deleted: false, Size: 12488, UpdatedAt: updated,
		}},
		MetadataConflict: true,
		Details: []provider.ConflictDetail{{
			Kind: "config_index",
			Local: provider.ConflictSide{
				Revision: 688, Size: 12301, Hash: "localhash000000000",
				Ciphertext: []byte("local-secret"),
			},
			Remote: provider.ConflictSide{
				Revision: 690, Size: 12488, Hash: "remotehash000000000",
				UpdatedAt: updated, Ciphertext: []byte("remote-secret"),
			},
		}},
		Metadata: &provider.MetadataConflictDetail{
			Local: []byte("local-meta-secret"), Remote: []byte("remote-meta-secret"),
		},
	}

	var out bytes.Buffer
	writeSyncConflictReport(&out, conflict)
	got := out.String()
	for _, want := range []string{
		"config_index/-/(index/meta)", "local", "revision=688", "size=12301",
		"hash=localhash0", "remote", "revision=690", "size=12488",
		"hash=remotehash0", "vault metadata", "--accept-remote", "--force-push",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"local-secret", "remote-secret", "local-meta-secret", "remote-meta-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("report leaked %q:\n%s", secret, got)
		}
	}
}
