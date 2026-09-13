package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Hosts written before the group field existed carry no "group" key; they
// must load as ungrouped and re-save in the legacy shape (group omitted).
func TestHostEntryLegacyJSONWithoutGroup(t *testing.T) {
	mgr, _ := setupTestManager(t)
	legacy := `{"alias":"web","hostname":"10.0.0.1","user":"deploy","port":2222,"identity_key":"web-key","tags":["prod"],"extra":{"ForwardAgent":"yes"},"updated_at":"2024-01-01T00:00:00Z"}`
	var host HostEntry
	if err := json.Unmarshal([]byte(legacy), &host); err != nil {
		t.Fatal(err)
	}
	if host.Group != "" {
		t.Fatalf("legacy host group = %q, want empty", host.Group)
	}
	if host.Alias != "web" || host.Hostname != "10.0.0.1" || host.Port != 2222 || host.IdentityKey != "web-key" || host.Extra["ForwardAgent"] != "yes" {
		t.Fatalf("legacy fields lost: %+v", host)
	}
	if err := mgr.SaveHost("web", &host, "test-password"); err != nil {
		t.Fatal(err)
	}
	loaded, err := mgr.LoadHost("web", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Group != "" {
		t.Fatalf("round-trip group = %q, want empty", loaded.Group)
	}
	raw, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("group")) {
		t.Fatalf("empty group must be omitted, got: %s", raw)
	}
}

// Keypairs written before the group field existed carry no "group" key; they
// must load as ungrouped and re-save in the legacy shape (group omitted).
func TestKeyPairEntryLegacyJSONWithoutGroup(t *testing.T) {
	mgr, _ := setupTestManager(t)
	legacy := `{"name":"web-key","private_key":"PRIVATE","public_key":"ssh-ed25519 AAA","fingerprint":"SHA256:test","comment":"web","imported_at":"2024-01-01T00:00:00Z"}`
	var keyPair KeyPairEntry
	if err := json.Unmarshal([]byte(legacy), &keyPair); err != nil {
		t.Fatal(err)
	}
	if keyPair.Group != "" {
		t.Fatalf("legacy keypair group = %q, want empty", keyPair.Group)
	}
	if keyPair.Name != "web-key" || keyPair.Fingerprint != "SHA256:test" || keyPair.Comment != "web" {
		t.Fatalf("legacy fields lost: %+v", keyPair)
	}
	if err := mgr.SaveKeyPair("web-key", &keyPair, "test-password"); err != nil {
		t.Fatal(err)
	}
	loaded, err := mgr.LoadKeyPair("web-key", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Group != "" {
		t.Fatalf("round-trip group = %q, want empty", loaded.Group)
	}
	raw, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("group")) {
		t.Fatalf("empty group must be omitted, got: %s", raw)
	}
}

