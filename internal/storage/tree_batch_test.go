//go:build unix

package storage

import "testing"

func TestLoadTreeGroupMatchesPerFile(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	if err := m.SaveTreeGroupMeta("deploy", "manifests", key); err != nil {
		t.Fatal(err)
	}
	type payload struct {
		Content string `json:"content"`
	}
	want := map[string]string{
		"BACKUP.md":            "# backup",
		"tcbj/caddy/Caddyfile": "example.com",
		"wire/bin.dat":         "abc",
	}
	for path, content := range want {
		if err := m.SaveTreeJSON("deploy", path, payload{Content: content}, key); err != nil {
			t.Fatalf("save %s: %v", path, err)
		}
	}

	got, err := m.LoadTreeGroupWithKey("deploy", key)
	if err != nil {
		t.Fatalf("LoadTreeGroupWithKey: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("files = %d, want %d", len(got), len(want))
	}
	for _, f := range got {
		var batch payload
		if err := FromJSON(f.Plaintext, &batch); err != nil {
			t.Fatalf("parse %s: %v", f.Path, err)
		}
		var per payload
		if err := m.LoadTreeJSON("deploy", f.Path, &per, key); err != nil {
			t.Fatalf("LoadTreeJSON %s: %v", f.Path, err)
		}
		if batch.Content != want[f.Path] || batch.Content != per.Content {
			t.Fatalf("%s content batch=%q per=%q want=%q", f.Path, batch.Content, per.Content, want[f.Path])
		}
	}

	missing, err := m.LoadTreeGroupWithKey("absent", key)
	if err != nil {
		t.Fatalf("missing group: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing group files = %d, want 0", len(missing))
	}
}

func TestLoadTreeGroupAcquiresSingleLock(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	if err := m.SaveTreeGroupMeta("deploy", "manifests", key); err != nil {
		t.Fatal(err)
	}
	const n = 8
	for i := 0; i < n; i++ {
		path := "d/f" + string(rune('a'+i)) + ".txt"
		if err := m.SaveTreeJSON("deploy", path, map[string]string{"content": "x"}, key); err != nil {
			t.Fatal(err)
		}
	}

	vaultLockAcquires.Store(0)
	files, err := m.LoadTreeGroupWithKey("deploy", key)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != n {
		t.Fatalf("files = %d, want %d", len(files), n)
	}
	if got := vaultLockAcquires.Load(); got != 1 {
		t.Fatalf("batch load acquired vault lock %d times, want 1", got)
	}
}
