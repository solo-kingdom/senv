package tests

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
	"golang.org/x/crypto/pbkdf2"
)

// setupIntegrationTest creates a full test environment with initialized storage
func setupIntegrationTest(t *testing.T) (*storage.Manager, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "senv-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config")
	dataPath := filepath.Join(tmpDir, "data")

	storeMgr := storage.NewManager(configPath, dataPath)
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	return storeMgr, tmpDir
}

// combinedGetter adapts env and text managers to implement ref.ValueGetter
type testGetter struct {
	envMgr  *env.Manager
	textMgr *text.Manager
}

func (g *testGetter) GetEnvValue(group, key string) (string, error) {
	return g.envMgr.Get(group, key)
}

func (g *testGetter) GetTextValue(group, key string) (string, error) {
	return g.textMgr.Get(group, key)
}

func TestIntegrationTextFullLifecycle(t *testing.T) {
	storeMgr, tmpDir := setupIntegrationTest(t)
	password := "test-password"

	textMgr := text.NewManager(storeMgr, password)
	_ = tmpDir

	// Step 1: Create a text group
	err := textMgr.AddGroup("notes")
	if err != nil {
		t.Fatalf("AddGroup failed: %v", err)
	}

	// Step 2: Set text via value
	err = textMgr.Set("notes", "README", "# My Project\nThis is a test.")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Step 3: Get text
	value, err := textMgr.Get("notes", "README")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !strings.Contains(value, "# My Project") {
		t.Errorf("Expected project header, got '%s'", value)
	}

	// Step 4: Set text from file
	srcFile := filepath.Join(tmpDir, "source.txt")
	content := "File content\nLine 2\nLine 3\n"
	os.WriteFile(srcFile, []byte(content), 0o644)

	err = textMgr.SetFromFile("notes", "FROM_FILE", srcFile)
	if err != nil {
		t.Fatalf("SetFromFile failed: %v", err)
	}

	value, err = textMgr.Get("notes", "FROM_FILE")
	if err != nil {
		t.Fatalf("Get FROM_FILE failed: %v", err)
	}
	if value != content {
		t.Errorf("Expected file content, got '%s'", value)
	}

	// Step 5: Set text from reader
	err = textMgr.SetFromReader("notes", "FROM_READER", strings.NewReader("reader value"))
	if err != nil {
		t.Fatalf("SetFromReader failed: %v", err)
	}

	// Step 6: List texts
	infos, err := textMgr.List("notes")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(infos) != 3 {
		t.Errorf("Expected 3 texts, got %d", len(infos))
	}

	// Step 7: Get to file
	outputPath := filepath.Join(tmpDir, "output.txt")
	err = textMgr.GetToFile("notes", "README", outputPath)
	if err != nil {
		t.Fatalf("GetToFile failed: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}
	if !strings.Contains(string(data), "# My Project") {
		t.Errorf("Output file should contain project header")
	}

	// Step 8: Update existing text
	err = textMgr.Set("notes", "README", "# Updated Project")
	if err != nil {
		t.Fatalf("Set (update) failed: %v", err)
	}
	value, err = textMgr.Get("notes", "README")
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if value != "# Updated Project" {
		t.Errorf("Expected updated value, got '%s'", value)
	}

	// Step 9: Delete text
	err = textMgr.Delete("notes", "FROM_FILE")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = textMgr.Get("notes", "FROM_FILE")
	if err == nil {
		t.Error("Should error after delete")
	}

	// Step 10: List groups
	groups, err := textMgr.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "notes" {
		t.Errorf("Expected 1 group 'notes', got %v", groups)
	}

	// Step 11: Delete group
	err = textMgr.DeleteGroup("notes")
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}
	groups, err = textMgr.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups after delete failed: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("Expected 0 groups after delete, got %d", len(groups))
	}
}

