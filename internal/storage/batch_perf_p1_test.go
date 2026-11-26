package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
)

func TestBatchLoadersSingleLockAndEquivalence(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := mgr.Initialize("pass1234"); err != nil {
		t.Fatal(err)
	}
	key, err := mgr.DeriveKeyFromPassword("pass1234")
	if err != nil {
		t.Fatal(err)
	}

	// Seed 5 entries in each domain.
	for i := 0; i < 5; i++ {
		suffix := string(rune('a' + i))
		if err := mgr.SaveHostWithKey("host-"+suffix, &HostEntry{
			Alias: "host-" + suffix, Hostname: "10.0.0.1", User: "root",
		}, key); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveKeyPairWithKey("kp-"+suffix, &KeyPairEntry{
			Name: "kp-" + suffix, PrivateKey: "priv", PublicKey: "pub", ImportedAt: time.Now().UTC(),
		}, key); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveLLMProviderWithKey("llm-"+suffix, &LLMProviderEntry{
			Alias: "llm-" + suffix, BaseURL: "https://api.example.com/v1",
			CredentialRef: "text:llm-keys/llm-" + suffix, Models: []string{"m1"}, DefaultModel: "m1",
		}, key); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveMCPServerWithKey("mcp-"+suffix, &MCPServerEntry{
			Alias: "mcp-" + suffix, Transport: "stdio", Command: "npx",
		}, key); err != nil {
			t.Fatal(err)
		}
		if err := mgr.SaveLedgerJSON(LedgerDirName, "default", "srv.n"+suffix, map[string]string{"id": suffix}, key); err != nil {
			t.Fatal(err)
		}
	}

	// Verify single-lock acquisition (0 exclusive mutation locks, <=1 read lock).
	vaultLockAcquires.Store(0)
	hosts, err := mgr.LoadAllHostsWithKey(key)
	if err != nil || len(hosts) != 5 {
		t.Fatalf("LoadAllHostsWithKey: len=%d err=%v", len(hosts), err)
	}
	kps, err := mgr.LoadAllKeyPairsWithKey(key)
	if err != nil || len(kps) != 5 {
		t.Fatalf("LoadAllKeyPairsWithKey: len=%d err=%v", len(kps), err)
	}
	llms, err := mgr.LoadAllLLMProvidersWithKey(key)
	if err != nil || len(llms) != 5 {
		t.Fatalf("LoadAllLLMProvidersWithKey: len=%d err=%v", len(llms), err)
	}
	mcps, err := mgr.LoadAllMCPServersWithKey(key)
	if err != nil || len(mcps) != 5 {
		t.Fatalf("LoadAllMCPServersWithKey: len=%d err=%v", len(mcps), err)
	}
	ledgers, err := mgr.LoadLedgerBatch(LedgerDirName, "default", "srv.", key)
	if err != nil || len(ledgers) != 5 {
		t.Fatalf("LoadLedgerBatch: len=%d err=%v", len(ledgers), err)
	}
	if locks := vaultLockAcquires.Load(); locks != 5 {
		t.Fatalf("5 batch calls acquired %d locks, want 5 (1 per batch call)", locks)
	}

	// Verify AEAD round-trip compatibility with package-level functions.
	aead, err := crypto.NewAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	aad := crypto.BuildEntryAAD("env", "default", "K", 0)
	enc, err := aead.EncryptWithAAD([]byte("hello"), aad)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := crypto.DecryptWithAADStrict(key, enc, aad, true)
	if err != nil || string(dec) != "hello" {
		t.Fatalf("AEAD roundtrip mismatch: %q err=%v", dec, err)
	}
}
