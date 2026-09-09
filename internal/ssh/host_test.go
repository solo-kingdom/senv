package ssh

import (
	"errors"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestHostValidationBranches(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	host := &storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Port: 22}
	if err := mgr.AddHost(host); err != nil {
		t.Fatalf("valid host: %v", err)
	}
	if err := mgr.AddHost(host); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	cases := []struct {
		name string
		host *storage.HostEntry
		want string
	}{
		{"missing hostname", &storage.HostEntry{Alias: "a"}, "hostname is required"},
		{"bad alias", &storage.HostEntry{Alias: "../a", Hostname: "a"}, `invalid SSH host "../a"`},
		{"bad port", &storage.HostEntry{Alias: "a", Hostname: "a", Port: 70000}, "invalid SSH port"},
		{"blank tag", &storage.HostEntry{Alias: "a", Hostname: "a", Tags: []string{" "}}, "invalid SSH tag"},
		{"blank attr key", &storage.HostEntry{Alias: "a", Hostname: "a", Extra: map[string]string{" ": "x"}}, "invalid SSH attribute key"},
		{"newline attr", &storage.HostEntry{Alias: "a", Hostname: "a", Extra: map[string]string{"X": "a\nb"}}, "value must not contain"},
	}
	for _, tc := range cases {
		if err := mgr.AddHost(tc.host); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "relay", Hostname: "relay"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "through", Hostname: "through", ProxyJump: "relay"}); err != nil {
		t.Fatalf("valid proxy: %v", err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "bad", Hostname: "bad", ProxyJump: "missing"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("dangling proxy error = %v", err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "self", Hostname: "self", ProxyJump: "self"}); err == nil {
		t.Fatal("self proxy accepted")
	}
}

func TestUpdateHostValidatesResultingRecord(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "update@test", "")
	if _, err := mgr.ImportKeyPair("update-key", path, false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "old"}); err != nil {
		t.Fatal(err)
	}
	err := mgr.UpdateHost("web", func(host *storage.HostEntry) error {
		host.IdentityKey = "update-key"
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	host, err := mgr.GetHost("web")
	if err != nil || host.IdentityKey != "update-key" {
		t.Fatalf("updated host = %+v, %v", host, err)
	}
	if err := mgr.UpdateHost("web", func(host *storage.HostEntry) error {
		host.IdentityKey = "missing"
		return nil
	}); err == nil {
		t.Fatal("dangling identity accepted")
	}
}

func TestHostExportGolden(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	t.Setenv("HOME", "/home/test-user")
	path := writePrivateKey(t, t.TempDir(), ed25519Private(t), "golden@test", "")
	if _, err := mgr.ImportKeyPair("golden-key", path, false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{
		Alias:       "jump",
		Hostname:    "jump.example",
		User:        "jump",
		Port:        2222,
		IdentityKey: "golden-key",
		Extra:       map[string]string{"IdentitiesOnly": "yes"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{
		Alias:     "web",
		Hostname:  "10.0.0.1",
		User:      "deploy",
		Port:      22,
		ProxyJump: "jump",
		Extra:     map[string]string{"ForwardAgent": "yes"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := mgr.Export("")
	if err != nil {
		t.Fatal(err)
	}
	want := `Host jump
  HostName jump.example
  User jump
  Port 2222
  IdentityFile /home/test-user/.ssh/senv/golden-key
  IdentitiesOnly yes
Host web
  HostName 10.0.0.1
  User deploy
  Port 22
  ProxyJump jump
  ForwardAgent yes
`
	if got != want {
		t.Fatalf("export mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
	single, err := mgr.Export("web")
	if err != nil || !strings.HasPrefix(single, "Host web\n") || strings.Contains(single, "Host jump\n") {
		t.Fatalf("single export = %q, %v", single, err)
	}
}

func TestExportDanglingProxyFails(t *testing.T) {
	mgr, store := newTestSSHManager(t)
	if err := store.WithVaultMutation(func(locked *storage.Manager) error {
		return locked.SaveHost("web", &storage.HostEntry{Alias: "web", Hostname: "web", ProxyJump: "gone"}, "test-password")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Export(""); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("dangling export error = %v", err)
	}
}
