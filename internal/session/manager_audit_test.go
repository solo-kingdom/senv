package session

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAuditFailureEventsAreSanitized exercises every non-reusable session state
// and asserts the audit trail records the typed event without leaking the
// cached key, the vault salt, or the password.
func TestAuditFailureEventsAreSanitized(t *testing.T) {
	isolateSessionCache(t)
	configPath, dataPath := setupProject(t, "correct-secret")
	sm := sessionManagerForTest(t, configPath, dataPath)
	defer sm.Close()

	duration, _ := ParseTimeout("8h")
	if err := sm.StartSession("correct-secret", duration); err != nil {
		t.Fatalf("start duration session: %v", err)
	}
	cache, err := sm.LoadCache()
	if err != nil || cache == nil {
		t.Fatalf("load cache: cache=%v err=%v", cache, err)
	}

	expired := *cache
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if err := saveCache(vaultSlotFor(dataPath), &expired); err != nil {
		t.Fatalf("save expired cache: %v", err)
	}
	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session error = %v", err)
	}

	restart, _ := ParseTimeout("restart")
	if err := sm.StartSession("correct-secret", restart); err != nil {
		t.Fatalf("start restart session: %v", err)
	}
	originalBootID := systemBootID
	systemBootID = func() (string, error) { return "boot-changed", nil }
	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionInvalidated) {
		systemBootID = originalBootID
		t.Fatalf("invalidated session error = %v", err)
	}

	if err := sm.StartSession("correct-secret", restart); err != nil {
		systemBootID = originalBootID
		t.Fatalf("restart session: %v", err)
	}
	systemBootID = func() (string, error) { return "", errors.New("boot id unavailable") }
	if _, err := sm.GetCachedKey(); !errors.Is(err, ErrSessionUnverifiable) {
		systemBootID = originalBootID
		t.Fatalf("unverifiable session error = %v", err)
	}
	systemBootID = originalBootID

	raw, err := os.ReadFile(AuditLogPath())
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	log := string(raw)
	for _, want := range []string{
		string(AuditSessionExpire),
		string(AuditSessionInvalidated),
		string(AuditSessionUnverifiable),
	} {
		if !strings.Contains(log, want) {
			t.Errorf("audit log missing %q: %s", want, log)
		}
	}
	for name, secret := range map[string]string{
		"cached key": cache.Key,
		"salt":       cache.Salt,
		"password":   "correct-secret",
	} {
		if secret != "" && strings.Contains(log, secret) {
			t.Errorf("audit log leaked %s", name)
		}
	}
}