func TestIntegrationEnvTextReferences(t *testing.T) {
	storeMgr, _ := setupIntegrationTest(t)
	password := "test-password"

	envMgr := env.NewManager(storeMgr, password)
	textMgr := text.NewManager(storeMgr, password)
	getter := &testGetter{envMgr: envMgr, textMgr: textMgr}

	// Setup: create text values
	textMgr.Set("secrets", "DB_PASS", "hunter2")
	textMgr.Set("secrets", "API_KEY", "sk-12345")

	// Setup: create env values with text references
	envMgr.Set("default", "DB_URL", "postgres://user:{{text:secrets:DB_PASS}}@host/db")
	envMgr.Set("default", "CONFIG", "key={{text:secrets:API_KEY}}&env={{env:default:NODE_ENV}}")
	envMgr.Set("default", "NODE_ENV", "production")

	// Test 1: env references text
	result, err := ref.Resolve("{{env:default:DB_URL}}", getter, ref.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve DB_URL failed: %v", err)
	}
	if result != "postgres://user:hunter2@host/db" {
		t.Errorf("Expected resolved URL, got '%s'", result)
	}

	// Test 2: nested references (env -> text -> env)
	result, err = ref.Resolve("{{env:default:CONFIG}}", getter, ref.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve CONFIG failed: %v", err)
	}
	if result != "key=sk-12345&env=production" {
		t.Errorf("Expected resolved config, got '%s'", result)
	}

	// Test 3: text references env
	textMgr.Set("templates", "APP_YAML", "database:\n  url: {{env:default:DB_URL}}")
	result, err = ref.Resolve("{{text:templates:APP_YAML}}", getter, ref.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve APP_YAML failed: %v", err)
	}
	if !strings.Contains(result, "hunter2") {
		t.Errorf("Expected resolved URL in YAML, got '%s'", result)
	}

	// Test 4: circular reference detection
	envMgr.Set("default", "A", "{{env:default:B}}")
	envMgr.Set("default", "B", "{{env:default:A}}")

	_, err = ref.Resolve("{{env:default:A}}", getter, ref.ResolveOptions{})
	if err == nil {
		t.Error("Should detect circular reference")
	}

	// Test 5: strict mode - unresolved reference
	_, err = ref.Resolve("{{text:nonexistent:KEY}}", getter, ref.ResolveOptions{})
	if err == nil {
		t.Error("Should error in strict mode on unresolved reference")
	}

	// Test 6: loose mode - unresolved reference
	result, warnings, err := ref.ResolveWithWarnings("{{text:nonexistent:KEY}}", getter, ref.ResolveOptions{Loose: true})
	if err != nil {
		t.Fatalf("Should not error in loose mode: %v", err)
	}
	if result != "{{text:nonexistent:KEY}}" {
		t.Errorf("Expected preserved reference in loose mode, got '%s'", result)
	}
	if len(warnings) == 0 {
		t.Error("Expected warnings in loose mode")
	}

	// Test 7: escaped reference
	result, err = ref.Resolve(`password is \{{env:secrets:PASS}}`, getter, ref.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve escaped failed: %v", err)
	}
	if result != "password is {{env:secrets:PASS}}" {
		t.Errorf("Expected escaped output, got '%s'", result)
	}
}

func TestIntegrationImplicitGroupResolution(t *testing.T) {
	storeMgr, _ := setupIntegrationTest(t)
	password := "test-password"

	envMgr := env.NewManager(storeMgr, password)
	textMgr := text.NewManager(storeMgr, password)
	getter := &testGetter{envMgr: envMgr, textMgr: textMgr}

	// Set value in "staging" env group
	envMgr.Set("staging", "HOST", "staging.example.com")

	// Reference with implicit group (should use CurrentGroup)
	result, err := ref.Resolve("{{env:HOST}}", getter, ref.ResolveOptions{CurrentGroup: "staging"})
	if err != nil {
		t.Fatalf("Resolve with implicit group failed: %v", err)
	}
	if result != "staging.example.com" {
		t.Errorf("Expected staging host, got '%s'", result)
	}

	// Reference with explicit group overrides CurrentGroup
	envMgr.Set("default", "HOST", "default.example.com")
	result, err = ref.Resolve("{{env:HOST}}", getter, ref.ResolveOptions{CurrentGroup: "staging"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	// Current group takes priority
	if result != "staging.example.com" {
		t.Errorf("Expected staging (current group priority), got '%s'", result)
	}
}

func TestIntegrationManagerWithKey(t *testing.T) {
	storeMgr, _ := setupIntegrationTest(t)
	password := "test-password"

	// Create managers using password
	textMgr := text.NewManager(storeMgr, password)
	textMgr.Set("notes", "KEY", "value")

	// Get the derived key
	metadata, _ := storeMgr.LoadMetadata()
	saltBytes, _ := base64.StdEncoding.DecodeString(metadata.Salt)
	key := pbkdf2.Key([]byte(password), saltBytes, crypto.Iterations, crypto.KeySize, sha256.New)

	// Create manager using key
	textMgrWithKey := text.NewManagerWithKey(storeMgr, key)

	// Should be able to read what was written with password
	value, err := textMgrWithKey.Get("notes", "KEY")
	if err != nil {
		t.Fatalf("Get with key failed: %v", err)
	}
	if value != "value" {
		t.Errorf("Expected 'value', got '%s'", value)
	}

	// Should be able to write with key and read with password
	textMgrWithKey.Set("notes", "KEY2", "value2")
	value, err = textMgr.Get("notes", "KEY2")
	if err != nil {
		t.Fatalf("Get with password failed: %v", err)
	}
	if value != "value2" {
		t.Errorf("Expected 'value2', got '%s'", value)
	}
}
