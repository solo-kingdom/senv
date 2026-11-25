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

func TestSkillApplyDefaultAndPolicyRouting(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	// 准备一个本机存在的项目目录 ~/proj-ok，和一个不存在的 ~/proj-missing
	projOK := filepath.Join(home, "proj-ok")
	if err := os.MkdirAll(projOK, 0o755); err != nil {
		t.Fatal(err)
	}

	// 建组：base (default=true, global, agents only)
	skillGrpDescription = "base global skills"
	skillGrpDefault = true
	skillGrpScope = "global"
	skillGrpTargets = []string{"agents"}
	if err := skillGroupAddCmd.Flags().Set("default-install", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = skillGroupAddCmd.Flags().Set("default-install", "false")
	})
	captureStdout(t, func() {
		if err := skillGroupAddCmd.RunE(skillGroupAddCmd, []string{"base"}); err != nil {
			t.Fatalf("group add base: %v", err)
		}
	})

	// 1. g-def: 继承 base (default=true, global, targets=[agents])
	skillGroup = "base"
	skillSource = writeSkillFixture(t, "g-def")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"g-def"}); err != nil {
			t.Fatal(err)
		}
	})

	// 2. g-opt: 显式 --no-default-install，不应被 --default 选中
	skillAddNoDefault = true
	skillSource = writeSkillFixture(t, "g-opt")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"g-opt"}); err != nil {
			t.Fatal(err)
		}
	})
	skillAddNoDefault = false
	skillGroup = ""

	// 3. p-ok: default=true, project, ~/proj-ok (存在)
	skillAddDefault = true
	skillAddScope = "project"
	skillAddInstallPath = "~/proj-ok"
	skillSource = writeSkillFixture(t, "p-ok")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"p-ok"}); err != nil {
			t.Fatal(err)
		}
	})

	// 4. p-missing: default=true, project, ~/proj-missing (不存在 → skip-missing-path)
	skillAddInstallPath = "~/proj-missing"
	skillSource = writeSkillFixture(t, "p-missing")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"p-missing"}); err != nil {
			t.Fatal(err)
		}
	})

	// 5. p-cwd: default=true, project, 无 path (→ skip-project-only)
	skillAddInstallPath = ""
	skillSource = writeSkillFixture(t, "p-cwd")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"p-cwd"}); err != nil {
			t.Fatal(err)
		}
	})
	skillAddDefault, skillAddScope = false, ""

	// 执行 apply --default --yes（auto 路由）
	skillApplyDefault = true
	skillApplyYes = true
	c, out, errOut := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, nil); err != nil {
		t.Fatalf("apply --default: %v\nout=%s\nerr=%s", err, out.String(), errOut.String())
	}

	// g-def 仅装到 ~/.agents/skills/g-def（不装 .claude）
	gAgents, gClaude := skillTargets(t, "g-def")
	if !pathExists(filepath.Join(gAgents, "SKILL.md")) || pathExists(gClaude) {
		t.Fatalf("g-def targets mismatch: agents=%v claude=%v", pathExists(gAgents), pathExists(gClaude))
	}
	// g-opt 未被 --default 安装
	optAgents, _ := skillTargets(t, "g-opt")
	if pathExists(optAgents) {
		t.Fatal("g-opt must not be installed by --default")
	}
	// p-ok 装到了 ~/proj-ok 下，且未污染全局
	if !pathExists(filepath.Join(projOK, ".agents", "skills", "p-ok", "SKILL.md")) {
		t.Fatalf("p-ok not installed to %s", projOK)
	}
	pOkGlobal, _ := skillTargets(t, "p-ok")
	if pathExists(pOkGlobal) {
		t.Fatal("project skill p-ok must not be installed globally")
	}
	// p-missing 与 p-cwd 被跳过且不建目录
	if !strings.Contains(out.String(), "[skip-missing-path] p-missing") || pathExists(filepath.Join(home, "proj-missing")) {
		t.Fatalf("p-missing handling wrong: out=%s", out.String())
	}
	if !strings.Contains(out.String(), "[skip-project-only] p-cwd") {
		t.Fatalf("p-cwd handling wrong: out=%s", out.String())
	}

	// 显式 --scope global 必须拦截 project skill（skip-scope-mismatch）
	resetSkillApplyFlags(t)
	skillApplyScope = "global"
	skillApplyYes = true
	c, out, _ = applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"p-cwd"}); err != nil {
		t.Fatalf("scope mismatch should warn+skip, got err: %v", err)
	}
	if !strings.Contains(out.String(), "[skip-scope-mismatch] p-cwd") {
		t.Fatalf("expected skip-scope-mismatch, out=%s", out.String())
	}
	pCwdGlobal, _ := skillTargets(t, "p-cwd")
	if pathExists(pCwdGlobal) {
		t.Fatal("project skill must not be installed globally without --force-scope")
	}

	// 加 --force-scope 允许强装到全局
	skillApplyForceScope = true
	c, _, _ = applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"p-cwd"}); err != nil {
		t.Fatalf("force-scope apply: %v", err)
	}
	if !pathExists(filepath.Join(pCwdGlobal, "SKILL.md")) {
		t.Fatal("--force-scope must install globally")
	}

	// --dir 无需 cd 即可安装到指定项目目录
	resetSkillApplyFlags(t)
	otherProj := t.TempDir()
	skillApplyDir = otherProj
	skillApplyYes = true
	c, _, _ = applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"p-cwd"}); err != nil {
		t.Fatalf("--dir apply: %v", err)
	}
	if !pathExists(filepath.Join(otherProj, ".agents", "skills", "p-cwd", "SKILL.md")) {
		t.Fatalf("--dir did not install into %s", otherProj)
	}

	// unapply --default 对称撤回 g-def 与 p-ok
	resetSkillApplyFlags(t)
	skillApplyDefault = true
	skillApplyYes = true
	c, _, _ = applyCmdWithBuffers()
	if err := skillUnapplyCmd.RunE(c, nil); err != nil {
		t.Fatalf("unapply --default: %v", err)
	}
	if pathExists(gAgents) || pathExists(filepath.Join(projOK, ".agents", "skills", "p-ok")) {
		t.Fatal("unapply --default must remove managed dirs for both global and project-path skills")
	}
}

