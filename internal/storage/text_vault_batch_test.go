//go:build unix

package storage

import (
	"testing"

	"github.com/wii/senv/internal/securefs"
)

// TestLoadTextVaultMatchesPerEntryLoad 验证批量装载路径（单锁单 root）与
// 逐组/逐条路径的读取结果逐字段等价（tui-startup-perf D2 的正确性前提）：
// 分组集合（含空组）、每组 key 集合、条目内容/大小/时间戳完全一致。
func TestLoadTextVaultMatchesPerEntryLoad(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	if err := mgr.AddTextGroup("zeta"); err != nil {
		t.Fatalf("create empty group zeta: %v", err)
	}
	want := map[string]map[string]string{
		"default":  {"A": "1", "B": "22"},
		"llm-keys": {},
		"prod":     {"X": "secret"},
		"zeta":     {},
	}
	for group, entries := range want {
		for k, v := range entries {
			if err := mgr.SaveTextFileWithKey(group, k, NewTextEntry(v), key); err != nil {
				t.Fatalf("save %s/%s: %v", group, k, err)
			}
		}
	}

	batch, err := mgr.LoadTextVaultWithKey(key)
	if err != nil {
		t.Fatalf("LoadTextVaultWithKey: %v", err)
	}
	if len(batch.Groups) != len(want) {
		t.Fatalf("batch groups = %v (%d), want %d", batch.Groups, len(batch.Groups), len(want))
	}
	for _, name := range batch.Groups {
		wantEntries, ok := want[name]
		if !ok {
			t.Fatalf("unexpected group %q in batch", name)
		}
		files := batch.Entries[name]
		if len(files) != len(wantEntries) {
			t.Fatalf("group %q entries = %d, want %d", name, len(files), len(wantEntries))
		}
		byKey := make(map[string]*TextEntry, len(files))
		for _, f := range files {
			if f.Key == "" || f.Entry == nil {
				t.Fatalf("group %q has file with empty key or entry: %+v", name, f)
			}
			byKey[f.Key] = f.Entry
		}
		for k, v := range wantEntries {
			// 与逐条路径对照：内容、大小、时间戳逐项一致
			perEntry, err := mgr.LoadTextFileWithKey(name, k, key)
			if err != nil {
				t.Fatalf("LoadTextFileWithKey(%q, %q): %v", name, k, err)
			}
			got := byKey[k]
			if got == nil {
				t.Fatalf("batch missing %s/%s", name, k)
			}
			if got.Value != v || got.Value != perEntry.Value {
				t.Errorf("%s/%s value: batch=%q per-entry=%q want=%q", name, k, got.Value, perEntry.Value, v)
			}
			if got.Size != perEntry.Size || got.UpdatedAt != perEntry.UpdatedAt {
				t.Errorf("%s/%s meta: batch=(%d,%s) per-entry=(%d,%s)", name, k,
					got.Size, got.UpdatedAt, perEntry.Size, perEntry.UpdatedAt)
			}
		}
	}

	// 批量结果对新写可见（无跨调用缓存）
	if err := mgr.SaveTextFileWithKey("prod", "X", NewTextEntry("rotated"), key); err != nil {
		t.Fatalf("rewrite prod/X: %v", err)
	}
	batch2, err := mgr.LoadTextVaultWithKey(key)
	if err != nil {
		t.Fatalf("reload after write: %v", err)
	}
	if got := batch2.Entries["prod"][0].Entry.Value; got != "rotated" {
		t.Fatalf("batch read after write = %q, want rotated", got)
	}
}

