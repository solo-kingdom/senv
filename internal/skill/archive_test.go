package skill

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSkillMD = "---\nname: demo\ndescription: A synthetic skill: for tests\n---\n\n# Demo\n"

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type rawEntry struct {
	hdr  tar.Header
	body string
}

// buildTar 直接构造任意头的 tar，用于喂入不可信归档。
func buildTar(t *testing.T, entries ...rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := e.hdr
		hdr.Format = tar.FormatUSTAR
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("write header %q: %v", hdr.Name, err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func skillMDEntry() rawEntry {
	return rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "SKILL.md", Mode: 0o644}, body: goodSkillMD}
}

func TestPackRootIsSkillRootAndInspectRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"SKILL.md":         goodSkillMD,
		"scripts/run.sh":   "#!/bin/sh\necho hi\n",
		"refs/notes/a.txt": "a",
	})
	if err := os.Chmod(filepath.Join(src, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive, err := Pack(src)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	// tar 根即 skill 根：没有以源目录名开头的包装层
	tr := tar.NewReader(bytes.NewReader(archive))
	first, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Name, filepath.Base(src)) || strings.HasPrefix(first.Name, "./") || strings.HasPrefix(first.Name, "/") {
		t.Fatalf("first entry %q must be relative to the skill root", first.Name)
	}

	a, err := Validate(archive, "demo")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	modes := map[string]Entry{}
	for _, e := range a.Entries {
		modes[e.Path] = e
	}
	for _, want := range []string{"SKILL.md", "scripts", "scripts/run.sh", "refs", "refs/notes", "refs/notes/a.txt", "empty"} {
		if _, ok := modes[want]; !ok {
			t.Errorf("entry %q missing: %+v", want, a.Entries)
		}
	}
	if modes["scripts/run.sh"].Mode&0o111 == 0 {
		t.Error("executable bit must be preserved")
	}
	if modes["SKILL.md"].Mode&0o111 != 0 {
		t.Error("plain file must not become executable")
	}
	if !modes["empty"].Dir {
		t.Error("empty directory must be preserved")
	}
	if a.FileCount() != 3 {
		t.Errorf("FileCount = %d, want 3", a.FileCount())
	}
}

func TestPackIsDeterministic(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD, "b.txt": "b", "a/c.txt": "c"})
	first, err := Pack(src)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Pack(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Pack output must be deterministic")
	}
}

func TestPackRejectsSymlinkAndSpecialFiles(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD})
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := Pack(src)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("Pack with symlink error = %v", err)
	}
}

func TestPackRejectsDanglingAndDirSymlink(t *testing.T) {
	src := t.TempDir()
	outside := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD})
	if err := os.Symlink(outside, filepath.Join(src, "dir-link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := Pack(src); err == nil {
		t.Fatal("symlink to a directory must be rejected, not followed")
	}
}

func TestPackAcceptsSymlinkedSourceRoot(t *testing.T) {
	real := t.TempDir()
	writeTree(t, real, map[string]string{"SKILL.md": goodSkillMD})
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := Pack(link); err != nil {
		t.Fatalf("a symlinked --source root should be resolved: %v", err)
	}
}

func TestPackRejectsMissingSkillMDAndNonDir(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"README.md": "x"})
	if _, err := Pack(src); err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("missing SKILL.md error = %v", err)
	}
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(file); err == nil {
		t.Fatal("a regular file source must be rejected")
	}
	if _, err := Pack(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing source must be rejected")
	}
}

func TestPackSizeLimit(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD})
	big := bytes.Repeat([]byte("x"), MaxArchiveBytes)
	if err := os.WriteFile(filepath.Join(src, "big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Pack(src)
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("error = %v, want ErrArchiveTooLarge", err)
	}
	if !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("error should name the limit: %v", err)
	}
}

func TestPackNearLimitCountsTarOverhead(t *testing.T) {
	src := t.TempDir()
	// 文件内容本身 < 1MiB，但加 tar 头/填充后 > 1MiB。
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD})
	if err := os.WriteFile(filepath.Join(src, "pad.bin"), bytes.Repeat([]byte("x"), MaxArchiveBytes-len(goodSkillMD)-100), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(src); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("error = %v, want ErrArchiveTooLarge (tar overhead pushes it over)", err)
	}
}

