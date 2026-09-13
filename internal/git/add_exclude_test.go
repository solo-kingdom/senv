package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRealRepo 建一个真实 git 仓（需要 git 二进制；跳过不可用环境）。
func initRealRepo(t *testing.T) *Manager {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	return NewManager(dir)
}

// TestAddExcludesMachineLocalSecrets 验证 git add . 不暂存机器本地敏感文件，
// 即使 .gitignore 完全缺失（旧版本初始化的仓库、用户自建仓库）。
func TestAddExcludesMachineLocalSecrets(t *testing.T) {
	m := initRealRepo(t)

	write := func(rel, content string) {
		path := filepath.Join(m.repoPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("metadata.json", "{}")
	write("server-token.json", `{"token":"secret-token"}`)
	write("mcp-exports.json", `{"cursor":{"github":"fp"}}`)
	write("agent-pointers.json", `{"provider":"x"}`)
	write(".senv-vault.lock", "")
	write("cache/models-dev.json", "{}")
	// 嵌套目录里的同名文件也要被排除（自定义 configPath 布局）
	write("nested/cfg/server-token.json", `{"token":"nested-token"}`)
	write("nested/cfg/agent-pointers.json", `{}`)
	// 机器本地缓存（TUI 快照、同步 state、同步锁）
	write("tui-snapshot.enc", "snapshot")
	write("data/tui-snapshot.enc", "snapshot")
	write("data/.senv-sync-state.json", "{}")
	write("data/.senv-sync.lock", "")
	write("data/envs/default/API_KEY.enc", "cipher")

	if err := m.Add(); err != nil {
		t.Fatalf("Add: %v", err)
	}

	staged := func(path string) bool {
		cmd := exec.Command("git", "diff", "--cached", "--name-only", "--", path)
		cmd.Dir = m.repoPath
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git diff --cached %s: %v", path, err)
		}
		return len(out) > 0
	}

	if !staged("metadata.json") {
		t.Fatal("metadata.json should be staged")
	}
	if !staged("data/envs/default/API_KEY.enc") {
		t.Fatal("regular data file should be staged")
	}
	for _, forbidden := range []string{
		"server-token.json",
		"nested/cfg/server-token.json",
		"mcp-exports.json",
		"agent-pointers.json",
		"nested/cfg/agent-pointers.json",
		".senv-vault.lock",
		"cache/models-dev.json",
		"tui-snapshot.enc",
		"data/tui-snapshot.enc",
		"data/.senv-sync-state.json",
		"data/.senv-sync.lock",
	} {
		if staged(forbidden) {
			t.Fatalf("%s must never be staged by git add", forbidden)
		}
	}
}
