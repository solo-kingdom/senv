package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func TestHistoryMasksByDefaultAndNeverRevealsSSHPrivateKey(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	envRaw, err := json.Marshal(storage.EnvVarEntry{Value: "super-secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	envCT, err := crypto.Encrypt(key, envRaw)
	if err != nil {
		t.Fatal(err)
	}
	envHV := provider.HistoryVersion{Kind: "env", Grp: "prod", Key: "API_KEY", Ciphertext: []byte(envCT)}

	masked := historyDisplay(envHV, key, false)
	if strings.Contains(masked, "super-secret-value") || !strings.Contains(masked, "********") {
		t.Fatalf("default historyDisplay leaked plaintext: %q", masked)
	}
	revealed := historyDisplay(envHV, key, true)
	if revealed != "super-secret-value" {
		t.Fatalf("revealed historyDisplay = %q, want %q", revealed, "super-secret-value")
	}

	kpRaw, err := json.Marshal(storage.KeyPairEntry{
		Name:        "deploy",
		Group:       "prod",
		Fingerprint: "SHA256:abc",
		PrivateKey:  "-----BEGIN OPENSSH PRIVATE KEY-----\nTOPSECRET\n-----END OPENSSH PRIVATE KEY-----",
	})
	if err != nil {
		t.Fatal(err)
	}
	kpCT, err := crypto.Encrypt(key, kpRaw)
	if err != nil {
		t.Fatal(err)
	}
	kpHV := provider.HistoryVersion{Kind: "ssh_keypair", Grp: "", Key: "deploy", Ciphertext: []byte(kpCT)}
	for _, rev := range []bool{false, true} {
		got := historyDisplay(kpHV, key, rev)
		if strings.Contains(got, "OPENSSH PRIVATE KEY") || strings.Contains(got, "TOPSECRET") {
			t.Fatalf("ssh_keypair historyDisplay(reveal=%v) leaked private key: %q", rev, got)
		}
		if !strings.Contains(got, "SHA256:abc") {
			t.Fatalf("ssh_keypair summary missing fingerprint: %q", got)
		}
	}
}

func TestHistoryCommandHasRevealFlag(t *testing.T) {
	if f := historyCmd.Flags().Lookup("reveal"); f == nil {
		t.Fatal("historyCmd missing --reveal flag")
	}
}
