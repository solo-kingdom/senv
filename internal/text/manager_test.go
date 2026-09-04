package text

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// setupTestTextManager creates a temporary text manager for testing
func setupTestTextManager(t *testing.T) (*Manager, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "senv-text-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config")
	dataPath := filepath.Join(tmpDir, "data")

	storeMgr := storage.NewManager(configPath, dataPath)
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	mgr := NewManager(storeMgr, "test-password")

	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return mgr, tmpDir
}

func TestSetAndGet(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	err := mgr.Set("notes", "README", "hello world")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	value, err := mgr.Get("notes", "README")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", value)
	}
}

func TestSetOverwrite(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.Set("notes", "KEY", "original")
	mgr.Set("notes", "KEY", "updated")

	value, err := mgr.Get("notes", "KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "updated" {
		t.Errorf("Expected 'updated', got '%s'", value)
	}
}

func TestGetNotFound(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	_, err := mgr.Get("notes", "NONEXISTENT")
	if err == nil {
		t.Error("Should error when getting nonexistent text")
	}
}

func TestSetExceedsSizeLimit(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	// Create a value that exceeds 512KB
	largeValue := strings.Repeat("x", storage.MaxTextSize+1)

	err := mgr.Set("notes", "LARGE", largeValue)
	if err == nil {
		t.Error("Should error when value exceeds size limit")
	}
}

func TestSetAtSizeLimit(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	// Create a value exactly at 512KB
	exactValue := strings.Repeat("x", storage.MaxTextSize)

	err := mgr.Set("notes", "EXACT", exactValue)
	if err != nil {
		t.Errorf("Should not error at exact size limit: %v", err)
	}

	value, err := mgr.Get("notes", "EXACT")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(value) != storage.MaxTextSize {
		t.Errorf("Expected size %d, got %d", storage.MaxTextSize, len(value))
	}
}

func TestSetMultilineValue(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	value := "line1\nline2\nline3\n"
	err := mgr.Set("notes", "MULTI", value)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := mgr.Get("notes", "MULTI")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got != value {
		t.Errorf("Expected '%s', got '%s'", value, got)
	}
}

func TestDelete(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.Set("notes", "TEMP", "to be deleted")

	err := mgr.Delete("notes", "TEMP")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = mgr.Get("notes", "TEMP")
	if err == nil {
		t.Error("Should error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	err := mgr.Delete("notes", "NONEXISTENT")
	if err == nil {
		t.Error("Should error when deleting nonexistent text")
	}
}

func TestList(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.Set("notes", "README", "content 1")
	mgr.Set("notes", "TODO", "content 2")
	mgr.Set("notes", "NOTES", "content 3")

	infos, err := mgr.List("notes")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(infos) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(infos))
	}

	keyMap := map[string]bool{}
	for _, info := range infos {
		keyMap[info.Key] = true
		if info.Size == 0 {
			t.Errorf("Size should not be zero for key '%s'", info.Key)
		}
		if info.UpdatedAt.IsZero() {
			t.Errorf("UpdatedAt should not be zero for key '%s'", info.Key)
		}
	}

	for _, expected := range []string{"README", "TODO", "NOTES"} {
		if !keyMap[expected] {
			t.Errorf("Expected key '%s' not found", expected)
		}
	}
}

func TestListEmptyGroup(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	infos, err := mgr.List("nonexistent")
	if err != nil {
		t.Fatalf("List on empty group should not error: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(infos))
	}
}

func TestSetFromFile(t *testing.T) {
	mgr, tmpDir := setupTestTextManager(t)

	// Create a temp source file
	srcFile := filepath.Join(tmpDir, "source.txt")
	content := "file content\nline 2\n"
	os.WriteFile(srcFile, []byte(content), 0o644)

	err := mgr.SetFromFile("notes", "FROM_FILE", srcFile)
	if err != nil {
		t.Fatalf("SetFromFile failed: %v", err)
	}

	value, err := mgr.Get("notes", "FROM_FILE")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != content {
		t.Errorf("Expected '%s', got '%s'", content, value)
	}
}

func TestSetFromFileNotFound(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	err := mgr.SetFromFile("notes", "KEY", "/nonexistent/file.txt")
	if err == nil {
		t.Error("Should error when source file not found")
	}
}

func TestSetFromReader(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	reader := strings.NewReader("reader content")

	err := mgr.SetFromReader("notes", "FROM_READER", reader)
	if err != nil {
		t.Fatalf("SetFromReader failed: %v", err)
	}

	value, err := mgr.Get("notes", "FROM_READER")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if value != "reader content" {
		t.Errorf("Expected 'reader content', got '%s'", value)
	}
}

func TestAddGroup(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	err := mgr.AddGroup("secrets")
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}

	// Verify by saving a text to the new group
	err = mgr.Set("secrets", "KEY", "value")
	if err != nil {
		t.Fatalf("Set to new group failed: %v", err)
	}
}

func TestAddGroupDuplicate(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.AddGroup("test-group")

	err := mgr.AddGroup("test-group")
	if err == nil {
		t.Error("Should error when adding duplicate group")
	}
}

func TestDeleteGroup(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.Set("test-group", "KEY1", "value1")
	mgr.Set("test-group", "KEY2", "value2")

	err := mgr.DeleteGroup("test-group")
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	// Verify group is gone
	_, err = mgr.Get("test-group", "KEY1")
	if err == nil {
		t.Error("Should error after group is deleted")
	}
}

