package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	internalcrypto "github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

type mcpGuardFixture struct {
	configPath    string
	dataPath      string
	cachePath     string
	session       *session.Manager
	authorize     mcpRequestAuthorizer
	authorization *session.MCPAuthorization
}

func newMCPGuardFixture(t *testing.T, timeoutValue string) *mcpGuardFixture {
	t.Helper()
	isolateSessionCache(t)
	configPath, dataPath := newInitializedProject(t, t.TempDir(), "correct-secret")
	timeout, err := session.ParseTimeout(timeoutValue)
	if err != nil {
		t.Fatalf("ParseTimeout: %v", err)
	}
	sessionManager := session.NewManager(configPath, dataPath)
	if err := sessionManager.StartSession("correct-secret", timeout); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	authorization, err := getMCPAuthorization(configPath, dataPath)
	if err != nil {
		t.Fatalf("getMCPAuthorization: %v", err)
	}
	return &mcpGuardFixture{
		configPath:    configPath,
		dataPath:      dataPath,
		cachePath:     filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "senv", fmt.Sprintf("session-%d", os.Getuid())),
		session:       sessionManager,
		authorize:     newMCPRequestAuthorizer(configPath, dataPath, authorization),
		authorization: authorization,
	}
}

func (fixture *mcpGuardFixture) rewriteCache(t *testing.T, mutate func(*session.SessionCache)) {
	t.Helper()
	data, err := os.ReadFile(fixture.cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var cache session.SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	mutate(&cache)
	data, err = json.Marshal(&cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(fixture.cachePath, data, 0o600); err != nil {
		t.Fatalf("rewrite cache: %v", err)
	}
}

func requireMCPRevoked(t *testing.T, fixture *mcpGuardFixture) {
	t.Helper()
	managers, release, err := fixture.authorize()
	if managers != nil || release != nil {
		t.Fatal("revoked request returned managers or release callback")
	}
	if !errors.Is(err, session.ErrMCPRevoked) || err.Error() != session.MCPRevocationMessage {
		t.Fatalf("authorization error = %v, want sanitized revocation", err)
	}
}

func TestMCPRequestSessionGuard(t *testing.T) {
	t.Run("valid restart", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		requestManagers, release, err := fixture.authorize()
		if err != nil {
			t.Fatalf("authorize valid never session: %v", err)
		}
		if requestManagers == nil || requestManagers.env == nil || requestManagers.text == nil || requestManagers.config == nil {
			t.Fatal("valid request did not construct all temporary managers")
		}
		if err := requestManagers.env.Set("default", "VALID", "value"); err != nil {
			t.Fatalf("temporary manager unusable: %v", err)
		}
		release()
		if requestManagers.env != nil || requestManagers.text != nil || requestManagers.config != nil {
			t.Fatal("release retained request-scoped managers")
		}
	})

	t.Run("duration expiry", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "1h")
		fixture.rewriteCache(t, func(cache *session.SessionCache) { cache.ExpiresAt = time.Now().Add(-time.Second) })
		requireMCPRevoked(t, fixture)
	})

	t.Run("clear", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		if err := fixture.session.ClearSession(); err != nil {
			t.Fatalf("ClearSession: %v", err)
		}
		requireMCPRevoked(t, fixture)
	})

	t.Run("session ID replacement", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		timeout, _ := session.ParseTimeout("restart")
		if err := fixture.session.StartSession("correct-secret", timeout); err != nil {
			t.Fatalf("replacement StartSession: %v", err)
		}
		requireMCPRevoked(t, fixture)
	})

	t.Run("boot ID change", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		fixture.rewriteCache(t, func(cache *session.SessionCache) { cache.BootID = "different-boot" })
		requireMCPRevoked(t, fixture)
	})

	t.Run("data path hash change", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		fixture.rewriteCache(t, func(cache *session.SessionCache) { cache.DataPathHash = "different-path" })
		requireMCPRevoked(t, fixture)
	})

	t.Run("metadata salt rekey change", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		store := storage.NewManager(fixture.configPath, fixture.dataPath)
		metadata, err := store.LoadMetadata()
		if err != nil {
			t.Fatal(err)
		}
		salt, err := internalcrypto.GenerateSalt()
		if err != nil {
			t.Fatal(err)
		}
		metadata.Salt = base64.StdEncoding.EncodeToString(salt)
		if err := store.SaveMetadata(metadata); err != nil {
			t.Fatal(err)
		}
		requireMCPRevoked(t, fixture)
	})

	t.Run("cached key replacement", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		fixture.rewriteCache(t, func(cache *session.SessionCache) {
			cache.Key = base64.StdEncoding.EncodeToString(make([]byte, internalcrypto.KeySize))
		})
		requireMCPRevoked(t, fixture)
	})

	// A cache written by an older senv with timeout_type "never" must fail
	// per-request validation: the timeout type no longer exists, so MCP must
	// not honor it even though the cached key itself is untouched.
	t.Run("legacy never timeout type", func(t *testing.T) {
		fixture := newMCPGuardFixture(t, "restart")
		fixture.rewriteCache(t, func(cache *session.SessionCache) {
			cache.TimeoutType = "never"
		})
		requireMCPRevoked(t, fixture)
	})
}
