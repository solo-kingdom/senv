package cmd

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

const e2ePassword = "skill-e2e-password"

// e2eGit 在 dir 里跑 git，输出失败即终止。环境由 TestSkillApplyAcrossMachinesE2E 统一收口
// （不读真实 gitconfig、固定身份），所以这里不需要再传 env。
func e2eGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func under(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// useMachine 把进程切到“某台机器”：HOME、配置目录、数据目录都指向 home 之下。
// 配置目录 home/.config/senv，数据目录 home/.config/senv/data，git 根即 home/.config/senv。
func useMachine(t *testing.T, home string) (cfg, data string) {
	t.Helper()
	cfg = filepath.Join(home, ".config", "senv")
	data = filepath.Join(cfg, "data")
	t.Setenv("HOME", home)
	prevCfg, prevData := configPathFn, dataPath
	configPathFn = func() string { return cfg }
	dataPath = data
	t.Cleanup(func() { configPathFn, dataPath = prevCfg, prevData })
	clearAuthMemo()
	authPrompt = stubPrompter(e2ePassword)
	return cfg, data
}

// assertIsolated 是判据 §4 的前置：任何写入之前，路径、git 根与 remote 都必须在测试沙箱内。
func assertIsolated(t *testing.T, home, realHome, realCfg, realData string, otherHomes ...string) {
	t.Helper()
	if home == realHome {
		t.Fatalf("test home %s is the real home", home)
	}
	cfg, data := filepath.Join(home, ".config", "senv"), filepath.Join(home, ".config", "senv", "data")
	if got := getConfigPath(); got != cfg || !under(got, home) || got == realCfg {
		t.Fatalf("getConfigPath() = %s, want %s", got, cfg)
	}
	if got := getDataPath(); got != data || !under(got, home) || got == realData {
		t.Fatalf("getDataPath() = %s, want %s", got, data)
	}
	// GetGitPath 是配置目录与数据目录的共同祖先：本布局即 HOME_X/.config/senv
	if got := getStorage().GetGitPath(); got != cfg || !under(got, home) {
		t.Fatalf("GetGitPath() = %s, want %s", got, cfg)
	}
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Fatalf("os.UserHomeDir() = %q (%v), want %s", got, err, home)
	}
	for _, other := range otherHomes {
		if under(cfg, other) {
			t.Fatalf("%s overlaps %s", cfg, other)
		}
	}
}

func assertGitRemote(t *testing.T, gitRoot, bare string, homes ...string) {
	t.Helper()
	settings, err := getStorage().LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider.Type != provider.TypeGit {
		t.Fatalf("provider type = %q, want explicit git", settings.Provider.Type)
	}
	if got := e2eGit(t, gitRoot, "remote", "get-url", "origin"); got != bare {
		t.Fatalf("origin = %s, want %s", got, bare)
	}
	for _, h := range homes {
		if under(bare, h) {
			t.Fatalf("bare remote %s must live outside %s", bare, h)
		}
	}
}

// vaultHasSkill 报告 vault（配置目录 + 数据目录）里是否有该 skill 的密文或索引项。
func vaultHasSkill(t *testing.T, cfg, data, id string) bool {
	t.Helper()
	found := false
	for _, root := range []string{cfg, data} {
		_ = filepath.WalkDir(filepath.Join(root, storage.SkillDirName), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == id+storage.SkillFileSuffix {
				found = true
			}
			return nil
		})
		if raw, err := os.ReadFile(filepath.Join(root, storage.SkillIndexFile)); err == nil && bytes.Contains(raw, []byte(id)) {
			found = true
		}
	}
	return found
}

func resetSkillApplyFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		skillApplyGroup, skillApplyAll, skillApplyDryRun, skillApplyYes = "", false, false, false
	}
	reset()
	t.Cleanup(reset)
}

func runSync(t *testing.T) {
	t.Helper()
	captureStdout(t, func() {
		if err := syncCmd.RunE(&cobra.Command{}, nil); err != nil {
			t.Fatalf("senv sync: %v", err)
		}
	})
}

