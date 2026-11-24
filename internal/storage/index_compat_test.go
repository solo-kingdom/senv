package storage

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func TestParseEncryptedConfigAndSkillIndexCompat(t *testing.T) {
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	cfgIdx := &ConfigIndex{
		Configs: map[string]ConfigFile{
			"app": {Name: "app", EncryptedFile: "app.enc", TargetPath: "/etc/app.conf"},
		},
	}
	rawCfg, _ := json.Marshal(cfgIdx)
	encCfg, err := crypto.EncryptWithAAD(key, rawCfg, crypto.BuildEntryAAD("config_index", "", "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConfigIndexData([]byte(encCfg), nil, 0); !errors.Is(err, ErrEncryptedIndexRequiresKey) {
		t.Fatalf("without key err = %v, want ErrEncryptedIndexRequiresKey", err)
	}
	gotCfg, err := ParseConfigIndexData([]byte(encCfg), key, 0)
	if err != nil || gotCfg.Configs["app"].TargetPath != "/etc/app.conf" {
		t.Fatalf("with key got=(%+v, %v)", gotCfg, err)
	}

	skillIdx := &SkillIndex{
		Skills: map[string]SkillEntry{
			"demo": {ID: "demo", Group: "default", Description: "d"},
		},
	}
	rawSkill, _ := json.Marshal(skillIdx)
	encSkill, err := crypto.EncryptWithAAD(key, rawSkill, crypto.BuildEntryAAD("skill_index", "", "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSkillIndexData([]byte(encSkill), nil, 0); !errors.Is(err, ErrEncryptedIndexRequiresKey) {
		t.Fatalf("skill without key err = %v, want ErrEncryptedIndexRequiresKey", err)
	}
	gotSkill, err := ParseSkillIndexData([]byte(encSkill), key, 0)
	if err != nil || gotSkill.Skills["demo"].Description != "d" {
		t.Fatalf("skill with key got=(%+v, %v)", gotSkill, err)
	}
}
