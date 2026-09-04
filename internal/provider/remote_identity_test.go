package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
)

type maliciousPullServer struct {
	serverAPI
	entries []Entry
	latest  int64
	hook    func() error
}

func (s *maliciousPullServer) Pull(_ context.Context, _ string, _ int64) ([]Entry, int64, error) {
	if s.hook != nil {
		if err := s.hook(); err != nil {
			return nil, 0, err
		}
	}
	return append([]Entry(nil), s.entries...), s.latest, nil
}

func TestRemoteEntryIdentity(t *testing.T) {
	attacks := []struct {
		name  string
		entry Entry
		value string
	}{
		{"traversal", Entry{Kind: KindConfig, Key: "../escaped", Ciphertext: []byte("bad")}, "../escaped"},
		{"absolute", Entry{Kind: KindConfig, Key: "/absolute", Ciphertext: []byte("bad")}, "/absolute"},
		{"windows separator", Entry{Kind: KindConfig, Key: `windows\escape`, Ciphertext: []byte("bad")}, `windows\escape`},
		{"unknown kind", Entry{Kind: "future_kind", Ciphertext: []byte("bad")}, "future_kind"},
	}
	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			server := newFakeServer()
			provider, cache := newTestProvider(t, server)
			beforeState := readTestFile(t, cache.stateFilePath())
			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			provider.api = &maliciousPullServer{
				serverAPI: server,
				latest:    99,
				entries: []Entry{
					{Kind: KindEnv, Grp: "default", Key: "WOULD_APPLY", Ciphertext: []byte("valid"), Revision: 98},
					tt.entry,
				},
			}

			_, err := provider.pull(context.Background())
			if !errors.Is(err, syncschema.ErrInvalidIdentity) {
				t.Fatalf("pull error = %v, want ErrInvalidIdentity", err)
			}
			if strings.Contains(err.Error(), tt.value) {
				t.Fatalf("error reflected attacker identity %q: %v", tt.value, err)
			}
			assertTestFile(t, cache.stateFilePath(), beforeState)
			state, err := cache.loadState()
			if err != nil || state.LastSyncedRevision != 0 {
				t.Fatalf("state after rejected pull = %+v, %v", state, err)
			}
			validPath := mustEntryPath(t, cache, KindEnv, "default", "WOULD_APPLY")
			if _, err := os.Lstat(validPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("valid peer entry was partially applied: %v", err)
			}
			assertTestFile(t, outside, []byte("outside"))
		})
	}

	t.Run("target symlink delete", func(t *testing.T) {
		server := newFakeServer()
		provider, cache := newTestProvider(t, server)
		beforeState := readTestFile(t, cache.stateFilePath())
		outside := filepath.Join(t.TempDir(), "sentinel")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := mustEntryPath(t, cache, KindConfig, "", "linked")
		provider.api = &maliciousPullServer{
			serverAPI: server,
			latest:    7,
			entries:   []Entry{{Kind: KindConfig, Key: "linked", Deleted: true, Revision: 7}},
			hook: func() error {
				return os.Symlink(outside, target)
			},
		}
		_, err := provider.pull(context.Background())
		if !errors.Is(err, securefs.ErrSymlink) {
			t.Fatalf("pull error = %v, want ErrSymlink", err)
		}
		assertTestFile(t, cache.stateFilePath(), beforeState)
		assertTestFile(t, outside, []byte("outside"))
		assertTestSymlink(t, target, outside)
	})

	t.Run("parent symlink delete", func(t *testing.T) {
		server := newFakeServer()
		provider, cache := newTestProvider(t, server)
		beforeState := readTestFile(t, cache.stateFilePath())
		if err := os.Mkdir(filepath.Join(cache.dataPath, "envs"), 0o700); err != nil {
			t.Fatal(err)
		}
		outsideDir := t.TempDir()
		outside := filepath.Join(outsideDir, "KEY.enc")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(cache.dataPath, "envs", "prod")
		provider.api = &maliciousPullServer{
			serverAPI: server,
			latest:    8,
			entries:   []Entry{{Kind: KindEnv, Grp: "prod", Key: "KEY", Deleted: true, Revision: 8}},
			hook: func() error {
				return os.Symlink(outsideDir, parent)
			},
		}
		_, err := provider.pull(context.Background())
		if !errors.Is(err, securefs.ErrSymlink) {
			t.Fatalf("pull error = %v, want ErrSymlink", err)
		}
		assertTestFile(t, cache.stateFilePath(), beforeState)
		assertTestFile(t, outside, []byte("outside"))
		assertTestSymlink(t, parent, outsideDir)
	})
}

func readTestFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return data
}

func assertTestFile(t testing.TB, path string, want []byte) {
	t.Helper()
	got := readTestFile(t, path)
	if string(got) != string(want) {
		t.Fatalf("%s changed: got %q, want %q", path, got, want)
	}
}

func assertTestSymlink(t testing.TB, path, wantTarget string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink", path)
	}
	got, err := os.Readlink(path)
	if err != nil || got != wantTarget {
		t.Fatalf("Readlink(%q) = %q, %v; want %q", path, got, err, wantTarget)
	}
}
