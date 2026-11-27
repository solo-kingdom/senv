package conflict

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
)

func buildTestSkillTar(t *testing.T, files map[string]string, gzipped bool) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Format:   tar.FormatUSTAR,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if !gzipped {
		return tarBuf.Bytes()
	}
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes()
}

func TestRenderSkillDiffAndContentEquivalent(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	rawLocal := buildTestSkillTar(t, map[string]string{
		"SKILL.md": "---\nname: demo\n---\nline1\n",
	}, false)
	gzRemoteSame := buildTestSkillTar(t, map[string]string{
		"SKILL.md": "---\nname: demo\n---\nline1\n",
	}, true)
	gzRemoteDiff := buildTestSkillTar(t, map[string]string{
		"SKILL.md":  "---\nname: demo\n---\nline2\n",
		"extra.txt": "hello\n",
	}, true)

	encLocal, _ := crypto.EncryptWithAAD(key, rawLocal, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))
	encRemoteSame, _ := crypto.EncryptWithAAD(key, gzRemoteSame, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))
	encRemoteDiff, _ := crypto.EncryptWithAAD(key, gzRemoteDiff, crypto.BuildEntryAAD(provider.KindSkill, "base", "demo", 1))
	auth := Auth{Key: key, KeyEpoch: 1, RemoteKeyEpoch: 1, RemoteKeyCompatible: true}

	// 1. 裸 tar 与 tar.gz 内容一致时，RenderDetail 识别等价
	sameDetail := provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "base", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Size: len(encLocal), Ciphertext: []byte(encLocal)},
		Remote: provider.ConflictSide{Revision: 2, Size: len(encRemoteSame), Ciphertext: []byte(encRemoteSame)},
	}
	outSame := RenderDetail(sameDetail, auth, false)
	if !strings.Contains(outSame, "内容完全一致") {
		t.Fatalf("expected content equivalent marker, got:\n%s", outSame)
	}

	// 2. 内容有差异时：reveal=false 仅列变更文件，reveal=true 附文本 diff；未解锁降级
	diffDetail := provider.ConflictDetail{
		Kind: provider.KindSkill, Grp: "base", Key: "demo",
		Local:  provider.ConflictSide{Revision: 1, Size: len(encLocal), Ciphertext: []byte(encLocal)},
		Remote: provider.ConflictSide{Revision: 3, Size: len(encRemoteDiff), Ciphertext: []byte(encRemoteDiff)},
	}
	masked := RenderDetail(diffDetail, auth, false)
	if !strings.Contains(masked, "+ extra.txt") || !strings.Contains(masked, "~ SKILL.md") || strings.Contains(masked, "+ line2") {
		t.Fatalf("reveal=false should show file list without line diff:\n%s", masked)
	}
	revealed := RenderDetail(diffDetail, auth, true)
	if !strings.Contains(revealed, "+ line2") || !strings.Contains(revealed, "- line1") {
		t.Fatalf("reveal=true should show line diff:\n%s", revealed)
	}
	locked := RenderDetail(diffDetail, Auth{}, true)
	if !strings.Contains(locked, "unlock vault to view diff") {
		t.Fatalf("locked auth should degrade safely:\n%s", locked)
	}
}

func TestRenderSkillIndexDiffAndCollision(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	local := []byte(`{"skills":{"butler":{"id":"butler","group":"base","current_version":"v6","versions":[{"version":"v6","sha256":"aaaa11112222"}]}}}`)
	remote := []byte(`{"skills":{"butler":{"id":"butler","group":"base","current_version":"v6","versions":[{"version":"v6","sha256":"bbbb33334444"}]},"hela":{"id":"hela","group":"on-demand","current_version":"v1"}}}`)
	detail := provider.ConflictDetail{
		Kind:   provider.KindSkillIndex,
		Local:  provider.ConflictSide{Revision: 1, Size: len(local), Ciphertext: local},
		Remote: provider.ConflictSide{Revision: 2, Size: len(remote), Ciphertext: remote},
	}
	out := RenderDetail(detail, Auth{Key: key, RemoteKeyCompatible: true}, true)
	if strings.Contains(out, "content unavailable") {
		t.Fatalf("skill_index must not fail decryption:\n%s", out)
	}
	if !strings.Contains(out, "version collision") || !strings.Contains(out, "hela: remote added") {
		t.Fatalf("expected version collision and remote added in skill_index diff:\n%s", out)
	}
}
