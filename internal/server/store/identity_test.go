package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wii/senv/internal/syncschema"
)

// TestPushEntrySSHAssetKinds：SSH 资产 kind 与 client 共享同一白名单（ADR-0020），
// server 侧接受合法档案、拒绝非法身份且不落地部分批次。
func TestPushEntrySSHAssetKinds(t *testing.T) {
	ctx := context.Background()

	t.Run("accepts ssh host and keypair entries", func(t *testing.T) {
		st := newStore(t)
		userID := newUser(t, st, "ssh-kinds-accept")
		seeded, latest, err := st.PushEntries(ctx, userID, "main", []Entry{
			{Kind: syncschema.KindSSHHost, Key: "web-prod", Ciphertext: []byte("host-blob")},
			{Kind: syncschema.KindSSHKeypair, Key: "deploy-key", Ciphertext: []byte("keypair-blob")},
		})
		if err != nil {
			t.Fatalf("PushEntries: %v", err)
		}
		if latest != 2 || len(seeded) != 2 {
			t.Fatalf("latest = %d, seeded = %d, want 2/2", latest, len(seeded))
		}
		entries, _, err := st.PullEntries(ctx, userID, "main", 0)
		if err != nil {
			t.Fatalf("PullEntries: %v", err)
		}
		got := map[string]string{}
		for _, e := range entries {
			got[e.Kind+"/"+e.Key] = string(e.Ciphertext)
		}
		if got["ssh_host/web-prod"] != "host-blob" || got["ssh_keypair/deploy-key"] != "keypair-blob" {
			t.Fatalf("pulled entries = %+v", got)
		}
	})

	t.Run("rejects invalid ssh identity with whole batch", func(t *testing.T) {
		st := newStore(t)
		userID := newUser(t, st, "ssh-kinds-reject")
		_, _, err := st.PushEntries(ctx, userID, "main", []Entry{
			{Kind: syncschema.KindSSHHost, Key: "valid-host", Ciphertext: []byte("ok")},
			{Kind: syncschema.KindSSHKeypair, Grp: "group", Key: "deploy-key", Ciphertext: []byte("invalid")},
		})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("PushEntries error = %v, want ValidationError", err)
		}
		if _, _, err := st.PullEntries(ctx, userID, "main", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("PullEntries after rejection = %v, want ErrNotFound", err)
		}
	})
}

func TestPushEntryIdentity(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid mixed batch does not create vault", func(t *testing.T) {
		st := newStore(t)
		userID := newUser(t, st, "identity-new-vault")
		attack := "../escaped"
		_, _, err := st.PushEntries(ctx, userID, "main", []Entry{
			{Kind: syncschema.KindEnv, Grp: "default", Key: "VALID", Ciphertext: []byte("valid")},
			{Kind: syncschema.KindConfig, Key: attack, Ciphertext: []byte("invalid")},
		})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("PushEntries error = %v, want ValidationError", err)
		}
		if strings.Contains(err.Error(), attack) {
			t.Fatalf("validation error reflected attacker identity: %v", err)
		}
		if _, _, err := st.PullEntries(ctx, userID, "main", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("PullEntries after rejection = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid mixed batch preserves existing revision and entries", func(t *testing.T) {
		st := newStore(t)
		userID := newUser(t, st, "identity-existing-vault")
		seed, latest, err := st.PushEntries(ctx, userID, "main", []Entry{{
			Kind: syncschema.KindEnv, Grp: "default", Key: "BASE", Ciphertext: []byte("old"),
		}})
		if err != nil || latest != 1 || seed[0].Revision != 1 {
			t.Fatalf("seed PushEntries = latest %d, entries %+v, err %v", latest, seed, err)
		}

		_, _, err = st.PushEntries(ctx, userID, "main", []Entry{
			{Kind: syncschema.KindEnv, Grp: "default", Key: "NEW", Ciphertext: []byte("new")},
			{Kind: "unknown_kind", Ciphertext: []byte("invalid")},
		})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("PushEntries error = %v, want ValidationError", err)
		}

		entries, gotLatest, err := st.PullEntries(ctx, userID, "main", 0)
		if err != nil {
			t.Fatalf("PullEntries: %v", err)
		}
		if gotLatest != 1 {
			t.Fatalf("latest revision = %d, want 1", gotLatest)
		}
		if len(entries) != 1 || entries[0].Key != "BASE" || string(entries[0].Ciphertext) != "old" || entries[0].Revision != 1 {
			t.Fatalf("entries changed after rejected batch: %+v", entries)
		}
	})
}
