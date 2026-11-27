package conflict

import (
	"bytes"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
)

func TestClassifyConflictAndDeleteVsFormatOnly(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	auth := Auth{Key: key, KeyEpoch: 1, RemoteKeyEpoch: 1, RemoteKeyCompatible: true}

	rawTar := buildTestSkillTar(t, map[string]string{"SKILL.md": "---\nname: demo\n---\nhi\n"}, false)
	gzTar := buildTestSkillTar(t, map[string]string{"SKILL.md": "---\nname: demo\n---\nhi\n"}, true)
	gzTarChanged := buildTestSkillTar(t, map[string]string{"SKILL.md": "---\nname: demo\n---\nchanged\n"}, true)

	encRaw, _ := crypto.EncryptWithAAD(key, rawTar, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))
	encGz, _ := crypto.EncryptWithAAD(key, gzTar, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))
	encGzChanged, _ := crypto.EncryptWithAAD(key, gzTarChanged, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))

	// 1. 裸 tar vs tar.gz 等价
	cls := ClassifyConflict(provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "base", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(encRaw)},
		Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(encGz)},
	}, auth)
	if !cls.Equivalent {
		t.Fatalf("expected bare tar and tar.gz to be equivalent: %+v", cls)
	}

	// 2. 内容不同不等价
	clsDiff := ClassifyConflict(provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "base", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(encRaw)},
		Remote: provider.ConflictSide{Revision: 3, Ciphertext: []byte(encGzChanged)},
	}, auth)
	if clsDiff.Equivalent {
		t.Fatalf("expected different skill content not to be equivalent")
	}

	// 3. 本地删除 vs 远端仅格式重压
	delDetail := provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "base", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Deleted: true},
		Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(encGz)},
	}
	if ok, _ := CheckDeleteVsFormatOnly(delDetail, []byte(encRaw), auth); !ok {
		t.Fatal("expected CheckDeleteVsFormatOnly to report true when remote only recompressed")
	}
	delDetailChanged := delDetail
	delDetailChanged.Remote.Ciphertext = []byte(encGzChanged)
	if ok, _ := CheckDeleteVsFormatOnly(delDetailChanged, []byte(encRaw), auth); ok {
		t.Fatal("expected CheckDeleteVsFormatOnly to report false when remote content changed")
	}

	// 4. SSH 私钥永不判定等价
	encSSH1, _ := crypto.EncryptWithAAD(key, []byte("privkey"), crypto.BuildEntryAAD(provider.KindSSHKeypair, "default", "id", 1))
	encSSH2, _ := crypto.EncryptWithAAD(key, []byte("privkey"), crypto.BuildEntryAAD(provider.KindSSHKeypair, "default", "id", 1))
	if ClassifyConflict(provider.ConflictDetail{
		Kind: provider.KindSSHKeypair, Grp: "default", Key: "id",
		Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(encSSH1)},
		Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(encSSH2)},
	}, auth).Equivalent {
		t.Fatal("ssh_keypair must never be auto-classified as equivalent")
	}
}
