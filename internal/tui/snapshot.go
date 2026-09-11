package tui

import (
	"fmt"
	"sync"

	"github.com/wii/senv/internal/env"
)

// envSnap is one in-memory vault view shared by env Tab, search, deref and
// AI credential-ref collection. Values are plaintext of env vars only.
type envSnap struct {
	Vars   map[string]map[string]string
	Groups []env.GroupInfo
}

// snapshotRegistry holds the latest env snapshot, rebuilt under a mutex
// (single-flight) and atomically replaced. A nil registry falls back to a
// direct Manager.Snapshot call so tests that skip New() still work.
type snapshotRegistry struct {
	mu   sync.Mutex
	env  *env.Manager
	snap *envSnap
}

func newSnapshotRegistry(envMgr *env.Manager) *snapshotRegistry {
	if envMgr == nil {
		return nil
	}
	return &snapshotRegistry{env: envMgr}
}

func (r *snapshotRegistry) Get() (*envSnap, error) {
	if r == nil || r.env == nil {
		return nil, fmt.Errorf("env manager unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snap != nil {
		return r.snap, nil
	}
	vars, groups, err := r.env.Snapshot()
	if err != nil {
		return nil, err
	}
	r.snap = &envSnap{Vars: vars, Groups: groups}
	return r.snap, nil
}

func (r *snapshotRegistry) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.snap = nil
	r.mu.Unlock()
}

func envSnapshot(mgr Managers) (map[string]map[string]string, []env.GroupInfo, error) {
	if mgr.snap != nil {
		snap, err := mgr.snap.Get()
		if err != nil {
			return nil, nil, err
		}
		return snap.Vars, snap.Groups, nil
	}
	if mgr.Env == nil {
		return nil, nil, fmt.Errorf("env manager unavailable")
	}
	return mgr.Env.Snapshot()
}