func TestSaveAndLoadSSHAssets(t *testing.T) {
	mgr, _ := setupTestManager(t)
	imported := time.Now().Truncate(time.Second).UTC()
	keyPair := &KeyPairEntry{
		Name:        "web-key",
		PrivateKey:  "PRIVATE-KEY-PLAINTEXT",
		PublicKey:   "ssh-ed25519 AAA public",
		Fingerprint: "SHA256:test",
		Comment:     "web",
		ImportedAt:  imported,
	}
	if err := mgr.SaveKeyPair("web-key", keyPair, "test-password"); err != nil {
		t.Fatalf("SaveKeyPair: %v", err)
	}
	host := &HostEntry{
		Alias:       "web",
		Hostname:    "10.0.0.1",
		User:        "deploy",
		Port:        2222,
		IdentityKey: "web-key",
		Extra:       map[string]string{"ForwardAgent": "yes"},
		UpdatedAt:   imported,
	}
	if err := mgr.SaveHost("web", host, "test-password"); err != nil {
		t.Fatalf("SaveHost: %v", err)
	}

	loadedKey, err := mgr.LoadKeyPair("web-key", "test-password")
	if err != nil {
		t.Fatalf("LoadKeyPair: %v", err)
	}
	if *loadedKey != *keyPair {
		t.Fatalf("keypair round trip = %+v, want %+v", loadedKey, keyPair)
	}
	loadedHost, err := mgr.LoadHost("web", "test-password")
	if err != nil {
		t.Fatalf("LoadHost: %v", err)
	}
	if !reflect.DeepEqual(loadedHost, host) {
		t.Fatalf("host round trip = %+v, want %+v", loadedHost, host)
	}

	raw, err := os.ReadFile(mgr.keypairFilePath("web-key"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PRIVATE-KEY-PLAINTEXT")) {
		t.Fatal("private key plaintext leaked to encrypted file")
	}
	if mode := statMode(t, mgr.keypairFilePath("web-key")); mode.Perm() != 0o600 {
		t.Fatalf("keypair mode = %o, want 600", mode.Perm())
	}
	if mode := statMode(t, mgr.hostFilePath("web")); mode.Perm() != 0o600 {
		t.Fatalf("host mode = %o, want 600", mode.Perm())
	}
}

func TestSSHAssetListsAndDeletes(t *testing.T) {
	mgr, _ := setupTestManager(t)
	if names, err := mgr.ListHosts(); err != nil || len(names) != 0 {
		t.Fatalf("empty hosts = %v, %v; want empty", names, err)
	}
	if names, err := mgr.ListKeyPairs(); err != nil || len(names) != 0 {
		t.Fatalf("empty keypairs = %v, %v; want empty", names, err)
	}
	if err := mgr.SaveHost("z-web", &HostEntry{Alias: "z-web", UpdatedAt: time.Unix(0, 0)}, "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveHost("a-web", &HostEntry{Alias: "a-web", UpdatedAt: time.Unix(0, 0)}, "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveKeyPair("z-key", &KeyPairEntry{Name: "z-key", ImportedAt: time.Unix(0, 0)}, "test-password"); err != nil {
		t.Fatal(err)
	}
	hosts, err := mgr.ListHosts()
	if err != nil || len(hosts) != 2 || hosts[0] != "a-web" {
		t.Fatalf("hosts = %v, %v; want sorted [a-web z-web]", hosts, err)
	}
	keyPairs, err := mgr.ListKeyPairs()
	if err != nil || len(keyPairs) != 1 {
		t.Fatalf("keypairs = %v, %v", keyPairs, err)
	}
	if err := mgr.DeleteHost("z-web"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteKeyPair("z-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadHost("z-web", "test-password"); err == nil {
		t.Fatal("deleted host loaded")
	}
	if _, err := mgr.LoadKeyPair("z-key", "test-password"); err == nil {
		t.Fatal("deleted keypair loaded")
	}
}

func TestSSHAssetConcurrentWritesAreSerialized(t *testing.T) {
	mgr, _ := setupTestManager(t)
	const writers = 8
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a' + i))
			errs <- mgr.SaveKeyPair(name, &KeyPairEntry{Name: name, PrivateKey: name, ImportedAt: time.Unix(0, 0)}, "test-password")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}
	names, err := mgr.ListKeyPairs()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != writers {
		t.Fatalf("saved %d keypairs, want %d", len(names), writers)
	}
}

func TestSSHAssetIdentityAndMissingKeyFailClosed(t *testing.T) {
	mgr, _ := setupTestManager(t)
	if err := mgr.SaveHost("../escape", &HostEntry{Alias: "../escape"}, "test-password"); err == nil {
		t.Fatal("invalid host accepted")
	}
	if err := mgr.SaveKeyPair("", &KeyPairEntry{}, "test-password"); err == nil {
		t.Fatal("empty keypair accepted")
	}
	if err := mgr.SaveHost("web", &HostEntry{Alias: "other"}, "test-password"); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	if err := mgr.SaveHost("web", &HostEntry{Alias: "web"}, "wrong-password"); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := mgr.LoadHost("missing", "test-password"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing host error = %v, want os.ErrNotExist", err)
	}
}

func TestCheckConsistencyIncludesSSHAssets(t *testing.T) {
	mgr, _ := setupTestManager(t)
	if err := mgr.SaveHost("web", &HostEntry{Alias: "web"}, "test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveKeyPair("web-key", &KeyPairEntry{Name: "web-key"}, "test-password"); err != nil {
		t.Fatal(err)
	}
	key, err := mgr.deriveKeyFromPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.HostFiles.Total != 1 || report.HostFiles.OK != 1 || report.KeyPairFiles.Total != 1 || report.KeyPairFiles.OK != 1 {
		t.Fatalf("SSH probes = hosts %+v, keypairs %+v", report.HostFiles, report.KeyPairFiles)
	}
	if !report.AllOK() {
		t.Fatal("consistent SSH assets reported failure")
	}
}

func TestHasOrphanedSSHData(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	data := filepath.Join(base, "data")
	mgr := NewManager(config, data)
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveKeyPair("web-key", &KeyPairEntry{Name: "web-key"}, "test-password"); err != nil {
		t.Fatal(err)
	}
	// Simulate a synchronized data directory whose metadata was not restored.
	if err := os.Remove(filepath.Join(config, MetadataFile)); err != nil {
		t.Fatal(err)
	}
	if !NewManager(config, data).HasOrphanedData() {
		t.Fatal("orphaned keypair collection was not detected")
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
