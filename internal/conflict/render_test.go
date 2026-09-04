package conflict

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func encrypted(t *testing.T, key []byte, value any) []byte {
	t.Helper()
	plaintext, err := storage.ToJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(ciphertext)
}

func TestRenderEnvAndText(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	updated := time.Date(2026, 9, 4, 10, 12, 0, 0, time.UTC)
	localEnv := encrypted(t, key, storage.EnvVarEntry{Value: "local-secret", UpdatedAt: updated})
	remoteEnv := encrypted(t, key, storage.EnvVarEntry{Value: "remote-secret", UpdatedAt: updated})
	detail := provider.ConflictDetail{
		Kind: provider.KindEnv, Grp: "default", Key: "API",
		Local:  provider.ConflictSide{Revision: 1, Size: len(localEnv), Ciphertext: localEnv},
		Remote: provider.ConflictSide{Revision: 2, Size: len(remoteEnv), Ciphertext: remoteEnv},
	}
	auth := Auth{Key: key, RemoteKeyCompatible: true}

	masked := RenderDetail(detail, auth, false)
	if !strings.Contains(masked, "<masked 12 bytes>") || strings.Contains(masked, "local-secret") {
		t.Fatalf("env must be masked by default:\n%s", masked)
	}
	revealed := RenderDetail(detail, auth, true)
	if !strings.Contains(revealed, "local-secret") || !strings.Contains(revealed, "remote-secret") {
		t.Fatalf("revealed env missing values:\n%s", revealed)
	}

	localText := encrypted(t, key, storage.TextEntry{Value: "local text", Size: 10, UpdatedAt: updated})
	remoteText := encrypted(t, key, storage.TextEntry{Value: "remote text", Size: 11, UpdatedAt: updated})
	detail.Kind = provider.KindText
	detail.Local.Ciphertext, detail.Remote.Ciphertext = localText, remoteText
	detail.Local.Size, detail.Remote.Size = len(localText), len(remoteText)
	hidden := RenderDetail(detail, auth, false)
	if strings.Contains(hidden, "local text") || !strings.Contains(hidden, "<text hidden>") {
		t.Fatalf("text leaked without explicit reveal:\n%s", hidden)
	}
	if !strings.Contains(RenderDetail(detail, auth, true), "remote text") {
		t.Fatal("text reveal missing remote content")
	}
}

func TestRenderBinaryConfigAndIncompatibleRemote(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	localPlain := []byte{0xff, 0xfe, 0x00}
	localCipher, err := crypto.Encrypt(key, localPlain)
	if err != nil {
		t.Fatal(err)
	}
	remoteCipher, err := crypto.Encrypt(bytes.Repeat([]byte{3}, 32), []byte("other key"))
	if err != nil {
		t.Fatal(err)
	}
	detail := provider.ConflictDetail{
		Kind: provider.KindConfig, Key: "app",
		Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(localCipher)},
		Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(remoteCipher)},
	}
	got := RenderDetail(detail, Auth{Key: key, RemoteKeyCompatible: true}, true)
	if !strings.Contains(got, "<binary content hidden>") || !strings.Contains(got, "<content unavailable>") {
		t.Fatalf("binary/incompatible render =\n%s", got)
	}
}

func TestRenderConfigIndexDiff(t *testing.T) {
	now := time.Now()
	local := storage.ConfigIndex{Configs: map[string]storage.ConfigFile{
		"app":  {Name: "app", TargetPath: "/local", Group: "default", UpdatedAt: now},
		"gone": {Name: "gone", UpdatedAt: now},
	}}
	remote := storage.ConfigIndex{Configs: map[string]storage.ConfigFile{
		"app": {Name: "app", TargetPath: "/remote", Group: "work", UpdatedAt: now},
		"new": {Name: "new", Group: "work", UpdatedAt: now},
	}}
	localBlob, err := storage.ToJSON(local)
	if err != nil {
		t.Fatal(err)
	}
	remoteBlob, err := storage.ToJSON(remote)
	if err != nil {
		t.Fatal(err)
	}
	got := RenderConfigIndexDiff(localBlob, remoteBlob)
	for _, want := range []string{"app target_path", "/local", "/remote", "app group", "default", "work", "gone: local only", "new: remote added"} {
		if !strings.Contains(got, want) {
			t.Errorf("config index diff missing %q:\n%s", want, got)
		}
	}
}

func TestRenderMetadataSafety(t *testing.T) {
	key := bytes.Repeat([]byte{5}, 32)
	passwordKey, err := crypto.Encrypt(key, []byte("verification"))
	if err != nil {
		t.Fatal(err)
	}
	local := storage.Metadata{Version: "1.0", KDFIterations: 600000, PasswordKey: passwordKey}
	localBlob, err := storage.ToJSON(local)
	if err != nil {
		t.Fatal(err)
	}
	got := RenderMetadata(localBlob, localBlob, NewAuth(key, localBlob))
	if !strings.Contains(got, "remote key compatible: true") || !strings.Contains(got, "raw metadata editing is disabled") {
		t.Fatalf("metadata summary =\n%s", got)
	}
	if strings.Contains(got, passwordKey) || strings.Contains(got, "verification") {
		t.Fatalf("metadata summary leaked key material:\n%s", got)
	}
}
