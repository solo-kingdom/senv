package provider

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/storage"
)

var errInjectedProviderAtomic = errors.New("injected provider atomic failure")

type providerFaultStage string

const (
	providerFailWrite     providerFaultStage = "write"
	providerFailFileSync  providerFaultStage = "file-fsync"
	providerFailRename    providerFaultStage = "rename"
	providerFailDirectory providerFaultStage = "directory-fsync"
)

type providerAtomicFault struct {
	mu     sync.Mutex
	stage  providerFaultStage
	target string
	fired  bool
}

func (f *providerAtomicFault) shouldFail(segments []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fired || len(segments) == 0 || segments[len(segments)-1] != f.target {
		return false
	}
	f.fired = true
	return true
}

type providerFaultRoot struct {
	securefs.TrustedRoot
	fault *providerAtomicFault
}

func (r *providerFaultRoot) AtomicWrite(segments []string, data []byte, mode fs.FileMode) error {
	if !r.fault.shouldFail(segments) {
		return r.TrustedRoot.AtomicWrite(segments, data, mode)
	}
	if r.fault.stage == providerFailDirectory {
		if err := r.TrustedRoot.AtomicWrite(segments, data, mode); err != nil {
			return err
		}
	}
	return errInjectedProviderAtomic
}

func injectProviderAtomicFault(cache *localCache, stage providerFaultStage, target string) {
	fault := &providerAtomicFault{stage: stage, target: target}
	cache.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := securefs.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		return &providerFaultRoot{TrustedRoot: root, fault: fault}, nil
	}
}

func TestRemoteApplyAtomicFailure(t *testing.T) {
	for _, stage := range []providerFaultStage{providerFailWrite, providerFailFileSync, providerFailRename, providerFailDirectory} {
		t.Run(string(stage), func(t *testing.T) {
			server := newFakeServer()
			provider, cache := newTestProvider(t, server)
			writeEnvVar(t, cache, "default", "A", "old-cache")
			if _, err := provider.SyncWithReport(context.Background()); err != nil {
				t.Fatalf("seed sync: %v", err)
			}
			if _, _, err := server.Push(context.Background(), "main", []Entry{{
				Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-new"), BaseRevision: 1,
			}}); err != nil {
				t.Fatalf("remote update: %v", err)
			}
			beforeState := readTestFile(t, cache.stateFilePath())
			path := mustEntryPath(t, cache, KindEnv, "default", "A")
			injectProviderAtomicFault(cache, stage, filepath.Base(path))

			_, err := provider.pull(context.Background())
			if !errors.Is(err, errInjectedProviderAtomic) {
				t.Fatalf("pull error = %v, want injected failure", err)
			}
			assertTestFile(t, path, []byte("old-cache"))
			assertTestFile(t, cache.stateFilePath(), beforeState)
			assertStateRevision(t, cache, 1)
		})
	}
}

func TestMetadataApplyAtomicFailure(t *testing.T) {
	for _, stage := range []providerFaultStage{providerFailWrite, providerFailFileSync, providerFailRename, providerFailDirectory} {
		t.Run(string(stage), func(t *testing.T) {
			server := newFakeServer()
			provider, cache := newTestProvider(t, server)
			beforeMetadata := readTestFile(t, cache.metadataPath())
			beforeState := readTestFile(t, cache.stateFilePath())
			server.metadata["main"] = []byte(`{"salt":"remote-new","password_key":"new"}`)
			injectProviderAtomicFault(cache, stage, storage.MetadataFile)

			_, err := provider.pull(context.Background())
			if !errors.Is(err, errInjectedProviderAtomic) {
				t.Fatalf("pull error = %v, want injected failure", err)
			}
			assertTestFile(t, cache.metadataPath(), beforeMetadata)
			assertTestFile(t, cache.stateFilePath(), beforeState)
			assertStateRevision(t, cache, 0)
		})
	}
}

func TestSyncStateAtomicFailure(t *testing.T) {
	for _, stage := range []providerFaultStage{providerFailWrite, providerFailFileSync, providerFailRename, providerFailDirectory} {
		t.Run(string(stage), func(t *testing.T) {
			server := newFakeServer()
			provider, cache := newTestProvider(t, server)
			beforeMetadata := readTestFile(t, cache.metadataPath())
			beforeState := readTestFile(t, cache.stateFilePath())
			if _, _, err := server.Push(context.Background(), "main", []Entry{{
				Kind: KindConfig, Key: "new-config", Ciphertext: []byte("remote"), BaseRevision: 0,
			}}); err != nil {
				t.Fatalf("remote insert: %v", err)
			}
			entryPath := mustEntryPath(t, cache, KindConfig, "", "new-config")
			injectProviderAtomicFault(cache, stage, syncStateFileName)

			_, err := provider.pull(context.Background())
			if !errors.Is(err, errInjectedProviderAtomic) {
				t.Fatalf("pull error = %v, want injected failure", err)
			}
			if _, statErr := os.Lstat(entryPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("new entry remained after failed state commit: %v", statErr)
			}
			assertTestFile(t, cache.metadataPath(), beforeMetadata)
			assertTestFile(t, cache.stateFilePath(), beforeState)
			assertStateRevision(t, cache, 0)
		})
	}
}

func assertStateRevision(t testing.TB, cache *localCache, want int64) {
	t.Helper()
	state, err := cache.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if state.LastSyncedRevision != want {
		t.Fatalf("LastSyncedRevision = %d, want %d", state.LastSyncedRevision, want)
	}
}
