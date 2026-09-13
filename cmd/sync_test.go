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

func TestWriteSyncConflictReportConfigSourceWarning(t *testing.T) {
	conflict := &provider.SyncConflictError{
		Conflicts: []provider.Conflict{
			{Kind: provider.KindLLMProvider, CurrentRevision: 12},
			{Kind: provider.KindEnv, Grp: "default", Key: "A", CurrentRevision: 13},
		},
		Details: []provider.ConflictDetail{
			{
				Kind:   provider.KindLLMProvider,
				Key:    "anthropic",
				Local:  provider.ConflictSide{Revision: 10, Hash: "llmlocal00000000"},
				Remote: provider.ConflictSide{Revision: 12, Hash: "llmremote00000000"},
			},
			{
				Kind:   provider.KindEnv,
				Grp:    "default",
				Key:    "A",
				Local:  provider.ConflictSide{Revision: 11, Hash: "envlocal000000000"},
				Remote: provider.ConflictSide{Revision: 13, Hash: "envremote00000000"},
			},
		},
	}

	var out bytes.Buffer
	writeSyncConflictReport(&out, conflict)
	got := out.String()
	for _, want := range []string{
		"⚠ 配置源 llm_provider anthropic 双端均有修改：local rev 10 / remote rev 12",
		"llm_provider/-/anthropic",
		"env/default/A",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
	// env 冲突不应带配置源对照行
	if strings.Count(got, "双端均有修改") != 1 {
		t.Errorf("config-source warning count = %d, want 1:\n%s", strings.Count(got, "双端均有修改"), got)
	}
}

func TestWriteSyncConflictReportSSHConfigSourceWarning(t *testing.T) {
	conflict := &provider.SyncConflictError{
		Conflicts: []provider.Conflict{
			{Kind: provider.KindSSHHost, CurrentRevision: 6},
			{Kind: provider.KindSSHKeypair, CurrentRevision: 9},
			{Kind: provider.KindEnv, Grp: "default", Key: "A", CurrentRevision: 13},
		},
		Details: []provider.ConflictDetail{
			{
				Kind:   provider.KindSSHHost,
				Key:    "web-prod",
				Local:  provider.ConflictSide{Revision: 5, Hash: "sshhostlocal000001"},
				Remote: provider.ConflictSide{Revision: 6, Hash: "sshhostremote00001"},
			},
			{
				Kind:   provider.KindSSHKeypair,
				Key:    "deploy-key",
				Local:  provider.ConflictSide{Revision: 8, Hash: "sshkeylocal000001"},
				Remote: provider.ConflictSide{Revision: 9, Hash: "sshkeyremote0001"},
			},
		},
	}

	var out bytes.Buffer
	writeSyncConflictReport(&out, conflict)
	got := out.String()
	for _, want := range []string{
		"⚠ 配置源 ssh_host web-prod 双端均有修改：local rev 5 / remote rev 6",
		"⚠ 配置源 ssh_keypair deploy-key 双端均有修改：local rev 8 / remote rev 9",
		"ssh_host/-/web-prod",
		"ssh_keypair/-/deploy-key",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
	// env 冲突不应带配置源对照行
	if n := strings.Count(got, "双端均有修改"); n != 2 {
		t.Errorf("config-source warning count = %d, want 2:\n%s", n, got)
	}
	if got := formatSyncConflictAuditMessage(conflict); got != "同步冲突 3 项（含配置源 2 项）" {
		t.Fatalf("audit message = %q", got)
	}
}

func TestFormatSyncConflictAuditMessage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		conflict *provider.SyncConflictError
		want     string
	}{
		{"plain only", &provider.SyncConflictError{Conflicts: []provider.Conflict{
			{Kind: provider.KindEnv},
			{Kind: provider.KindText},
		}}, "同步冲突 2 项"},
		{"with config source", &provider.SyncConflictError{Conflicts: []provider.Conflict{
			{Kind: provider.KindEnv},
			{Kind: provider.KindMCPServer},
			{Kind: provider.KindLLMProvider},
		}}, "同步冲突 3 项（含配置源 2 项）"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSyncConflictAuditMessage(tc.conflict); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}