func TestDeleteGroupNotFound(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	err := mgr.DeleteGroup("nonexistent")
	if err == nil {
		t.Error("Should error when deleting nonexistent group")
	}
}

func TestListGroups(t *testing.T) {
	mgr, _ := setupTestTextManager(t)

	mgr.Set("notes", "KEY1", "v1")
	mgr.Set("keys", "KEY2", "v2")
	mgr.Set("templates", "KEY3", "v3")

	groups, err := mgr.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}

	if len(groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(groups))
	}

	groupMap := map[string]int{}
	for _, g := range groups {
		groupMap[g.Name] = g.KeyCount
	}

	if groupMap["notes"] != 1 {
		t.Errorf("Expected notes to have 1 key, got %d", groupMap["notes"])
	}
}

func TestGetToFile(t *testing.T) {
	mgr, tmpDir := setupTestTextManager(t)

	mgr.Set("notes", "README", "file output content")

	outputPath := filepath.Join(tmpDir, "output.txt")
	err := mgr.GetToFile("notes", "README", outputPath)
	if err != nil {
		t.Fatalf("GetToFile failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if string(data) != "file output content" {
		t.Errorf("Expected 'file output content', got '%s'", string(data))
	}
}

func TestGetEditor(t *testing.T) {
	editor := getEditor()
	if editor == "" {
		t.Error("getEditor should return a non-empty string")
	}
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		input    string
		hasHome  bool
		expected string
	}{
		{"~/test", true, ""},
		{"/absolute/path", false, "/absolute/path"},
		{"relative/path", false, "relative/path"},
	}

	for _, tt := range tests {
		result := expandHome(tt.input)
		if tt.hasHome {
			if !strings.HasPrefix(result, "/") || !strings.Contains(result, "test") {
				t.Errorf("expandHome(%s) should expand home dir, got %s", tt.input, result)
			}
		} else {
			if result != tt.expected {
				t.Errorf("expandHome(%s) = %s, expected %s", tt.input, result, tt.expected)
			}
		}
	}
}

func TestTextExportPathsAndPermissions(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("notes", "PRIVATE", "secret-data"); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	absolute := filepath.Join(t.TempDir(), "absolute", "secret.txt")
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "basename", raw: "secret.txt", want: filepath.Join(cwd, "secret.txt")},
		{name: "relative", raw: filepath.Join("relative", "nested", "secret.txt"), want: filepath.Join(cwd, "relative", "nested", "secret.txt")},
		{name: "absolute", raw: absolute, want: absolute},
		{name: "home", raw: "~/keys/id.pem", want: filepath.Join(home, "keys", "id.pem")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := mgr.GetToFile("notes", "PRIVATE", tt.raw); err != nil {
				t.Fatalf("GetToFile(%q): %v", tt.raw, err)
			}
			assertTextExport(t, tt.want, "secret-data", 0o600)
		})
	}
	assertTextDirMode(t, filepath.Join(cwd, "relative"), 0o700)
	assertTextDirMode(t, filepath.Join(cwd, "relative", "nested"), 0o700)
}

func TestTextExportFileModes(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("notes", "VALUE", "new"); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit 0644 new file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.txt")
		if err := mgr.GetToFileWithMode("notes", "VALUE", path, 0o644); err != nil {
			t.Fatal(err)
		}
		assertTextExport(t, path, "new", 0o644)
	})

	for _, tt := range []struct {
		name      string
		existing  os.FileMode
		requested os.FileMode
		want      os.FileMode
	}{
		{name: "default tightens loose", existing: 0o644, requested: 0o600, want: 0o600},
		{name: "default preserves stricter", existing: 0o400, requested: 0o600, want: 0o400},
		{name: "explicit mode never widens existing", existing: 0o600, requested: 0o644, want: 0o600},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.txt")
			if err := os.WriteFile(path, []byte("old"), tt.existing); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.existing); err != nil {
				t.Fatal(err)
			}
			if err := mgr.GetToFileWithMode("notes", "VALUE", path, tt.requested); err != nil {
				t.Fatal(err)
			}
			assertTextExport(t, path, "new", tt.want)
		})
	}
}

func TestTextExportRejectsSymlinks(t *testing.T) {
	mgr, _ := setupTestTextManager(t)
	if err := mgr.Set("notes", "VALUE", "new-secret"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("target", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := mgr.GetToFile("notes", "VALUE", target); err == nil {
			t.Fatal("export through target symlink succeeded")
		}
	})

	t.Run("parent", func(t *testing.T) {
		dir := t.TempDir()
		parent := filepath.Join(dir, "linked")
		if err := os.Symlink(filepath.Dir(outside), parent); err != nil {
			t.Fatal(err)
		}
		if err := mgr.GetToFile("notes", "VALUE", filepath.Join(parent, "outside")); err == nil {
			t.Fatal("export through parent symlink succeeded")
		}
	})

	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside target changed: data=%q err=%v", data, err)
	}
}

func assertTextExport(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != content {
		t.Fatalf("content of %q = %q, want %q", path, data, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("mode of %q = %04o, want %04o", path, info.Mode().Perm(), mode.Perm())
	}
}

func assertTextDirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("directory mode of %q = %04o, want %04o", path, info.Mode().Perm(), mode.Perm())
	}
}
