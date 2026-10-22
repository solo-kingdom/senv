package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// 全部用合成 fixture，数据/配置目录显式注入到临时目录（HOME 也在 isolateSessionCache 里改到临时目录）。
func writeSkillFixture(t *testing.T, name string) string {
	t.Helper()
	src := t.TempDir()
	body := "---\nname: " + name + "\ndescription: synthetic\n---\nBODY-MARKER-SHOULD-NOT-PRINT\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return src
}

func resetSkillFlags(t *testing.T) {
	t.Helper()
	skillSource, skillGroup, skillDescription, skillForce, skillListGroup = "", "", "", false, ""
	t.Cleanup(func() {
		skillSource, skillGroup, skillDescription, skillForce, skillListGroup = "", "", "", false, ""
	})
}

func TestSkillCommandSurfaceHasNoApply(t *testing.T) {
	names := map[string]bool{}
	for _, c := range skillCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"add", "list", "show", "remove"} {
		if !names[want] {
			t.Errorf("skill %s is missing", want)
		}
	}
	if names["apply"] {
		t.Error("skill apply belongs to the next slice and must not exist yet")
	}
}

func TestSkillAddListShowRemoveFlow(t *testing.T) {
	cfg, data := newAuditTestProject(t)
	resetSkillFlags(t)
	home, _ := os.UserHomeDir()

	skillSource = writeSkillFixture(t, "demo-skill")
	skillDescription = "demo description"
	out := captureStdout(t, func() {
		if err := skillAddCmd.RunE(&cobra.Command{}, []string{"demo-skill"}); err != nil {
			t.Fatalf("skill add: %v", err)
		}
	})
	if !strings.Contains(out, "Added skill demo-skill") {
		t.Fatalf("add output = %q", out)
	}
	// 密文与索引落在注入的数据目录，不在配置目录
	if _, err := os.Stat(filepath.Join(data, storage.SkillDirName, "default", "demo-skill.enc")); err != nil {
		t.Fatalf("ciphertext not in injected data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, storage.SkillIndexFile)); err != nil {
		t.Fatalf("index not in injected data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, storage.SkillIndexFile)); !os.IsNotExist(err) {
		t.Fatalf("index must not be in the config dir: %v", err)
	}
	// add 不会碰家目录下的 skills 根（apply 属于下一片）
	for _, root := range []string{".agents", ".claude"} {
		if _, err := os.Stat(filepath.Join(home, root, "skills", "demo-skill")); !os.IsNotExist(err) {
			t.Fatalf("add must not write %s skills root: %v", root, err)
		}
	}

	listOut := captureStdout(t, func() {
		if err := skillListCmd.RunE(&cobra.Command{}, nil); err != nil {
			t.Fatalf("skill list: %v", err)
		}
	})
	if !strings.Contains(listOut, "demo-skill") || !strings.Contains(listOut, "demo description") || !strings.Contains(listOut, "[default]") {
		t.Fatalf("list output = %q", listOut)
	}

	showOut := captureStdout(t, func() {
		if err := skillShowCmd.RunE(&cobra.Command{}, []string{"demo-skill"}); err != nil {
			t.Fatalf("skill show: %v", err)
		}
	})
	for _, want := range []string{"ID: demo-skill", "SKILL.md", "scripts/run.sh", "(executable)"} {
		if !strings.Contains(showOut, want) {
			t.Errorf("show output missing %q:\n%s", want, showOut)
		}
	}
	if strings.Contains(showOut, "BODY-MARKER") || strings.Contains(listOut, "BODY-MARKER") {
		t.Fatal("list/show must not print file contents")
	}

	// 审计只含 kind/group/id
	audit := readAuditLogForTest(t)
	if !strings.Contains(audit, "op_skill") || !strings.Contains(audit, "skill:default:demo-skill") {
		t.Fatalf("audit log missing skill add: %q", audit)
	}
	if strings.Contains(audit, "BODY-MARKER") {
		t.Fatal("audit log leaked content")
	}

	removeOut := captureStdout(t, func() {
		if err := skillRemoveCmd.RunE(&cobra.Command{}, []string{"demo-skill"}); err != nil {
			t.Fatalf("skill remove: %v", err)
		}
	})
	if !strings.Contains(removeOut, "Removed skill demo-skill") {
		t.Fatalf("remove output = %q", removeOut)
	}
	emptyOut := captureStdout(t, func() {
		if err := skillListCmd.RunE(&cobra.Command{}, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(emptyOut, "No skills found") {
		t.Fatalf("list after remove = %q", emptyOut)
	}
}

func TestSkillAddErrors(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)

	skillSource = writeSkillFixture(t, "other-name")
	if err := skillAddCmd.RunE(&cobra.Command{}, []string{"wanted-id"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("name != id error = %v", err)
	}

	skillSource = writeSkillFixture(t, "dup")
	if err := skillAddCmd.RunE(&cobra.Command{}, []string{"dup"}); err != nil {
		t.Fatal(err)
	}
	if err := skillAddCmd.RunE(&cobra.Command{}, []string{"dup"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate id error = %v", err)
	}
	skillForce = true
	if err := skillAddCmd.RunE(&cobra.Command{}, []string{"dup"}); err != nil {
		t.Fatalf("--force replace: %v", err)
	}
	if err := skillRemoveCmd.RunE(&cobra.Command{}, []string{"missing"}); err == nil {
		t.Fatal("remove of a missing skill must fail")
	}
	if err := skillShowCmd.RunE(&cobra.Command{}, []string{"missing"}); err == nil {
		t.Fatal("show of a missing skill must fail")
	}
}

func TestSkillAddRequiresSource(t *testing.T) {
	flag := skillAddCmd.Flags().Lookup("source")
	if flag == nil {
		t.Fatal("--source flag missing")
	}
	if ann := flag.Annotations[cobra.BashCompOneRequiredFlag]; len(ann) == 0 || ann[0] != "true" {
		t.Fatalf("--source must be marked required, annotations = %v", flag.Annotations)
	}
}

func TestDoctorReportsSkills(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "correct-secret")
	store := storage.NewManager(cfg, data)
	key := doctorProbeKey(t, store, "correct-secret")
	if err := store.SaveSkillWithKey("default", "demo", []byte("x"), key); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDoctorAt(cfg, data, stubPrompter("correct-secret"), &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out.String(), "skills:") || !strings.Contains(out.String(), "(1/1)") {
		t.Fatalf("doctor output missing skills probe:\n%s", out.String())
	}
}

func TestHistoryHandlesSkillKinds(t *testing.T) {
	if got := renderDecryptedHistory("skill", []byte("TAR-BODY")); strings.Contains(got, "TAR-BODY") || !strings.Contains(got, "8") {
		t.Fatalf("skill history must show size only, got %q", got)
	}
	plain, err := decryptHistoryValue(provider.HistoryVersion{Kind: "skill_index", Ciphertext: []byte(`{"skills":{}}`)}, nil)
	if err != nil || string(plain) != `{"skills":{}}` {
		t.Fatalf("skill_index history is plaintext: %q err=%v", plain, err)
	}
}
