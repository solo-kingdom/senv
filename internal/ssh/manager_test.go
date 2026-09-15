package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/wii/senv/internal/storage"
)

func newTestSSHManager(t *testing.T) (*Manager, *storage.Manager) {
	t.Helper()
	base := t.TempDir()
	store := storage.NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return NewManager(store, "test-password"), store
}

func writePrivateKey(t *testing.T, dir string, private any, comment string, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(private, comment)
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, comment, []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	path := filepath.Join(dir, strings.ReplaceAll(comment, "@", "-at-"))
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ed25519Private(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return private
}

func TestImportDerivesEd25519AndRSAPublicKeys(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	dir := t.TempDir()
	edPath := writePrivateKey(t, dir, ed25519Private(t), "ed@test", "")
	summary, err := mgr.ImportKeyPair("ed-key", edPath, false)
	if err != nil {
		t.Fatalf("ed25519 import: %v", err)
	}
	if summary.PublicKey == "" || !strings.HasPrefix(summary.PublicKey, "ssh-ed25519 ") || !strings.HasPrefix(summary.Fingerprint, "SHA256:") {
		t.Fatalf("ed25519 summary = %+v", summary)
	}
	if summary.Comment != "ed@test" {
		t.Fatalf("ed25519 comment = %q, want ed@test", summary.Comment)
	}
	if !strings.HasSuffix(summary.PublicKey, " ed@test") {
		t.Fatalf("public key should append comment: %q", summary.PublicKey)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPath := writePrivateKey(t, dir, rsaKey, "rsa@test", "")
	summary, err = mgr.ImportKeyPair("rsa-key", rsaPath, false)
	if err != nil {
		t.Fatalf("rsa import: %v", err)
	}
	if summary.PublicKey == "" || !strings.HasPrefix(summary.PublicKey, "ssh-rsa ") {
		t.Fatalf("rsa summary = %+v", summary)
	}
	if summary.Comment != "rsa@test" {
		t.Fatalf("rsa comment = %q, want rsa@test", summary.Comment)
	}
}

func TestKeyPairSummaryReDerivesLegacyComment(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "user@example.com", "")
	if _, err := mgr.ImportKeyPair("legacy", path, false); err != nil {
		t.Fatal(err)
	}
	// Simulate the old bug: Comment stored as key algorithm.
	if err := mgr.UpdateKeyPair("legacy", func(entry *storage.KeyPairEntry) error {
		entry.Comment = "ssh-ed25519"
		entry.PublicKey = strings.Fields(entry.PublicKey)[0] + " " + strings.Fields(entry.PublicKey)[1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := mgr.GetKeyPairSummary("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Comment != "user@example.com" {
		t.Fatalf("re-derived comment = %q, want user@example.com", summary.Comment)
	}
	if !strings.HasSuffix(summary.PublicKey, " user@example.com") {
		t.Fatalf("re-derived public key missing comment: %q", summary.PublicKey)
	}
}

func TestImportEncryptedPrivateKeyWithoutPublicKey(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "encrypted@test", "passphrase")
	summary, err := mgr.ImportKeyPair("encrypted-key", path, false)
	if err != nil {
		t.Fatalf("encrypted import: %v", err)
	}
	if summary.PublicKey != "" || summary.Fingerprint != "" || summary.HasPubKey {
		t.Fatalf("encrypted key unexpectedly derived public material: %+v", summary)
	}
}

func TestImportValidatesFileAndDuplicate(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	if _, err := mgr.ImportKeyPair("missing", filepath.Join(t.TempDir(), "missing"), false); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid")
	if err := os.WriteFile(invalid, []byte("not a private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ImportKeyPair("invalid", invalid, false); err == nil {
		t.Fatal("invalid key accepted")
	}
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "dup@test", "")
	if _, err := mgr.ImportKeyPair("dup", path, false); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ImportKeyPair("dup", path, false); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate error = %v, want ErrExists", err)
	}
	if _, err := mgr.ImportKeyPair("dup", path, true); err != nil {
		t.Fatalf("force import: %v", err)
	}
}

func TestKeyPairGroupImportAndUpdate(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "group@test", "")
	summary, err := mgr.ImportKeyPairWithGroup("group-key", path, "prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Group != "prod" {
		t.Fatalf("summary group = %q, want prod", summary.Group)
	}
	summaries, err := mgr.ListKeyPairs()
	if err != nil || len(summaries) != 1 || summaries[0].Group != "prod" {
		t.Fatalf("list summaries = %v, %v", summaries, err)
	}

	// UpdateKeyPair 修改 group；空值回退未分组。
	if err := mgr.UpdateKeyPair("group-key", func(entry *storage.KeyPairEntry) error {
		entry.Group = "staging"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if summary, err := mgr.GetKeyPairSummary("group-key"); err != nil || summary.Group != "staging" {
		t.Fatalf("updated group = %+v, %v", summary, err)
	}
	if err := mgr.UpdateKeyPair("group-key", func(entry *storage.KeyPairEntry) error {
		entry.Group = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if summary, err := mgr.GetKeyPairSummary("group-key"); err != nil || summary.Group != "" {
		t.Fatalf("cleared group = %+v, %v", summary, err)
	}

	// 改名被禁止；含行分隔符的 group 被拒绝。
	if err := mgr.UpdateKeyPair("group-key", func(entry *storage.KeyPairEntry) error {
		entry.Name = "other"
		return nil
	}); err == nil || !strings.Contains(err.Error(), "cannot be renamed") {
		t.Fatalf("rename error = %v", err)
	}
	if _, err := mgr.ImportKeyPairWithGroup("bad", path, "a\nb", false); err == nil || !strings.Contains(err.Error(), "group must not contain") {
		t.Fatalf("newline group error = %v", err)
	}
	if err := mgr.UpdateKeyPair("group-key", func(entry *storage.KeyPairEntry) error {
		entry.Group = "x\x00y"
		return nil
	}); err == nil || !strings.Contains(err.Error(), "group must not contain") {
		t.Fatalf("NUL group error = %v", err)
	}
	if err := mgr.UpdateKeyPair("missing", func(entry *storage.KeyPairEntry) error { return nil }); err == nil {
		t.Fatal("update of missing keypair accepted")
	}
}

func TestKeyPairSummariesAndDeleteProtection(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "delete@test", "")
	if _, err := mgr.ImportKeyPair("delete-key", path, false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web", IdentityKey: "delete-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.DeleteKeyPair("delete-key", false); err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("protected delete error = %v", err)
	}
	references, err := mgr.DeleteKeyPair("delete-key", true)
	if err != nil || len(references) != 1 || references[0] != "web" {
		t.Fatalf("force delete = %v, %v", references, err)
	}
	host, err := mgr.GetHost("web")
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityKey != "" {
		t.Fatalf("reference = %q, want empty", host.IdentityKey)
	}
	summaries, err := mgr.ListKeyPairs()
	if err != nil || len(summaries) != 0 {
		t.Fatalf("summaries after delete = %+v, %v", summaries, err)
	}
}

func TestMaterializeCreatesPrivatePermissions(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "material@test", "")
	if _, err := mgr.ImportKeyPair("material-key", path, false); err != nil {
		t.Fatal(err)
	}
	target, err := mgr.Materialize("material-key", false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("modes = file %o, dir %o", info.Mode().Perm(), dirInfo.Mode().Perm())
	}
	if _, err := mgr.Materialize("material-key", false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite error = %v", err)
	}
	if _, err := mgr.Materialize("material-key", true); err != nil {
		t.Fatalf("force materialize: %v", err)
	}
}

func TestImportedPrivateKeyRemainsEncrypted(t *testing.T) {
	mgr, store := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "secret@test", "")
	if _, err := mgr.ImportKeyPair("secret-key", path, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.GetDataPath() + "/keypairs/secret-key.enc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "OPENSSH PRIVATE KEY") {
		t.Fatal("raw private key marker leaked into vault file")
	}
	if _, err := x509.ParsePKCS8PrivateKey(raw); err == nil {
		t.Fatal("vault file unexpectedly parsed as plaintext key")
	}
}