func TestInspectRejectsNonWhitelistedTypes(t *testing.T) {
	for name, typ := range map[string]byte{
		"symlink":   tar.TypeSymlink,
		"hardlink":  tar.TypeLink,
		"fifo":      tar.TypeFifo,
		"chardev":   tar.TypeChar,
		"blockdev":  tar.TypeBlock,
		"contig":    tar.TypeCont,
		"xglobal":   tar.TypeXGlobalHeader,
		"unknown-Z": 'Z',
	} {
		t.Run(name, func(t *testing.T) {
			hdr := tar.Header{Typeflag: typ, Name: "evil", Mode: 0o644}
			if typ == tar.TypeSymlink || typ == tar.TypeLink {
				hdr.Linkname = "SKILL.md"
			}
			var buf bytes.Buffer
			// 手写 512 字节头绕开 Writer 对未知类型的拒绝。
			raw := rawHeader(hdr.Name, typ, hdr.Linkname)
			buf.Write(raw)
			buf.Write(make([]byte, 1024))
			_, err := Inspect(buf.Bytes())
			if err == nil {
				t.Fatalf("type %q must be rejected", string(typ))
			}
		})
	}
}

// rawHeader 生成校验和正确的 ustar 头（可含任意 typeflag）。
func rawHeader(name string, typ byte, linkname string) []byte {
	h := make([]byte, 512)
	copy(h[0:100], name)
	copy(h[100:108], "0000644\x00")
	copy(h[108:116], "0000000\x00")
	copy(h[116:124], "0000000\x00")
	copy(h[124:136], "00000000000\x00")
	copy(h[136:148], "00000000000\x00")
	h[156] = typ
	copy(h[157:257], linkname)
	copy(h[257:263], "ustar\x00")
	copy(h[263:265], "00")
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	var sum int
	for _, b := range h {
		sum += int(b)
	}
	copy(h[148:156], []byte(octal(sum, 6)+"\x00 "))
	return h
}

func octal(v, width int) string {
	s := ""
	for v > 0 {
		s = string(rune('0'+v%8)) + s
		v /= 8
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func TestInspectRejectsPathAttacks(t *testing.T) {
	for name, path := range map[string]string{
		"absolute":      "/etc/cron.d/x",
		"parent":        "../escape",
		"nested parent": "a/../../escape",
		"dot segment":   "./SKILL.md2",
		"inner dot":     "a/./b",
		"empty segment": "a//b",
		"backslash":     `a\b`,
		"drive":         "C:evil",
		"control char":  "a\x01b",
	} {
		t.Run(name, func(t *testing.T) {
			data := buildTar(t, skillMDEntry(), rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: path, Mode: 0o644}, body: "x"})
			if _, err := Inspect(data); err == nil {
				t.Fatalf("path %q must be rejected", path)
			}
		})
	}
	dir := buildTar(t, skillMDEntry(), rawEntry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: "../up/", Mode: 0o755}})
	if _, err := Inspect(dir); err == nil {
		t.Fatal("directory traversal must be rejected")
	}
}

func TestInspectAcceptsRegAAndPlainEntries(t *testing.T) {
	data := buildTar(t,
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeRegA, Name: "SKILL.md", Mode: 0o644}, body: goodSkillMD},
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: "d/", Mode: 0o755}},
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "d/f.txt", Mode: 0o644}, body: "f"},
	)
	if _, err := Validate(data, "demo"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestInspectRejectsDuplicatesAndFileDirConflicts(t *testing.T) {
	reg := func(name string) rawEntry {
		return rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644}, body: "x"}
	}
	dir := func(name string) rawEntry {
		return rawEntry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o755}}
	}
	for name, entries := range map[string][]rawEntry{
		"duplicate file":          {skillMDEntry(), reg("a"), reg("a")},
		"duplicate dir":           {skillMDEntry(), dir("d/"), dir("d/")},
		"file then dir same name": {skillMDEntry(), reg("a"), dir("a/")},
		"dir then file same name": {skillMDEntry(), dir("a/"), reg("a")},
		"file under file":         {skillMDEntry(), reg("a"), reg("a/b")},
		"file after implicit dir": {skillMDEntry(), reg("a/b"), reg("a")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect(buildTar(t, entries...)); err == nil {
				t.Fatal("must be rejected")
			}
		})
	}
	// 隐式父目录之后再出现显式目录条目是合法的
	ok := buildTar(t, skillMDEntry(), reg("a/b"), dir("a/"))
	if _, err := Inspect(ok); err != nil {
		t.Fatalf("explicit dir after implicit parent should be fine: %v", err)
	}
}

