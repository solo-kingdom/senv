package provider

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

type rollbackFailureRoot struct {
	securefs.TrustedRoot
	mu     *sync.Mutex
	writes map[string]int
}

func (r *rollbackFailureRoot) AtomicWrite(segments []string, data []byte, mode fs.FileMode) error {
	name := segments[len(segments)-1]
	r.mu.Lock()
	r.writes[name]++
	count := r.writes[name]
	r.mu.Unlock()
	if name == "B.enc" || (name == "A.enc" && count >= 2) {
		return errInjectedProviderAtomic
	}
	return r.TrustedRoot.AtomicWrite(segments, data, mode)
}

func TestRemoteApplyRollbackRestoreFailureDoesNotAdvanceState(t *testing.T) {
	_, cache := newTestProvider(t, newFakeServer())
	writeEnvVar(t, cache, "default", "A", "old-a")
	writeEnvVar(t, cache, "default", "B", "old-b")
	beforeState := &syncState{LastSyncedRevision: 2, Entries: map[string]syncEntryState{}}
	beforeBytes, err := json.MarshalIndent(beforeState, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache.dataPath, syncStateFileName), beforeBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writes := map[string]int{}
	var mu sync.Mutex
	cache.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := securefs.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		return &rollbackFailureRoot{TrustedRoot: root, mu: &mu, writes: writes}, nil
	}
	err = cache.applyRemote([]Entry{
		{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("new-a")},
		{Kind: KindEnv, Grp: "default", Key: "B", Ciphertext: []byte("new-b")},
	}, nil, false, &syncState{LastSyncedRevision: 3, Entries: map[string]syncEntryState{}})
	if !errors.Is(err, errInjectedProviderAtomic) || !strings.Contains(err.Error(), "cache rollback failure") {
		t.Fatalf("apply error = %v, want forward and rollback failure", err)
	}
	assertStateRevision(t, cache, 2)
	assertTestFile(t, filepath.Join(cache.dataPath, "envs", "default", "A.enc"), []byte("new-a"))
}
