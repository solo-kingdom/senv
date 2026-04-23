package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestManager creates a temporary storage manager for testing
func setupTestManager(t *testing.T) (*Manager, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "senv-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config")
	dataPath := filepath.Join(tmpDir, "data")

	mgr := NewManager(configPath, dataPath)

	// Initialize with a known password
	if err := mgr.Initialize("test-password"); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return mgr, tmpDir
}

func TestSaveAndLoadTextFile(t *testing.T) {
	mgr, _ := setupTestManager(t)

	entry := NewTextEntry("hello world")

	// Save
	err := mgr.SaveTextFile("notes", "README", entry, "test-password")
	if err != nil {
		t.Fatalf("SaveTextFile failed: %v", err)
	}

	// Verify file exists
	path := mgr.textFilePath("notes", "README")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Text file should exist after save")
	}

	// Load
	loaded, err := mgr.LoadTextFile("notes", "README", "test-password")
	if err != nil {
		t.Fatalf("LoadTextFile failed: %v", err)
	}

	if loaded.Value != "hello world" {
		t.Errorf("Expected value 'hello world', got '%s'", loaded.Value)
	}
	if loaded.Size != len("hello world") {
		t.Errorf("Expected size %d, got %d", len("hello world"), loaded.Size)
	}
	if loaded.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestLoadTextFileNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)

	_, err := mgr.LoadTextFile("notes", "NONEXISTENT", "test-password")
	if err == nil {
		t.Error("Should error when loading nonexistent text")
	}
}

func TestLoadTextFileGroupNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)

	_, err := mgr.LoadTextFile("nonexistent-group", "KEY", "test-password")
	if err == nil {
		t.Error("Should error when loading from nonexistent group")
	}
}

func TestDeleteTextFile(t *testing.T) {
	mgr, _ := setupTestManager(t)

	entry := NewTextEntry("to be deleted")
	err := mgr.SaveTextFile("notes", "TEMP", entry, "test-password")
	if err != nil {
		t.Fatalf("SaveTextFile failed: %v", err)
	}

	// Delete
	err = mgr.DeleteTextFile("notes", "TEMP")
	if err != nil {
		t.Fatalf("DeleteTextFile failed: %v", err)
	}

	// Verify file is gone
	_, err = mgr.LoadTextFile("notes", "TEMP", "test-password")
	if err == nil {
		t.Error("Should error when loading deleted text")
	}
}

func TestDeleteTextFileNotExist(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Deleting nonexistent file should not error
	err := mgr.DeleteTextFile("notes", "NONEXISTENT")
	if err != nil {
		t.Errorf("DeleteTextFile should not error on nonexistent file: %v", err)
	}
}

func TestListTextFiles(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create multiple texts
	for _, key := range []string{"README", "TODO", "NOTES"} {
		entry := NewTextEntry("content of " + key)
		err := mgr.SaveTextFile("notes", key, entry, "test-password")
		if err != nil {
			t.Fatalf("SaveTextFile failed for %s: %v", key, err)
		}
	}

	keys, err := mgr.ListTextFiles("notes")
	if err != nil {
		t.Fatalf("ListTextFiles failed: %v", err)
	}

	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Check all keys exist (order not guaranteed)
	keyMap := map[string]bool{}
	for _, k := range keys {
		keyMap[k] = true
	}
	for _, expected := range []string{"README", "TODO", "NOTES"} {
		if !keyMap[expected] {
			t.Errorf("Expected key '%s' not found", expected)
		}
	}
}

func TestListTextFilesEmptyGroup(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Group doesn't exist
	keys, err := mgr.ListTextFiles("nonexistent")
	if err != nil {
		t.Fatalf("ListTextFiles on nonexistent group should not error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("Expected 0 keys, got %d", len(keys))
	}
}

func TestListTextGroups(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create groups by saving texts
	for _, group := range []string{"notes", "keys", "templates"} {
		entry := NewTextEntry("test")
		err := mgr.SaveTextFile(group, "TEST", entry, "test-password")
		if err != nil {
			t.Fatalf("SaveTextFile failed for group %s: %v", group, err)
		}
	}

	groups, err := mgr.ListTextGroups()
	if err != nil {
		t.Fatalf("ListTextGroups failed: %v", err)
	}

	if len(groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(groups))
	}

	groupMap := map[string]bool{}
	for _, g := range groups {
		groupMap[g] = true
	}
	for _, expected := range []string{"notes", "keys", "templates"} {
		if !groupMap[expected] {
			t.Errorf("Expected group '%s' not found", expected)
		}
	}
}

func TestListTextGroupsEmpty(t *testing.T) {
	mgr, _ := setupTestManager(t)

	groups, err := mgr.ListTextGroups()
	if err != nil {
		t.Fatalf("ListTextGroups on empty should not error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("Expected 0 groups, got %d", len(groups))
	}
}

func TestNewTextEntry(t *testing.T) {
	entry := NewTextEntry("test content")

	if entry.Value != "test content" {
		t.Errorf("Expected value 'test content', got '%s'", entry.Value)
	}
	if entry.Size != len("test content") {
		t.Errorf("Expected size %d, got %d", len("test content"), entry.Size)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if entry.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestTextEntryUpdatePreservesCreatedAt(t *testing.T) {
	entry := NewTextEntry("original")
	originalCreatedAt := entry.CreatedAt

	// Simulate update
	entry.Value = "updated"
	entry.Size = len("updated")

	if !entry.CreatedAt.Equal(originalCreatedAt) {
		t.Error("CreatedAt should be preserved on update")
	}
}

func TestMaxTextSize(t *testing.T) {
	if MaxTextSize != 512*1024 {
		t.Errorf("Expected MaxTextSize to be %d, got %d", 512*1024, MaxTextSize)
	}
}
