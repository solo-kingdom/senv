package conflict

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
)

// skill 正文是加密 tar：冲突渲染即使 reveal 也不解密展示，只给元数据。
func TestRenderSkillIsMetadataOnly(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	local, _ := crypto.Encrypt(key, []byte("TAR-BODY-LOCAL"))
	remote, _ := crypto.Encrypt(key, []byte("TAR-BODY-REMOTE"))
	detail := provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "default", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Size: len(local), Ciphertext: []byte(local)},
		Remote: provider.ConflictSide{Revision: 2, Size: len(remote), Ciphertext: []byte(remote)},
	}
	auth := Auth{Key: key, RemoteKeyCompatible: true}
	for _, reveal := range []bool{false, true} {
		out := RenderDetail(detail, auth, reveal)
		if strings.Contains(out, "TAR-BODY") {
			t.Fatalf("reveal=%v leaked skill archive content:\n%s", reveal, out)
		}
		if !strings.Contains(out, "metadata only") {
			t.Fatalf("reveal=%v missing metadata-only marker:\n%s", reveal, out)
		}
	}
}

// skill_index 与 config_index 一样是明文 JSON，不能走解密路径。
func TestRenderSkillIndexIsPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	local := []byte(`{"skills":{"a":{"id":"a"}}}`)
	remote := []byte(`{"skills":{"b":{"id":"b"}}}`)
	detail := provider.ConflictDetail{
		Kind:   provider.KindSkillIndex,
		Local:  provider.ConflictSide{Revision: 1, Size: len(local), Ciphertext: local},
		Remote: provider.ConflictSide{Revision: 2, Size: len(remote), Ciphertext: remote},
	}
	out := RenderDetail(detail, Auth{Key: key, RemoteKeyCompatible: true}, true)
	if strings.Contains(out, "content unavailable") {
		t.Fatalf("skill_index must not go through decryption:\n%s", out)
	}
	if !strings.Contains(out, `"a"`) || !strings.Contains(out, `"b"`) {
		t.Fatalf("revealed skill_index should show both sides:\n%s", out)
	}
}