// TestSkillApplyAcrossMachinesE2E 是 senv-skills-config 的判据（design §4）：
// A 机入库并 sync → 移走 A 与源目录 → B 机 sync 后 apply，双根得到合法 skill。
// 全程 HOME、配置目录、数据目录都在临时目录；同步走显式 git + 外部 bare remote。
func TestSkillApplyAcrossMachinesE2E(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// 切换 HOME 之前记下真实家目录与真实 vault 位置，最后用来证明没有污染。
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	realCfg, realData := getDefaultConfigPath(), getDefaultDataPath()
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		t.Fatal(err)
	}
	id := "e2e-" + hex.EncodeToString(rnd[:])
	realRoots := []string{
		filepath.Join(realHome, ".agents", "skills", id),
		filepath.Join(realHome, ".claude", "skills", id),
	}
	assertRealUntouched := func(stage string) {
		t.Helper()
		for _, p := range realRoots {
			if pathExists(p) {
				t.Fatalf("%s: real skills root polluted: %s", stage, p)
			}
		}
		if vaultHasSkill(t, realCfg, realData, id) {
			t.Fatalf("%s: real vault contains test skill %s", stage, id)
		}
	}
	assertRealUntouched("before")

	isolateSessionCache(t) // 会话缓存、审计日志与 XDG_RUNTIME_DIR 也进沙箱
	resetSkillApplyFlags(t)
	resetSkillFlags(t)

	// git 不读真实 gitconfig，身份固定；PATH 陷阱：一旦有人调用 dotf 就留下标记。
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	for _, kv := range [][2]string{
		{"GIT_AUTHOR_NAME", "senv-e2e"}, {"GIT_AUTHOR_EMAIL", "e2e@example.invalid"},
		{"GIT_COMMITTER_NAME", "senv-e2e"}, {"GIT_COMMITTER_EMAIL", "e2e@example.invalid"},
	} {
		t.Setenv(kv[0], kv[1])
	}
	trapDir := t.TempDir()
	dotfMarker := filepath.Join(trapDir, "dotf-was-called")
	trap := "#!/bin/sh\necho \"$@\" >> '" + dotfMarker + "'\nexit 99\n"
	if err := os.WriteFile(filepath.Join(trapDir, "dotf"), []byte(trap), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", trapDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := exec.LookPath("dotf"); err != nil || filepath.Dir(got) != trapDir {
		t.Fatalf("dotf trap is not first on PATH: %q %v", got, err)
	}

	homeA, homeB := t.TempDir(), t.TempDir()
	bareRoot := t.TempDir()
	bare := filepath.Join(bareRoot, "vault.git")
	fixture := t.TempDir()
	for _, h := range []string{homeA, homeB} {
		if under(bare, h) || under(fixture, h) {
			t.Fatalf("bare remote / fixture must be outside %s", h)
		}
	}
	e2eGit(t, bareRoot, "init", "--bare", "-b", "main", bare)

	// 合成 fixture：SKILL.md name == id，带一个可执行脚本与子目录。
	writeFile := func(rel, content string, perm os.FileMode) {
		t.Helper()
		p := filepath.Join(fixture, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), perm); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("SKILL.md", "---\nname: "+id+"\ndescription: synthetic e2e skill\n---\n# E2E\n", 0o644)
	writeFile("scripts/run.sh", "#!/bin/sh\necho ok\n", 0o755)
	writeFile("references/note.md", "synthetic reference\n", 0o644)

	// ---- A：init、显式 git、baseline 推送 ----
	cfgA, dataA := useMachine(t, homeA)
	assertIsolated(t, homeA, realHome, realCfg, realData, homeB)
	if err := storage.NewManager(cfgA, dataA).Initialize(e2ePassword); err != nil {
		t.Fatalf("init A: %v", err)
	}
	settings, err := getStorage().LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Provider.Type = provider.TypeGit
	if err := getStorage().SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, cfgA, "init", "-b", "main")
	e2eGit(t, cfgA, "remote", "add", "origin", bare)
	assertGitRemote(t, cfgA, bare, homeA, homeB)
	runSync(t) // baseline：此刻 vault 里还没有 skill
	if vaultHasSkill(t, cfgA, dataA, id) {
		t.Fatal("baseline vault must not contain the skill yet")
	}

	// ---- B：先从 bare remote 取得 vault（此时远端还没有 skill）----
	cfgB, dataB := filepath.Join(homeB, ".config", "senv"), filepath.Join(homeB, ".config", "senv", "data")
	e2eGit(t, bareRoot, "clone", bare, cfgB)
	useMachine(t, homeB)
	assertIsolated(t, homeB, realHome, realCfg, realData, homeA)
	assertGitRemote(t, cfgB, bare, homeA, homeB)
	if !pathExists(filepath.Join(cfgB, storage.MetadataFile)) {
		t.Fatal("B must already hold the vault layout before A syncs the skill")
	}
	runSync(t)
	if vaultHasSkill(t, cfgB, dataB, id) {
		t.Fatal("B must not have the skill before A pushes it")
	}

	// ---- A：add + sync 推送 ----
	useMachine(t, homeA)
	assertIsolated(t, homeA, realHome, realCfg, realData, homeB)
	skillSource = fixture
	skillDescription = "e2e synthetic"
	captureStdout(t, func() {
		if err := skillAddCmd.RunE(&cobra.Command{}, []string{id}); err != nil {
			t.Fatalf("skill add on A: %v", err)
		}
	})
	if !vaultHasSkill(t, cfgA, dataA, id) {
		t.Fatal("skill add did not land in A's vault")
	}
	runSync(t)

	// ---- B：skill 在 sync 之后才出现 ----
	useMachine(t, homeB)
	assertIsolated(t, homeB, realHome, realCfg, realData, homeA)
	if vaultHasSkill(t, cfgB, dataB, id) {
		t.Fatal("B must not have the skill before its own sync")
	}
	runSync(t)
	if !pathExists(filepath.Join(dataB, storage.SkillDirName, "default", id+storage.SkillFileSuffix)) ||
		!pathExists(filepath.Join(dataB, storage.SkillIndexFile)) {
		t.Fatal("skill ciphertext/index must appear in B's data dir after sync")
	}

	// ---- 移走 A 与源目录：B 只能靠 vault 里的密文 ----
	if err := os.RemoveAll(homeA); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture); err != nil {
		t.Fatal(err)
	}
	if pathExists(homeA) || pathExists(fixture) {
		t.Fatal("HOME_A and the fixture source must be gone")
	}

	// ---- B：apply --yes，目标由 os.UserHomeDir() 解析为 HOME_B ----
	clearAuthMemo()
	skillApplyYes = true
	out := captureStdout(t, func() {
		if err := skillApplyCmd.RunE(&cobra.Command{}, []string{id}); err != nil {
			t.Fatalf("skill apply on B: %v", err)
		}
	})
	if !strings.Contains(out, "create") {
		t.Fatalf("apply output = %q", out)
	}
	for _, root := range []string{".agents", ".claude"} {
		dir := filepath.Join(homeB, root, "skills", id)
		md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: SKILL.md missing: %v", root, err)
		}
		name, err := skill.ParseName(md)
		if err != nil || name != id {
			t.Fatalf("%s: SKILL.md name = %q (%v), want %q", root, name, err, id)
		}
		if fi, err := os.Stat(filepath.Join(dir, "scripts", "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s: run.sh must keep its executable bit: %v", root, err)
		}
		if !pathExists(filepath.Join(dir, "references", "note.md")) || !pathExists(filepath.Join(dir, skill.OwnedMarker)) {
			t.Fatalf("%s: tree incomplete", root)
		}
	}
	// 家目录里只多出两个 skills 根与 senv 状态目录，没有别的落点
	if pathExists(homeA) {
		t.Fatal("apply must not recreate HOME_A")
	}

	// ---- 真实环境未被污染、dotf 未被调用 ----
	assertRealUntouched("after")
	if raw, err := os.ReadFile(dotfMarker); err == nil {
		t.Fatalf("dotf must not be invoked, trap recorded: %s", raw)
	}
}
