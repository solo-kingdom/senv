package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func newBackupTestProject(t *testing.T) {
	t.Helper()
	newAuditTestProject(t)
}

func TestBackupInitSeedsDefault(t *testing.T) {
	newBackupTestProject(t)
	mgr, err := getBackupManager()
	if err != nil {
		t.Fatal(err)
	}
	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range groups {
		if g.Name == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("init must seed backup default, groups=%v", groups)
	}
	if err := mgr.Set("default", "DUMP", "seeded"); err != nil {
		t.Fatalf("write into init default: %v", err)
	}
}

func TestBackupGetHasNoDecodeFlags(t *testing.T) {
	if backupGetCmd.Flags().Lookup("decode") != nil {
		t.Fatal("backup get must not expose --decode")
	}
	if backupGetCmd.Flags().ShorthandLookup("d") != nil {
		t.Fatal("backup get must not expose -d")
	}
	if backupGetCmd.Flags().Lookup("loose") != nil {
		t.Fatal("backup get must not expose --loose")
	}
}

func TestBackupGroupAddRequiresDescriptionFlag(t *testing.T) {
	flag := backupGroupAddCmd.Flags().Lookup("description")
	if flag == nil {
		t.Fatal("backup group add --description is missing")
	}
	if flag.DefValue != "" {
		t.Fatalf("description default = %q, want empty", flag.DefValue)
	}
}

func TestBackupListOmitsValue(t *testing.T) {
	newBackupTestProject(t)
	mgr, err := getBackupManager()
	if err != nil {
		t.Fatal(err)
	}
	note := "weekly dump"
	secret := "SECRET-BACKUP-BODY-XYZ"
	if err := mgr.SetWithDescription("default", "DUMP", secret, &note); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t, func() {
		if err := backupListCmd.RunE(&cobra.Command{}, []string{"default"}); err != nil {
			t.Fatalf("backup list: %v", err)
		}
	})
	if !strings.Contains(stdout, "DUMP") {
		t.Fatalf("list missing key: %q", stdout)
	}
	if !strings.Contains(stdout, note) {
		t.Fatalf("list missing description: %q", stdout)
	}
	if strings.Contains(stdout, secret) {
		t.Fatalf("list leaked value: %q", stdout)
	}
}

func TestBackupImportFlow(t *testing.T) {
	newBackupTestProject(t)
	dir := t.TempDir()
	mgr, err := getBackupManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddGroup("notes", "test"); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "dump.txt")
	content := []byte("# hello\nmulti-line import\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	backupImportFile = src
	t.Cleanup(func() { backupImportFile = "" })
	if err := backupImportCmd.RunE(&cobra.Command{}, []string{"notes:DUMP"}); err != nil {
		t.Fatalf("backup import: %v", err)
	}

	value, err := mgr.Get("notes", "DUMP")
	if err != nil || value != string(content) {
		t.Fatalf("imported value = %q, %v", value, err)
	}

	data, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("source file changed: %q, %v", data, err)
	}

	if log := readAuditLogForTest(t); !strings.Contains(log, "import "+src) {
		t.Fatalf("audit missing import detail: %s", log)
	}

	backupGroup = "elsewhere"
	t.Cleanup(func() { backupGroup = "default" })
	src2 := filepath.Join(dir, "ADDR.txt")
	if err := os.WriteFile(src2, []byte("addr"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupImportFile = src2
	if err := backupImportCmd.RunE(&cobra.Command{}, []string{"notes:ADDR"}); err != nil {
		t.Fatalf("backup import with flag group: %v", err)
	}
	backupGroup = "default"
	if _, err := mgr.Get("notes", "ADDR"); err != nil {
		t.Fatalf("address group should win over -g: %v", err)
	}
	if _, err := mgr.Get("elsewhere", "ADDR"); err == nil {
		t.Fatal("entry must not land in the -g group when address has a group")
	}

	infos, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	var firstUpdated time.Time
	for _, info := range infos {
		if info.Key == "DUMP" {
			firstUpdated = info.UpdatedAt
		}
	}
	if firstUpdated.IsZero() {
		t.Fatalf("DUMP missing from list: %v", infos)
	}
	updated := []byte("replaced content\n")
	if err := os.WriteFile(src, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	backupImportFile = src
	if err := backupImportCmd.RunE(&cobra.Command{}, []string{"notes:DUMP"}); err != nil {
		t.Fatalf("backup import overwrite: %v", err)
	}
	value, err = mgr.Get("notes", "DUMP")
	if err != nil || value != string(updated) {
		t.Fatalf("overwritten value = %q, %v", value, err)
	}
	infos, err = mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Key == "DUMP" {
			if !info.UpdatedAt.After(firstUpdated) {
				t.Fatalf("updated_at not refreshed: before=%v after=%v", firstUpdated, info.UpdatedAt)
			}
		}
	}

	before, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	backupImportFile = filepath.Join(dir, "missing.md")
	if err := backupImportCmd.RunE(&cobra.Command{}, []string{"notes:GHOST"}); err == nil {
		t.Fatal("import of a missing file should fail")
	}
	after, err := mgr.List("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("vault side effects: before=%v after=%v", before, after)
	}

	backupImportFile = ""
	if err := backupImportCmd.RunE(&cobra.Command{}, []string{"notes:DUMP"}); err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("missing --file error = %v", err)
	}
}