// TestLoadTextVaultAcquiresSingleLock 验证单趟批量读取的排它锁获取次数
// 不随条目数增长：N 个条目 + G 个分组只取 1 次锁；逐条路径为 1+G+N 次。
func TestLoadTextVaultAcquiresSingleLock(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	const groups = 3
	const perGroup = 10
	for g := 0; g < groups; g++ {
		name := "g" + string(rune('a'+g))
		for i := 0; i < perGroup; i++ {
			k := "k" + string(rune('a'+i))
			if err := mgr.SaveTextFileWithKey(name, k, NewTextEntry("v"), key); err != nil {
				t.Fatalf("seed %s/%s: %v", name, k, err)
			}
		}
	}

	vaultLockAcquires.Store(0)
	if _, err := mgr.LoadTextVaultWithKey(key); err != nil {
		t.Fatalf("LoadTextVaultWithKey: %v", err)
	}
	if got := vaultLockAcquires.Load(); got != 1 {
		t.Fatalf("batch load acquired vault lock %d times, want exactly 1", got)
	}

	// 对照：逐组/逐条路径的锁次数 = 1(ListTextGroups) + G(ListTextFiles) + N(LoadTextFile)
	vaultLockAcquires.Store(0)
	names, err := mgr.ListTextGroups()
	if err != nil {
		t.Fatalf("ListTextGroups: %v", err)
	}
	total := 0
	for _, name := range names {
		keys, err := mgr.ListTextFiles(name)
		if err != nil {
			t.Fatalf("ListTextFiles(%q): %v", name, err)
		}
		for _, k := range keys {
			if _, err := mgr.LoadTextFileWithKey(name, k, key); err != nil {
				t.Fatalf("LoadTextFileWithKey(%q,%q): %v", name, k, err)
			}
			total++
		}
	}
	wantLocks := int64(1 + len(names) + total)
	if got := vaultLockAcquires.Load(); got != wantLocks {
		t.Fatalf("per-entry path acquired vault lock %d times, want %d (1+%d groups+%d entries)",
			got, wantLocks, len(names), total)
	}
	if total != groups*perGroup {
		t.Fatalf("seeded entries = %d, want %d", total, groups*perGroup)
	}
}

// TestLoadTextVaultDegradesPerGroup 验证条目级失败按组降级：某组一个条目
// 损坏（无法解密）时，该组保留列出（KeyCount 为列出文件数）、条目缺失并
// 记录原因，其余组不受影响——与逐组消费方「List 失败 → 该组置空」等价。
func TestLoadTextVaultDegradesPerGroup(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	if err := mgr.SaveTextFileWithKey("good", "A", NewTextEntry("1"), key); err != nil {
		t.Fatalf("seed good/A: %v", err)
	}
	if err := mgr.SaveTextFileWithKey("bad", "B", NewTextEntry("2"), key); err != nil {
		t.Fatalf("seed bad/B: %v", err)
	}
	if err := mgr.SaveTextFileWithKey("bad", "C", NewTextEntry("3"), key); err != nil {
		t.Fatalf("seed bad/C: %v", err)
	}

	// 用密文根把 bad/C 覆写成非密文垃圾（与真实损坏/旧格式条目等效）
	root, err := securefs.OpenRoot(mgr.dataPath)
	if err != nil {
		t.Fatalf("open data root: %v", err)
	}
	if err := root.AtomicWrite([]string{TextDirName, "bad", "C" + TextFileSuffix}, []byte("not-encrypted"), 0o600); err != nil {
		t.Fatalf("corrupt bad/C: %v", err)
	}
	_ = root.Close()

	// 逐条路径对该条目同样失败（降级不是批量路径放宽校验）
	if _, err := mgr.LoadTextFileWithKey("bad", "C", key); err == nil {
		t.Fatal("per-entry load of corrupt entry should fail")
	}

	snap, err := mgr.LoadTextVaultWithKey(key)
	if err != nil {
		t.Fatalf("LoadTextVaultWithKey: %v", err)
	}
	if len(snap.Entries["good"]) != 1 || snap.Entries["good"][0].Entry.Value != "1" {
		t.Fatalf("good group = %+v, want one entry value 1", snap.Entries["good"])
	}
	if _, ok := snap.Entries["bad"]; ok {
		t.Fatal("corrupt group must not contribute entries")
	}
	if snap.KeyCount["bad"] != 2 {
		t.Fatalf("bad KeyCount = %d, want 2 (listed files)", snap.KeyCount["bad"])
	}
	if snap.Errors["bad"] == nil {
		t.Fatal("bad group error must be recorded")
	}
	found := false
	for _, name := range snap.Groups {
		if name == "bad" {
			found = true
		}
	}
	if !found {
		t.Fatal("bad group must remain listed")
	}
}
