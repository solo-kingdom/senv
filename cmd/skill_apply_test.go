package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func applyCmdWithBuffers() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	c := &cobra.Command{}
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	return c, &out, &errOut
}

func addTestSkill(t *testing.T, id string) {
	t.Helper()
	skillSource = writeSkillFixture(t, id)
	skillForce = true
	t.Cleanup(func() { skillForce = false })
	if err := skillAddCmd.RunE(&cobra.Command{}, []string{id}); err != nil {
		t.Fatalf("skill add %s: %v", id, err)
	}
}

func skillTargets(t *testing.T, id string) (agents, claude string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".agents", "skills", id), filepath.Join(home, ".claude", "skills", id)
}

func TestSkillApplyRequiresExactlyOneSelector(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	addTestSkill(t, "demo")
	skillApplyYes = true

	c, _, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, nil); err == nil {
		t.Fatal("no selector must be rejected")
	}
	skillApplyAll = true
	if err := skillApplyCmd.RunE(c, []string{"demo"}); err == nil {
		t.Fatal("id plus --all must be rejected")
	}
	agents, _ := skillTargets(t, "demo")
	if pathExists(agents) {
		t.Fatal("rejected invocations must not write")
	}
}

func TestSkillApplyMultiSelectAndProjectScope(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	addTestSkill(t, "alpha")
	addTestSkill(t, "beta")
	skillApplyYes = true
	skillApplyTargets = []string{"agents"}

	proj := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	skillApplyScope = "project"
	c, out, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"alpha", "beta", "alpha"}); err != nil {
		t.Fatalf("multi project apply: %v\nout=%s", err, out.String())
	}
	agentsAlpha := filepath.Join(proj, ".agents", "skills", "alpha")
	agentsBeta := filepath.Join(proj, ".agents", "skills", "beta")
	claudeAlpha := filepath.Join(proj, ".claude", "skills", "alpha")
	if !pathExists(filepath.Join(agentsAlpha, "SKILL.md")) || !pathExists(filepath.Join(agentsBeta, "SKILL.md")) {
		t.Fatalf("project agents roots missing: %v %v", agentsAlpha, agentsBeta)
	}
	if pathExists(claudeAlpha) {
		t.Fatal("--target agents must not write .claude")
	}
	home, _ := os.UserHomeDir()
	if pathExists(filepath.Join(home, ".agents", "skills", "alpha")) {
		t.Fatal("project scope must not write global home skills")
	}
}

func TestSkillApplyDryRunWritesNothing(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	addTestSkill(t, "demo")
	skillApplyDryRun = true

	c, out, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"demo"}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "[create] demo") || !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("dry-run output = %q", out.String())
	}
	agents, claude := skillTargets(t, "demo")
	if pathExists(agents) || pathExists(claude) {
		t.Fatal("dry-run wrote to the skills roots")
	}
	home, _ := os.UserHomeDir()
	if pathExists(filepath.Join(home, ".local", "state", "senv")) {
		t.Fatal("dry-run created senv state")
	}
}

func TestSkillApplyNeedsConfirmation(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	addTestSkill(t, "demo")
	agents, claude := skillTargets(t, "demo")

	// 非交互且无 --yes：拒绝
	stdinIsTerminal = func() bool { return false }
	c, _, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"demo"}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("non-interactive without --yes = %v", err)
	}
	if pathExists(agents) {
		t.Fatal("unconfirmed apply wrote files")
	}

	// 交互：回答 n → 不写；回答 y → 写
	stdinIsTerminal = func() bool { return true }
	answer := func(text string) string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.WriteString(text)
		_ = w.Close()
		old := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = old; _ = r.Close() }()
		c, out, _ := applyCmdWithBuffers()
		var runErr error
		captureStdout(t, func() { runErr = skillApplyCmd.RunE(c, []string{"demo"}) })
		if runErr != nil {
			t.Fatal(runErr)
		}
		return out.String()
	}
	if out := answer("n\n"); !strings.Contains(out, "Aborted") || pathExists(agents) {
		t.Fatalf("answer n: out=%q exists=%v", out, pathExists(agents))
	}
	answer("y\n")
	if !pathExists(filepath.Join(agents, "SKILL.md")) || !pathExists(filepath.Join(claude, "SKILL.md")) {
		t.Fatal("answer y must write both roots")
	}
}

func TestSkillApplyConflictSkipsWithWarning(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	addTestSkill(t, "demo")
	skillApplyYes = true
	agents, claude := skillTargets(t, "demo")

	// .agents 下已有无标记的手写目录；.claude 下是软链
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "SKILL.md"), []byte("hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	userDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(claude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(userDir, claude); err != nil {
		t.Fatal(err)
	}

	c, out, errOut := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"demo"}); err != nil {
		t.Fatalf("conflicts must be warnings, got error %v", err)
	}
	if strings.Count(errOut.String(), "warning: skipped") != 2 {
		t.Fatalf("want two warnings, stderr = %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Nothing to apply") {
		t.Fatalf("stdout = %q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(agents, "SKILL.md")); string(b) != "hand written" {
		t.Fatalf("unowned dir modified: %q", b)
	}
	if entries, _ := os.ReadDir(userDir); len(entries) != 0 {
		t.Fatalf("symlink target modified: %v", entries)
	}
}

func TestSkillApplyGroupAllAndOverwriteBackup(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	skillGroup = "work"
	addTestSkill(t, "in-work")
	skillGroup = ""
	addTestSkill(t, "in-default")
	skillApplyYes = true

	skillApplyGroup = "work"
	c, _, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, nil); err != nil {
		t.Fatal(err)
	}
	work, _ := skillTargets(t, "in-work")
	other, _ := skillTargets(t, "in-default")
	if !pathExists(work) || pathExists(other) {
		t.Fatalf("--group work: work=%v default=%v", pathExists(work), pathExists(other))
	}

	skillApplyGroup, skillApplyAll = "", true
	c, out, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, nil); err != nil {
		t.Fatal(err)
	}
	if !pathExists(other) || !strings.Contains(out.String(), "[skip] in-work") {
		t.Fatalf("--all: default=%v out=%q", pathExists(other), out.String())
	}

	// 托管目录被本地改动 → backup+overwrite，备份在 skills 根之外
	if err := os.WriteFile(filepath.Join(work, "SKILL.md"), []byte("locally edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, out, _ = applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "backup+overwrite in-work") || !strings.Contains(out.String(), "backed up at") {
		t.Fatalf("out = %q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(work, "SKILL.md")); !strings.Contains(string(b), "name: in-work") {
		t.Fatalf("not restored: %q", b)
	}
	home, _ := os.UserHomeDir()
	backups, _ := filepath.Glob(filepath.Join(home, ".local", "state", "senv", "skill-apply", "backups", "in-work.senv-backup-*"))
	if len(backups) == 0 {
		t.Fatal("backup missing in the senv state directory")
	}
	for _, root := range []string{".agents", ".claude"} {
		entries, _ := os.ReadDir(filepath.Join(home, root, "skills"))
		for _, e := range entries {
			if strings.Contains(e.Name(), "backup") {
				t.Fatalf("backup leaked into %s/skills: %s", root, e.Name())
			}
		}
	}
}

func TestSkillApplyUnknownSkillAndEmptyVault(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	skillApplyYes = true
	c, out, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"missing"}); err == nil {
		t.Fatal("unknown id must fail")
	}
	skillApplyAll = true
	if err := skillApplyCmd.RunE(c, nil); err != nil || !strings.Contains(out.String(), "No skills to apply") {
		t.Fatalf("empty vault: err=%v out=%q", err, out.String())
	}
}