func TestBackupExportFlow(t *testing.T) {
	newBackupTestProject(t)
	dir := t.TempDir()
	mgr, err := getBackupManager()
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

	out := filepath.Join(dir, "key.pem")
	backupExportPath = out
	t.Cleanup(func() { backupExportPath = "" })
	stdout := captureStdout(t, func() {
		if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err != nil {
			t.Fatalf("backup export: %v", err)
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

	if err := os.WriteFile(out, []byte("loose"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err != nil {
		t.Fatalf("backup export overwrite: %v", err)
	}
	info, err = os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("tightened mode = %o, %v", info.Mode().Perm(), err)
	}
	data, err = os.ReadFile(out)
	if err != nil || !bytes.Equal(data, secret) {
		t.Fatalf("overwritten content = %q, %v", data, err)
	}

	raw := []byte("token={{env:secrets:API_KEY}}\n")
	if err := mgr.Set("secrets", "TEMPLATE", string(raw)); err != nil {
		t.Fatal(err)
	}
	tplOut := filepath.Join(dir, "tpl.txt")
	backupExportPath = tplOut
	if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:TEMPLATE"}); err != nil {
		t.Fatalf("backup export template: %v", err)
	}
	data, err = os.ReadFile(tplOut)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatalf("template export must be byte-exact without ref resolution: %q, %v", data, err)
	}

	missing := filepath.Join(dir, "missing-out.txt")
	backupExportPath = missing
	if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:NOPE"}); err == nil {
		t.Fatal("export of a missing key should fail")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing key must not create the file: %v", err)
	}

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	backupExportPath = link
	if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err == nil {
		t.Fatal("export onto a symlink should fail")
	}
	data, err = os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}

	backupExportPath = ""
	if err := backupExportCmd.RunE(&cobra.Command{}, []string{"secrets:KEY"}); err == nil || !strings.Contains(err.Error(), "--path is required") {
		t.Fatalf("missing --path error = %v", err)
	}
}

func TestRootShorthandWritesTextNotBackup(t *testing.T) {
	dir := t.TempDir()
	storeMgr := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	textMgr := text.NewManager(storeMgr, "test-password")
	backupMgr := backup.NewManager(storeMgr, "test-password")
	if err := textMgr.AddGroup("notes", "test"); err != nil {
		t.Fatal(err)
	}

	group, key, ok := parseAddress("notes:TODO")
	if !ok {
		t.Fatal("parseAddress failed")
	}
	if err := runTextShorthandWithManager(textMgr, group, key, "", []string{"hello"}); err != nil {
		t.Fatalf("text shorthand: %v", err)
	}
	got, err := textMgr.Get("notes", "TODO")
	if err != nil || got != "hello" {
		t.Fatalf("text got %q err %v", got, err)
	}
	if _, err := backupMgr.Get("notes", "TODO"); err == nil {
		t.Fatal("root shorthand must not write backup")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "backups", "notes", "TODO.enc")); !os.IsNotExist(err) {
		t.Fatal("backups/ must not contain the shorthand file")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "texts", "notes", "TODO.enc")); err != nil {
		t.Fatalf("texts/ missing shorthand file: %v", err)
	}
}
