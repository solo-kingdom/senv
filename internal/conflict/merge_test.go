package conflict

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func mergeKey() []byte { return bytes.Repeat([]byte{11}, 32) }

func envConflictSide(t *testing.T, key []byte, value string, created time.Time) provider.ConflictSide {
	t.Helper()
	blob, err := storage.ToJSON(storage.EnvVarEntry{Value: value, CreatedAt: created, UpdatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := crypto.Encrypt(key, blob)
	if err != nil {
		t.Fatal(err)
	}
	return provider.ConflictSide{Revision: 1, Ciphertext: []byte(ciphertext)}
}

func TestMergeSessionBufferPermissionsAndEnvFinish(t *testing.T) {
	key := mergeKey()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	detail := provider.ConflictDetail{
		Kind: provider.KindEnv, Grp: "default", Key: "API",
		Local:  envConflictSide(t, key, "local-value", created),
		Remote: envConflictSide(t, key, "remote-value", created),
	}
	session, err := PrepareMergeEditor(detail, key)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer session.Close()

	dirInfo, err := os.Stat(session.Dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(session.Path)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("permissions dir=%v file=%v, want 0700/0600", dirInfo.Mode(), fileInfo.Mode())
	}
	buffer, err := os.ReadFile(session.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<<<<<<< SENV_LOCAL", "local-value", "=======", "remote-value", ">>>>>>> SENV_REMOTE"} {
		if !strings.Contains(string(buffer), want) {
			t.Fatalf("buffer missing %q:\n%s", want, buffer)
		}
	}

	if err := os.WriteFile(session.Path, []byte("merged-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := session.Finish(key)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	plaintext, err := crypto.Decrypt(key, string(ciphertext))
	if err != nil {
		t.Fatal(err)
	}
	var entry storage.EnvVarEntry
	if err := storage.FromJSON(plaintext, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Value != "merged-value" || !entry.CreatedAt.Equal(created) || entry.UpdatedAt.Before(created) {
		t.Fatalf("merged env entry = %+v", entry)
	}
}

func TestMergeSessionTextConfigAndIndex(t *testing.T) {
	key := mergeKey()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	textEntry := func(value string) []byte {
		blob, err := storage.ToJSON(storage.TextEntry{Value: value, CreatedAt: created})
		if err != nil {
			t.Fatal(err)
		}
		ciphertext, err := crypto.Encrypt(key, blob)
		if err != nil {
			t.Fatal(err)
		}
		return []byte(ciphertext)
	}
	textLocal, textRemote := textEntry("local"), textEntry("remote")
	textSession, err := PrepareMergeEditor(provider.ConflictDetail{
		Kind: provider.KindText, Grp: "g", Key: "T",
		Local:  provider.ConflictSide{Ciphertext: textLocal},
		Remote: provider.ConflictSide{Ciphertext: textRemote},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(textSession.Path, []byte("merged text"), 0o600); err != nil {
		t.Fatal(err)
	}
	textCipher, err := textSession.Finish(key)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := crypto.Decrypt(key, string(textCipher))
	var decodedText storage.TextEntry
	_ = storage.FromJSON(plain, &decodedText)
	if decodedText.Value != "merged text" || decodedText.Size != len("merged text") || !decodedText.CreatedAt.Equal(created) {
		t.Fatalf("text entry = %+v", decodedText)
	}
	_ = textSession.Close()

	configLocal, _ := crypto.Encrypt(key, []byte("local-conf"))
	configRemote, _ := crypto.Encrypt(key, []byte("remote-conf"))
	configSession, err := PrepareMergeEditor(provider.ConflictDetail{
		Kind: provider.KindConfig, Key: "app",
		Local:  provider.ConflictSide{Ciphertext: []byte(configLocal)},
		Remote: provider.ConflictSide{Ciphertext: []byte(configRemote)},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configSession.Path, []byte("merged-conf"), 0o600); err != nil {
		t.Fatal(err)
	}
	configCipher, err := configSession.Finish(key)
	if err != nil {
		t.Fatal(err)
	}
	if plain, _ := crypto.Decrypt(key, string(configCipher)); string(plain) != "merged-conf" {
		t.Fatalf("config merge = %q", plain)
	}
	_ = configSession.Close()

	indexLocal := storage.NewConfigIndex()
	indexLocal.Configs["app"] = storage.ConfigFile{Name: "app", Group: "default"}
	indexRemote := storage.NewConfigIndex()
	indexRemote.Configs["app"] = storage.ConfigFile{Name: "app", Group: "work"}
	localBlob, _ := storage.ToJSON(indexLocal)
	remoteBlob, _ := storage.ToJSON(indexRemote)
	indexSession, err := PrepareMergeEditor(provider.ConflictDetail{
		Kind:   provider.KindConfigIndex,
		Local:  provider.ConflictSide{Ciphertext: localBlob},
		Remote: provider.ConflictSide{Ciphertext: remoteBlob},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	mergedIndex := storage.NewConfigIndex()
	mergedIndex.Configs["app"] = storage.ConfigFile{Name: "app", Group: "default"}
	mergedBlob, _ := storage.ToJSON(mergedIndex)
	if err := os.WriteFile(indexSession.Path, mergedBlob, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := indexSession.Finish(key)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip storage.ConfigIndex
	if err := storage.FromJSON(got, &roundTrip); err != nil || roundTrip.Configs["app"].Group != "default" {
		t.Fatalf("config index merge got=%s err=%v", got, err)
	}
	_ = indexSession.Close()
}

func TestValidateMergedBufferRejectsUnsafeContent(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{name: "markers", data: []byte("<<<<<<< SENV_LOCAL\nx\n=======\ny\n>>>>>>> SENV_REMOTE\n")},
		{name: "invalid json", data: []byte("{")},
		{name: "binary", data: []byte{0xff}},
		{name: "oversize", data: bytes.Repeat([]byte("x"), storage.MaxTextSize+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind := provider.KindEnv
			if tc.name == "invalid json" {
				kind = provider.KindConfigIndex
			}
			if tc.name == "binary" || tc.name == "oversize" {
				tc.data = bytes.ToValidUTF8(tc.data, []byte("x"))
				if tc.name == "binary" {
					tc.data = []byte{0xff}
				}
			}
			if err := ValidateMergedBuffer(kind, tc.data); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestMergeSessionEditorAndCleanupFailure(t *testing.T) {
	key := mergeKey()
	created := time.Now()
	session, err := PrepareMergeEditor(provider.ConflictDetail{
		Kind: provider.KindEnv, Grp: "default", Key: "A",
		Local:  envConflictSide(t, key, "local", created),
		Remote: envConflictSide(t, key, "remote", created),
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "true")
	session.editor = "true"
	if err := session.RunEditor(); err != nil {
		t.Fatalf("successful editor run: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(session.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("merge directory survived close: %v", err)
	}

	session.editor = "false"
	if err := session.RunEditor(); err == nil {
		t.Fatal("failed editor exit must return error")
	}
	originalRemove := removeAll
	removeAll = func(string) error { return errors.New("cleanup denied") }
	t.Cleanup(func() { removeAll = originalRemove })
	if err := session.Close(); err == nil || !strings.Contains(err.Error(), "cleanup denied") {
		t.Fatalf("cleanup failure = %v, want explicit error", err)
	}
}
