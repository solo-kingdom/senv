package storage_test

import (
	"reflect"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// TestMachineLocalGitIgnoreEntriesLocked 锁定登记集合，防止新增机器本地
// 文件时忘记登记（集合变化必须同步更新本测试并说明理由）。
func TestMachineLocalGitIgnoreEntriesLocked(t *testing.T) {
	want := []string{
		"tui-snapshot.enc",
		".senv-sync-state.json",
		".senv-sync.lock",
		"server-token.json",
		"mcp-exports.json",
		"agent-pointers.json",
		".senv-vault.lock",
		"cache/",
	}
	if got := storage.MachineLocalGitIgnoreEntries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("MachineLocalGitIgnoreEntries() = %v, want %v", got, want)
	}
}

func TestMachineLocalDataArtifactExactMatch(t *testing.T) {
	yes := []string{"tui-snapshot.enc", ".senv-sync-state.json", ".senv-sync.lock"}
	for _, name := range yes {
		if !storage.IsMachineLocalDataArtifact(name) {
			t.Errorf("IsMachineLocalDataArtifact(%q) = false, want true", name)
		}
	}
	no := []string{
		"tui-snapshot.encX",
		"data/tui-snapshot.enc",
		"/tui-snapshot.enc",
		"tui-snapshot",
		"",
		"server-token.json", // configPath scope，不在 dataPath
		"cache",             // configPath scope，不在 dataPath
	}
	for _, name := range no {
		if storage.IsMachineLocalDataArtifact(name) {
			t.Errorf("IsMachineLocalDataArtifact(%q) = true, want false", name)
		}
	}
}

func TestMachineLocalConfigArtifactExactMatch(t *testing.T) {
	yes := []string{
		"server-token.json",
		"mcp-exports.json",
		"agent-pointers.json",
		".senv-vault.lock",
		"cache",
	}
	for _, name := range yes {
		if !storage.IsMachineLocalConfigArtifact(name) {
			t.Errorf("IsMachineLocalConfigArtifact(%q) = false, want true", name)
		}
	}
	no := []string{
		"tui-snapshot.enc", // dataPath scope，不在 configPath
		"cache/models-dev.json",
		"server-token.json.bak",
		"server-token",
		"",
	}
	for _, name := range no {
		if storage.IsMachineLocalConfigArtifact(name) {
			t.Errorf("IsMachineLocalConfigArtifact(%q) = true, want false", name)
		}
	}
}

func TestMachineLocalGitExcludeGlobs(t *testing.T) {
	globs := storage.MachineLocalGitExcludeGlobs()
	// 7 个文件条目各 1 条 + 1 个目录条目 2 条 = 9
	if len(globs) != 9 {
		t.Fatalf("len(MachineLocalGitExcludeGlobs()) = %d, want 9: %v", len(globs), globs)
	}
	seen := make(map[string]bool, len(globs))
	for _, g := range globs {
		seen[g] = true
	}
	for _, want := range []string{
		":(exclude,glob)**/tui-snapshot.enc",
		":(exclude,glob)**/.senv-sync-state.json",
		":(exclude,glob)**/server-token.json",
		":(exclude,glob)**/agent-pointers.json",
		":(exclude,glob)**/cache",
		":(exclude,glob)**/cache/**",
	} {
		if !seen[want] {
			t.Errorf("MachineLocalGitExcludeGlobs() missing %q: %v", want, globs)
		}
	}
}
