package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestKeychainStoreSaveCommand(t *testing.T) {
	var gotArgs []string
	var gotStdin string
	store := keychainStore{runner: func(args []string, stdin string) (string, error) {
		gotArgs, gotStdin = args, stdin
		return "", nil
	}}
	cache := &SessionCache{SessionID: "sess-keychain"}

	if err := store.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "-i" {
		t.Fatalf("security args = %v, want interactive mode", gotArgs)
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	for _, want := range []string{
		"add-generic-password", "-U", "-s", keychainServiceName(),
		"-a", keychainAccount, "-w", encoded, "-T", keychainTrustedBinary,
	} {
		if !strings.Contains(gotStdin, want) {
			t.Fatalf("security stdin %q missing %q", gotStdin, want)
		}
	}
}

func TestKeychainStoreLoadRoundTrip(t *testing.T) {
	cache := &SessionCache{SessionID: "sess-roundtrip", DataPathHash: "abc123"}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	store := keychainStore{runner: func(args []string, stdin string) (string, error) {
		if len(args) == 0 || args[0] != "find-generic-password" {
			t.Fatalf("security args = %v, want find-generic-password", args)
		}
		return base64.StdEncoding.EncodeToString(data) + "\n", nil
	}}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil || loaded.SessionID != cache.SessionID || loaded.DataPathHash != cache.DataPathHash {
		t.Fatalf("loaded cache = %+v, want %+v", loaded, cache)
	}
}

func TestKeychainStoreLoadMissing(t *testing.T) {
	store := keychainStore{runner: func([]string, string) (string, error) {
		return "", errors.New("security: could not be found")
	}}

	loaded, err := store.Load()
	if err != nil || loaded != nil {
		t.Fatalf("Load() = (%v, %v), want (nil, nil)", loaded, err)
	}
}

func TestKeychainStoreFailureIsActionable(t *testing.T) {
	store := keychainStore{runner: func([]string, string) (string, error) {
		return "", errors.New("security: keychain is locked")
	}}

	err := store.Save(&SessionCache{})
	if !errors.Is(err, ErrNoSecureSessionStore) {
		t.Fatalf("Save() error = %v, want ErrNoSecureSessionStore", err)
	}
	if !strings.Contains(err.Error(), "--insecure-cache") {
		t.Fatalf("Save() error = %v, missing escape-hatch hint", err)
	}
}

func TestKeychainStoreFailureIncludesSecurityStderr(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 45")
	if err := command.Run(); err == nil {
		t.Fatal("expected helper command to fail")
	}
	securityErr := command.ProcessState
	if securityErr == nil {
		t.Fatal("missing helper process state")
	}
	store := keychainStore{runner: func([]string, string) (string, error) {
		return "", &exec.ExitError{ProcessState: securityErr, Stderr: []byte("security: User interaction is not allowed.\n")}
	}}

	_, err := store.Load()
	if !errors.Is(err, ErrNoSecureSessionStore) {
		t.Fatalf("Load() error = %v, want ErrNoSecureSessionStore", err)
	}
	for _, want := range []string{
		"User interaction is not allowed",
		"unlock the login keychain",
		"--insecure-cache",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() error = %q, missing %q", err, want)
		}
	}
}

func TestKeychainStoreClearMissing(t *testing.T) {
	store := keychainStore{runner: func([]string, string) (string, error) {
		return "", errors.New("security: could not be found")
	}}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error = %v, want nil for missing item", err)
	}
}
