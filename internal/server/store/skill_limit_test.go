package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wii/senv/internal/syncschema"
)

// kind=skill 的密文上限放宽到 1.5MiB，其它 kind 仍是 512KiB。
func TestValidateEntrySkillCiphertextLimit(t *testing.T) {
	cases := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{"skill at 1.5MiB", Entry{Kind: syncschema.KindSkill, Grp: "default", Key: "s", Ciphertext: make([]byte, MaxSkillCiphertext)}, false},
		{"skill over 1.5MiB", Entry{Kind: syncschema.KindSkill, Grp: "default", Key: "s", Ciphertext: make([]byte, MaxSkillCiphertext+1)}, true},
		{"skill above 512KiB", Entry{Kind: syncschema.KindSkill, Grp: "default", Key: "s", Ciphertext: make([]byte, MaxEntryCiphertext+1)}, false},
		{"backup above 512KiB", Entry{Kind: syncschema.KindBackup, Grp: "default", Key: "b", Ciphertext: make([]byte, MaxEntryCiphertext+1)}, true},
		{"config above 512KiB", Entry{Kind: syncschema.KindConfig, Key: "c", Ciphertext: make([]byte, MaxEntryCiphertext+1)}, true},
		{"skill_index above 512KiB", Entry{Kind: syncschema.KindSkillIndex, Ciphertext: make([]byte, MaxEntryCiphertext+1)}, true},
		{"skill_index at 512KiB", Entry{Kind: syncschema.KindSkillIndex, Ciphertext: make([]byte, MaxEntryCiphertext)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEntry(tc.entry)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateEntry error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateEntrySkillLimitMessageNamesLimit(t *testing.T) {
	err := validateEntry(Entry{Kind: syncschema.KindSkill, Grp: "default", Key: "s", Ciphertext: make([]byte, MaxSkillCiphertext+1)})
	if err == nil {
		t.Fatal("oversized skill should be rejected")
	}
	if want := "1572864"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q should name the limit %s", err.Error(), want)
	}
}

// 经真实 store 推送：skill 可超过 512KiB，同批次其它 kind 超限仍整批拒绝。
func TestPushSkillEntryLimitsThroughStore(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	userID := newUser(t, st, "skill-limits")

	big := make([]byte, MaxEntryCiphertext+1024)
	if _, _, err := st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: syncschema.KindSkill, Grp: "default", Key: "big", Ciphertext: big},
		{Kind: syncschema.KindSkillIndex, Ciphertext: []byte(`{"skills":{}}`)},
	}); err != nil {
		t.Fatalf("skill above 512KiB should be accepted: %v", err)
	}

	_, _, err := st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: syncschema.KindBackup, Grp: "default", Key: "big", Ciphertext: big},
	})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("oversized backup error = %v, want ValidationError", err)
	}
	_, _, err = st.PushEntries(ctx, userID, "main", []Entry{
		{Kind: syncschema.KindSkill, Grp: "default", Key: "huge", Ciphertext: make([]byte, MaxSkillCiphertext+1)},
	})
	if !errors.As(err, &verr) {
		t.Fatalf("oversized skill error = %v, want ValidationError", err)
	}
}