func TestInspectRequiresSkillMDAtRoot(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty tar":      buildTar(t),
		"nested only":    buildTar(t, rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "sub/SKILL.md", Mode: 0o644}, body: goodSkillMD}),
		"dir named file": buildTar(t, rawEntry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: "SKILL.md/", Mode: 0o755}}),
		"wrong case":     buildTar(t, rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "skill.md", Mode: 0o644}, body: goodSkillMD}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect(data); err == nil || !strings.Contains(err.Error(), "SKILL.md") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestInspectRejectsOversizedAndTruncated(t *testing.T) {
	if _, err := Inspect(make([]byte, MaxArchiveBytes+1)); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("oversized error = %v", err)
	}
	good := buildTar(t, skillMDEntry())
	if _, err := Inspect(good[:540]); err == nil {
		t.Fatal("truncated archive must be rejected")
	}
	if _, err := Inspect([]byte("not a tar at all, just some text that is long enough to fill a header block....")); err == nil {
		t.Fatal("garbage must be rejected")
	}
}

func TestInspectRejectsPaxExtensions(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Typeflag: tar.TypeReg, Name: "SKILL.md", Mode: 0o644, Size: int64(len(goodSkillMD)), Format: tar.FormatPAX,
		PAXRecords: map[string]string{"SCHILY.xattr.user.k": "v"}}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(goodSkillMD))
	_ = tw.Close()
	if _, err := Inspect(buf.Bytes()); err == nil {
		t.Fatal("pax headers must be rejected")
	}
}

func TestParseName(t *testing.T) {
	ok := map[string]string{
		"plain":           "---\nname: demo\n---\nbody",
		"quoted":          "---\nname: \"demo\"\n---\n",
		"single quoted":   "---\nname: 'demo'\n---\n",
		"crlf":            "---\r\nname: demo\r\ndescription: x\r\n---\r\n",
		"bom":             "\ufeff---\nname: demo\n---\n",
		"colon in desc":   "---\ndescription: use when: a: b\nname: demo\n---\n",
		"trailing spaces": "---  \nname:   demo  \n---\t\n",
	}
	for label, md := range ok {
		t.Run("ok/"+label, func(t *testing.T) {
			got, err := ParseName([]byte(md))
			if err != nil || got != "demo" {
				t.Fatalf("name=%q err=%v", got, err)
			}
		})
	}
	bad := map[string]string{
		"no frontmatter": "# Demo\nname: demo\n",
		"unclosed":       "---\nname: demo\n",
		"no name":        "---\ndescription: x\n---\n",
		"empty name":     "---\nname:\n---\n",
		"indented name":  "---\nmeta:\n  name: demo\n---\n",
		"name in body":   "---\ndescription: x\n---\nname: demo\n",
		"empty file":     "",
	}
	for label, md := range bad {
		t.Run("bad/"+label, func(t *testing.T) {
			if got, err := ParseName([]byte(md)); err == nil {
				t.Fatalf("expected error, got name %q", got)
			}
		})
	}
}

func TestValidateRequiresNameEqualsID(t *testing.T) {
	data := buildTar(t, skillMDEntry())
	if _, err := Validate(data, "demo"); err != nil {
		t.Fatalf("matching id: %v", err)
	}
	_, err := Validate(data, "other")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched id error = %v", err)
	}
}

func TestInspectRejectsGNUFormat(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	long := strings.Repeat("d", 120) + "/f.txt"
	for _, h := range []*tar.Header{
		{Typeflag: tar.TypeReg, Name: "SKILL.md", Mode: 0o644, Size: int64(len(goodSkillMD)), Format: tar.FormatGNU},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(goodSkillMD))
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: long, Mode: 0o644, Size: 1, Format: tar.FormatGNU}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	if _, err := Inspect(buf.Bytes()); err == nil {
		t.Fatal("GNU long-name archives must be rejected")
	}
}

func TestPackLongAndNonASCIIPaths(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD})
	// ustar 用 prefix+name 可容纳 < 256 字节的路径
	deep := strings.Repeat("d", 60) + "/" + strings.Repeat("e", 60) + "/f.txt"
	writeTree(t, src, map[string]string{deep: "x"})
	if _, err := Pack(src); err != nil {
		t.Fatalf("path within ustar prefix limits should pack: %v", err)
	}

	tooLong := t.TempDir()
	writeTree(t, tooLong, map[string]string{"SKILL.md": goodSkillMD, strings.Repeat("x", 120) + "/" + strings.Repeat("y", 120) + "/z.txt": "x"})
	if _, err := Pack(tooLong); err == nil || !strings.Contains(err.Error(), "ustar") {
		t.Fatalf("over-long path error = %v, want ustar message", err)
	}

	unicode := t.TempDir()
	writeTree(t, unicode, map[string]string{"SKILL.md": goodSkillMD, "笔记.md": "x"})
	if _, err := Pack(unicode); err == nil || !strings.Contains(err.Error(), "ustar") {
		t.Fatalf("non-ASCII path error = %v, want ustar message", err)
	}
}

func tarSymlinkHeader(name, target string) tar.Header {
	return tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: target, Mode: 0o777}
}
