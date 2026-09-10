package storage

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func TestValidateLLMProviderURL(t *testing.T) {
	tests := []struct {
		raw       string
		allowHTTP bool
		wantErr   string
	}{
		{raw: "https://api.example.com/v1"},
		{raw: "http://127.0.0.1:11434/v1", allowHTTP: true},
		{raw: "http://api.example.com", wantErr: "HTTPS"},
		{raw: "https://user:pass@example.com", wantErr: "userinfo"},
		{raw: "http://user:pass@127.0.0.1", allowHTTP: true, wantErr: "userinfo"},
		{raw: "https:///only-path", wantErr: "host"},
		{raw: "ftp://example.com", wantErr: "http(s)"},
		{raw: "example.com", wantErr: "http(s)"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			err := ValidateLLMProviderURL(tt.raw, tt.allowHTTP)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateLLMProviderURL() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateLLMProviderURL() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRekeyMigratesLLMProviders(t *testing.T) {
	mgr, _ := setupTestManager(t)
	oldKey := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveLLMProviderWithKey("main", validProviderEntry("main"), oldKey); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKeyWithIterations("new-password", newSalt, crypto.DefaultIterations)
	newHash := crypto.HashPassword("new-password")
	newPasswordKey, _ := crypto.Encrypt(newKey, []byte(newHash))
	result, err := mgr.Rekey(oldKey, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, crypto.DefaultIterations)
	if err != nil {
		t.Fatalf("Rekey() error = %v", err)
	}
	if result.ProviderFiles != 1 {
		t.Fatalf("ProviderFiles = %d, want 1", result.ProviderFiles)
	}
	if _, err := mgr.LoadLLMProviderWithKey("main", oldKey); err == nil {
		t.Fatal("old key unexpectedly decrypts provider")
	}
	if _, err := mgr.LoadLLMProviderWithKey("main", newKey); err != nil {
		t.Fatalf("new key cannot decrypt provider: %v", err)
	}
}

func TestHasOrphanedLLMProviders(t *testing.T) {
	mgr, base := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveLLMProviderWithKey("main", validProviderEntry("main"), key); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if err := os.Remove(filepath.Join(base, "config", MetadataFile)); err != nil {
		t.Fatalf("remove metadata: %v", err)
	}
	if !NewManager(filepath.Join(base, "config"), filepath.Join(base, "data")).HasOrphanedData() {
		t.Fatal("HasOrphanedData() = false, want true for provider ciphertext")
	}
}

func TestCheckConsistencyReportsBadLLMProviderCiphertext(t *testing.T) {
	mgr, base := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveLLMProviderWithKey("main", validProviderEntry("main"), key); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	bad := filepath.Join(base, "data", LLMProviderDirName, "main"+ConfigFileSuffix)
	if err := os.WriteFile(bad, []byte("not ciphertext"), 0o600); err != nil {
		t.Fatalf("write bad ciphertext: %v", err)
	}
	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatalf("CheckConsistency() error = %v", err)
	}
	if report.ProviderFiles.Total != 1 || report.ProviderFiles.OK != 0 || len(report.ProviderFiles.Failed) != 1 {
		t.Fatalf("provider probes = %+v", report.ProviderFiles)
	}
	if report.AllOK() {
		t.Fatal("AllOK() = true for bad provider ciphertext")
	}
}

func TestLoadLLMProviderValidatesDecryptedFields(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	raw, err := json.Marshal(map[string]any{
		"alias": "bad", "base_url": "", "credential_ref": "text:llm-keys/bad",
		"models": []string{"m1"}, "default_model": "missing",
	})
	if err != nil {
		t.Fatalf("marshal bad entry: %v", err)
	}
	if err := mgr.saveSSHEntry(LLMProviderDirName, "bad", json.RawMessage(raw), key); err != nil {
		t.Fatalf("write raw encrypted provider: %v", err)
	}
	_, err = mgr.LoadLLMProviderWithKey("bad", key)
	if err == nil {
		t.Fatal("LoadLLMProviderWithKey() unexpectedly accepted malformed decrypted profile")
	}
	if !strings.Contains(err.Error(), "invalid provider") {
		t.Fatalf("error = %v, want validation failure", err)
	}
}

func TestValidateNameRejectsControlCharacters(t *testing.T) {
	for _, name := range []string{"a\nb", "a\rb", "a\tb", "a\x00b", "a\x07b", "a\x7fb"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("ValidateName(%q) unexpectedly succeeded", name)
		}
	}
	if err := ValidateName("ok-alias_1"); err != nil {
		t.Fatalf("ValidateName(ok-alias_1) error = %v", err)
	}
}
