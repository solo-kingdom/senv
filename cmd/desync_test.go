package cmd

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// desyncMetadata replaces metadata with one derived from a different password,
// while leaving the existing encrypted env files untouched. This mimics a git
// pull that brought a teammate's re-initialized metadata.
func desyncMetadata(t *testing.T, cfg, data string) {
	t.Helper()
	store := storage.NewManager(cfg, data)
	md, err := store.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("other-machine-password", otherSalt)
	pk, err := crypto.Encrypt(otherKey, []byte(crypto.HashPassword("other-machine-password")))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	md.Salt = base64.StdEncoding.EncodeToString(otherSalt)
	md.PasswordKey = pk
	if err := store.SaveMetadata(md); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
}

// startNeverSession starts a never-expiring session for the project password.
func startNeverSession(t *testing.T, cfg, data, password string) {
	t.Helper()
	to, err := session.ParseTimeout("never")
	if err != nil || to == nil {
		t.Fatalf("parse timeout: %v", err)
	}
	sm := session.NewManager(cfg, data)
	if err := sm.StartSession(password, to); err != nil {
		t.Fatalf("start session: %v", err)
	}
}

// writeEnvFileWithPassword creates an env group encrypted with the given
// password, so there is real data the cached key can still decrypt after the
// metadata is desynced.
func writeEnvFileWithPassword(t *testing.T, cfg, data, group, password string) {
	t.Helper()
	store := storage.NewManager(cfg, data)
	grp := storage.NewEnvGroup(group)
	grp.Variables["K"] = "V"
	if err := store.SaveEnvGroup(grp, password); err != nil {
		t.Fatalf("save env group: %v", err)
	}
}

// (a) Desync must surface ErrDataDesync, NOT errInvalidPassword, and must not
// prompt for a password (the cached key is a valid recovery key).
func TestDesync_ReportsErrDataDesyncNotInvalidPassword(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	writeEnvFileWithPassword(t, cfg, data, "default", "correct-secret")
	startNeverSession(t, cfg, data, "correct-secret")

	desyncMetadata(t, cfg, data)

	prompted := false
	fail := func(string) (string, error) {
		prompted = true
		return "", errors.New("should not prompt on desync")
	}
	_, _, _, err := getManagersAt(cfg, data, fail)
	if !errors.Is(err, storage.ErrDataDesync) {
		t.Fatalf("expected ErrDataDesync, got %v", err)
	}
	if errors.Is(err, errInvalidPassword) {
		t.Fatal("must not report invalid password on desync")
	}
	if prompted {
		t.Fatal("must not prompt for password when cache is a valid recovery key")
	}
}

// (c) The stale cache must be preserved across desync diagnosis so it can still
// serve as a recovery key.
func TestDesync_StaleCachePreservedAsRecoveryKey(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")
	writeEnvFileWithPassword(t, cfg, data, "default", "correct-secret")
	startNeverSession(t, cfg, data, "correct-secret")

	desyncMetadata(t, cfg, data)

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("correct-secret"))
	if !errors.Is(err, storage.ErrDataDesync) {
		t.Fatalf("expected ErrDataDesync, got %v", err)
	}

	sm := session.NewManager(cfg, data)
	key, cache, perr := sm.PeekCachedKey()
	if perr != nil || cache == nil || len(key) == 0 {
		t.Fatalf("recovery cache must survive desync diagnosis: err=%v cache=%v", perr, cache)
	}
}

// (b) A plain wrong password on a consistent project must still yield
// errInvalidPassword (the desync path must not false-trigger).
func TestDesync_WrongPasswordStillReportsInvalidPassword(t *testing.T) {
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data := newInitializedProject(t, dir, "correct-secret")

	_, _, _, err := getManagersAt(cfg, data, stubPrompter("definitely-wrong"))
	if !errors.Is(err, errInvalidPassword) {
		t.Fatalf("expected errInvalidPassword, got %v", err)
	}
}
