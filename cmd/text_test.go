package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newTextTestProject(t *testing.T) {
	t.Helper()
	newAuditTestProject(t)
}

func TestTextImportFlow(t *testing.T) {
	newTextTestProject(t)
	dir := t.TempDir()
	mgr, err := getTextManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddGroup("notes", "test"); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "README.md")
	content := []byte("# hello\nmulti-line import\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	textImportFile = src
	t.Cleanup(func() { textImportFile = "" })
	if err := textImportCmd.RunE(&cobra.Command{}, []string{"notes:README"}); err != nil {
		t.Fatalf("text import: %v", err)
	}

	// 新键创建：vault 值与文件逐字节一致。
	value, err := mgr.Get("notes", "README")
	if err != nil || value != string(content) {
		t.Fatalf("imported value = %q, %v", value, err)
	}

	// 源文件内容不变。
	data, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("source file changed: %q, %v", data, err)
	}

	// 审计 detail 含源路径。
	if log := readAuditLogForTest(t); !strings.Contains(log, "import "+src) {
		t.Fatalf("audit missing import detail: %s", log)
	}

	// g:k 地址优先于 -g。
	textGroup = "elsewhere"
	t.Cleanup(func() { textGroup = "default" })
	src2 := filepath.Join(dir, "ADDR.txt")
	if err := os.WriteFile(src2, []byte("addr"), 0o644); err != nil {
		t.Fatal(err)
	}
	textImportFile = src2
	if err := textImportCmd.RunE(&cobra.Command{}, []string{"notes:ADDR"}); err != nil {
		t.Fatalf("text import with flag group: %v", err)
	}
	textGroup = "default"
	if _, err := mgr.Get("notes", "ADDR"); err != nil {
		t.Fatalf("address group should win over -g: %v", err)
	}
	if _, err := mgr.Get("elsewhere", "ADDR"); err == nil {
		t.Fatal("entry must not land in the -g group when address has a group")
	}

	// 已存在 key 覆盖且 updated_at 刷新。
	infos, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	var firstUpdated time.Time
	for _, info := range infos {
		if info.Key == "README" {
			firstUpdated = info.UpdatedAt
		}
	}
	if firstUpdated.IsZero() {
		t.Fatalf("README missing from list: %v", infos)
	}
	updated := []byte("replaced content\n")
	if err := os.WriteFile(src, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	textImportFile = src
	if err := textImportCmd.RunE(&cobra.Command{}, []string{"notes:README"}); err != nil {
		t.Fatalf("text import overwrite: %v", err)
	}
	value, err = mgr.Get("notes", "README")
	if err != nil || value != string(updated) {
		t.Fatalf("overwritten value = %q, %v", value, err)
	}
	infos, err = mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Key == "README" {
			if !info.UpdatedAt.After(firstUpdated) {
				t.Fatalf("updated_at not refreshed: before=%v after=%v", firstUpdated, info.UpdatedAt)
			}
		}
	}

	// 缺失文件：报错且零副作用（notes 键集合不变）。
	before, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	textImportFile = filepath.Join(dir, "missing.md")
	if err := textImportCmd.RunE(&cobra.Command{}, []string{"notes:GHOST"}); err == nil {
		t.Fatal("import of a missing file should fail")
	}
	after, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("vault side effects: before=%v after=%v", before, after)
	}

	// 缺 --file：参数错误。
	textImportFile = ""
	if err := textImportCmd.RunE(&cobra.Command{}, []string{"notes:README"}); err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("missing --file error = %v", err)
	}
}

func TestTextExportFlow(t *testing.T) {
	newTextTestProject(t)
	dir := t.TempDir()
	mgr, err := getTextManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddGroup("secrets", "test"); err != nil {
		t.Fatal(err)
	}

	secret := []byte("PRIVATE-KEY-MATERIAL\n")
	if err := mgr.Set("secrets", "KEY", string(secret)); err != nil {
		t.Fatal(err)
	}

	// 0600 导出，内容逐字节一致，stdout 只提示路径不回显明文。
	out := filepath.Join(dir, "key.pem")
	textExportPath = out
	t.Cleanup(func() { textExportPath = "" })
	stdout := captureStdout(t, func() {
		if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err != nil {
			t.Fatalf("text export: %v", err)
		}
	})
	if strings.Contains(stdout, string(secret)) {
		t.Fatalf("stdout leaks plaintext: %q", stdout)
	}
	if !strings.Contains(stdout, out) {
		t.Fatalf("stdout should mention the path: %q", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(data, secret) {
		t.Fatalf("exported content = %q, %v", data, err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("exported mode = %o, %v", info.Mode().Perm(), err)
	}

	// 覆盖既有宽松文件后收紧为 0600。
	if err := os.WriteFile(out, []byte("loose"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err != nil {
		t.Fatalf("text export overwrite: %v", err)
	}
	info, err = os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("tightened mode = %o, %v", info.Mode().Perm(), err)
	}
	data, err = os.ReadFile(out)
	if err != nil || !bytes.Equal(data, secret) {
		t.Fatalf("overwritten content = %q, %v", data, err)
	}

	// 值含 {{env:...}} 引用时逐字节原样落盘（不经引用解析）。
	raw := []byte("token={{env:secrets:API_KEY}}\n")
	if err := mgr.Set("secrets", "TEMPLATE", string(raw)); err != nil {
		t.Fatal(err)
	}
	tplOut := filepath.Join(dir, "tpl.txt")
	textExportPath = tplOut
	if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:TEMPLATE"}); err != nil {
		t.Fatalf("text export template: %v", err)
	}
	data, err = os.ReadFile(tplOut)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatalf("template export must be byte-exact without ref resolution: %q, %v", data, err)
	}

	// key 不存在：报错且不创建文件。
	missing := filepath.Join(dir, "missing-out.txt")
	textExportPath = missing
	if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:NOPE"}); err == nil {
		t.Fatal("export of a missing key should fail")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing key must not create the file: %v", err)
	}

	// 符号链接目标被拒绝，链接目标内容不变。
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	textExportPath = link
	if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err == nil {
		t.Fatal("export onto a symlink should fail")
	}
	data, err = os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}

	// 缺 --path：参数错误。
	textExportPath = ""
	if err := textExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err == nil || !strings.Contains(err.Error(), "--path is required") {
		t.Fatalf("missing --path error = %v", err)
	}
}
