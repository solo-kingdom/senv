package session

import (
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingSessionStore struct {
	mu      sync.Mutex
	cache   *SessionCache
	loads   int
	saves   int
	cleared bool
}

func (r *recordingSessionStore) Save(slot string, cache *SessionCache) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	r.cache = cache
	return nil
}

func (r *recordingSessionStore) Load(slot string) (*SessionCache, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads++
	return r.cache, nil
}

func (r *recordingSessionStore) Clear(slot string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleared = true
	r.cache = nil
	return nil
}

func (r *recordingSessionStore) ClearAll() error { return r.Clear("") }

func (r *recordingSessionStore) LoadLegacy() (*SessionCache, error) { return nil, nil }

func (r *recordingSessionStore) ClearLegacy() error { return nil }

func (r *recordingSessionStore) loadCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loads
}

func (r *recordingSessionStore) replace(cache *SessionCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = cache
}

func TestAuthorizeMCPRequestReloadsSessionEachTime(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := setupProject(t, "correct-secret")
	manager := sessionManagerForTest(t, cfg, data)
	timeout, err := ParseTimeout("8h")
	if err != nil {
		t.Fatalf("ParseTimeout: %v", err)
	}
	if err := manager.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	cache, err := loadCache(vaultSlotFor(data))
	if err != nil || cache == nil {
		t.Fatalf("loadCache() = (%v, %v)", cache, err)
	}
	store := &recordingSessionStore{cache: cache}
	setActiveSessionStore(t, store)

	authorization, err := manager.AuthorizeMCPStartup()
	if err != nil {
		t.Fatalf("AuthorizeMCPStartup: %v", err)
	}
	for i := 0; i < 3; i++ {
		key, err := manager.AuthorizeMCPRequest(authorization)
		if err != nil {
			t.Fatalf("AuthorizeMCPRequest #%d: %v", i+1, err)
		}
		if len(key) == 0 {
			t.Fatalf("AuthorizeMCPRequest #%d returned empty key", i+1)
		}
		ZeroKey(key)
	}
	if got := store.loadCount(); got != 4 { // startup + three requests
		t.Fatalf("store loads = %d, want 4 (one per startup/request)", got)
	}

	// A new session instance for the same vault and key must NOT revoke a
	// running MCP server (ADR-0010).
	replaced := *cache
	replaced.SessionID = "sess-replaced"
	replaced.ExpiresAt = cache.ExpiresAt.Add(time.Hour)
	store.replace(&replaced)
	if _, err := manager.AuthorizeMCPRequest(authorization); err != nil {
		t.Fatalf("AuthorizeMCPRequest after new session instance error = %v, want nil", err)
	}

	// A different derived key must revoke it.
	rotated := *cache
	rotated.Key = base64.StdEncoding.EncodeToString(make([]byte, 32))
	store.replace(&rotated)
	if _, err := manager.AuthorizeMCPRequest(authorization); !errors.Is(err, ErrMCPRevoked) {
		t.Fatalf("AuthorizeMCPRequest after key change error = %v, want ErrMCPRevoked", err)
	}
}