func TestSkillApplyAppendsExtraInfoAndTracksDrift(t *testing.T) {
	newAuditTestProject(t)
	resetSkillFlags(t)
	resetSkillApplyFlags(t)

	// 1. 入库时带 --extra，模拟 llm-wiki 服务地址扩展信息
	skillSource = writeSkillFixture(t, "llm-wiki")
	skillAddExtra = "- 服务地址: https://wiki.example.internal\n- 认证方式: 通过 `senv env get svc:WIKI_TOKEN` 获取"
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"llm-wiki"}); err != nil {
			t.Fatalf("skill add with extra: %v", err)
		}
	})

	// show 展示 Extra Info
	showOut := captureStdout(t, func() {
		if err := skillShowCmd.RunE(skillShowCmd, []string{"llm-wiki"}); err != nil {
			t.Fatalf("skill show: %v", err)
		}
	})
	if !strings.Contains(showOut, "Extra Info:") || !strings.Contains(showOut, "https://wiki.example.internal") {
		t.Fatalf("show missing extra info:\n%s", showOut)
	}

	// 2. apply 后 SKILL.md 末尾包含扩展块
	skillApplyYes = true
	c, _, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"llm-wiki"}); err != nil {
		t.Fatalf("apply with extra: %v", err)
	}
	agentsDir, _ := skillTargets(t, "llm-wiki")
	installed, err := os.ReadFile(filepath.Join(agentsDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "<!-- senv:extra:start -->") || !strings.Contains(string(installed), "https://wiki.example.internal") {
		t.Fatalf("installed SKILL.md missing extra block:\n%s", installed)
	}

	// 3. 再次 apply 必须精确 skip（不误判漂移）
	c, out, _ := applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"llm-wiki"}); err != nil {
		t.Fatalf("idempotent apply: %v", err)
	}
	if !strings.Contains(out.String(), "[skip] llm-wiki") {
		t.Fatalf("expected skip on second apply, out=%s", out.String())
	}

	// 4. 上游更新 add --force（不传 --extra）保留原有扩展信息
	skillAddExtra = ""
	skillForce = true
	skillSource = writeSkillFixture(t, "llm-wiki")
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(skillAddCmd, []string{"llm-wiki"}); err != nil {
			t.Fatalf("force upgrade: %v", err)
		}
	})
	skillForce = false

	// 5. 通过 skill edit --extra-file 更新扩展信息 → apply 触发 backup+overwrite 刷入新地址
	extraFile := filepath.Join(t.TempDir(), "extra.md")
	if err := os.WriteFile(extraFile, []byte("- 服务地址: https://wiki-v2.example.internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := skillEditCmd.Flags().Set("extra-file", extraFile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skillEditCmd.Flags().Set("extra-file", "") })
	captureStdout(t, func() {
		if err := skillEditCmd.RunE(skillEditCmd, []string{"llm-wiki"}); err != nil {
			t.Fatalf("skill edit --extra-file: %v", err)
		}
	})

	c, out, _ = applyCmdWithBuffers()
	if err := skillApplyCmd.RunE(c, []string{"llm-wiki"}); err != nil {
		t.Fatalf("apply after edit extra: %v", err)
	}
	if !strings.Contains(out.String(), "[backup+overwrite] llm-wiki") {
		t.Fatalf("expected backup+overwrite after extra update, out=%s", out.String())
	}
	installedV2, err := os.ReadFile(filepath.Join(agentsDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installedV2), "https://wiki-v2.example.internal") || strings.Contains(string(installedV2), "https://wiki.example.internal") {
		t.Fatalf("installed SKILL.md not updated:\n%s", installedV2)
	}
}
