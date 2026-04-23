package ref

import (
	"fmt"
	"strings"
	"testing"
)

// mockGetter implements ValueGetter for testing
type mockGetter struct {
	envValues  map[string]map[string]string // group -> key -> value
	textValues map[string]map[string]string // group -> key -> value
}

func newMockGetter() *mockGetter {
	return &mockGetter{
		envValues:  make(map[string]map[string]string),
		textValues: make(map[string]map[string]string),
	}
}

func (m *mockGetter) setEnv(group, key, value string) {
	if m.envValues[group] == nil {
		m.envValues[group] = make(map[string]string)
	}
	m.envValues[group][key] = value
}

func (m *mockGetter) setText(group, key, value string) {
	if m.textValues[group] == nil {
		m.textValues[group] = make(map[string]string)
	}
	m.textValues[group][key] = value
}

func (m *mockGetter) GetEnvValue(group, key string) (string, error) {
	if g, ok := m.envValues[group]; ok {
		if v, ok := g[key]; ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func (m *mockGetter) GetTextValue(group, key string) (string, error) {
	if g, ok := m.textValues[group]; ok {
		if v, ok := g[key]; ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func TestResolveSimpleEnvRef(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "NAME", "Alice")

	result, err := Resolve("Hello {{env:NAME}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "Hello Alice" {
		t.Errorf("Expected 'Hello Alice', got '%s'", result)
	}
}

func TestResolveSimpleTextRef(t *testing.T) {
	getter := newMockGetter()
	getter.setText("secrets", "PASS", "s3cr3t")

	result, err := Resolve("pass={{text:secrets:PASS}}", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "pass=s3cr3t" {
		t.Errorf("Expected 'pass=s3cr3t', got '%s'", result)
	}
}

func TestResolveWithExplicitGroup(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("prod", "HOST", "prod.example.com")

	result, err := Resolve("{{env:prod:HOST}}", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "prod.example.com" {
		t.Errorf("Expected 'prod.example.com', got '%s'", result)
	}
}

func TestResolveImplicitGroupUsesCurrentGroup(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("staging", "URL", "staging.example.com")

	result, err := Resolve("{{env:URL}}", getter, ResolveOptions{CurrentGroup: "staging"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "staging.example.com" {
		t.Errorf("Expected 'staging.example.com', got '%s'", result)
	}
}

func TestResolveNoRefs(t *testing.T) {
	getter := newMockGetter()

	result, err := Resolve("plain text", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "plain text" {
		t.Errorf("Expected 'plain text', got '%s'", result)
	}
}

func TestResolveNoTypePrefixIsLiteral(t *testing.T) {
	getter := newMockGetter()

	result, err := Resolve("{{not_a_ref}}", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "{{not_a_ref}}" {
		t.Errorf("Expected '{{not_a_ref}}', got '%s'", result)
	}
}

func TestResolveEscapedRef(t *testing.T) {
	getter := newMockGetter()

	result, err := Resolve(`password is \{{env:secrets:PASS}}`, getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != `password is {{env:secrets:PASS}}` {
		t.Errorf("Expected escaped output, got '%s'", result)
	}
}

func TestResolveMixedEscapedAndReal(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "KEY", "value")

	result, err := Resolve(`escaped: \{{env:KEY}}, real: {{env:KEY}}`, getter, ResolveOptions{CurrentGroup: "default"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != `escaped: {{env:KEY}}, real: value` {
		t.Errorf("Expected mixed output, got '%s'", result)
	}
}

func TestResolveNestedRefs(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "GREETING", "Hello {{env:NAME}}")
	getter.setEnv("default", "NAME", "{{text:secrets:USER}}")
	getter.setText("secrets", "USER", "Alice")

	result, err := Resolve("{{env:GREETING}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "Hello Alice" {
		t.Errorf("Expected 'Hello Alice', got '%s'", result)
	}
}

func TestResolveMixedEnvText(t *testing.T) {
	getter := newMockGetter()
	getter.setText("secrets", "USER", "admin")
	getter.setText("secrets", "PASS", "s3cr3t")

	result, err := Resolve("postgres://{{text:secrets:USER}}:{{text:secrets:PASS}}@host/db", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "postgres://admin:s3cr3t@host/db" {
		t.Errorf("Expected full URL, got '%s'", result)
	}
}

func TestResolveStrictMode(t *testing.T) {
	getter := newMockGetter()

	_, err := Resolve("{{env:prod:NONEXISTENT}}", getter, ResolveOptions{})
	if err == nil {
		t.Error("Should error in strict mode when reference not found")
	}
}

func TestResolveLooseMode(t *testing.T) {
	getter := newMockGetter()

	result, warnings, err := ResolveWithWarnings("value={{env:prod:MISSING}}", getter, ResolveOptions{Loose: true})
	if err != nil {
		t.Fatalf("Should not error in loose mode: %v", err)
	}
	if result != "value={{env:prod:MISSING}}" {
		t.Errorf("Expected reference preserved, got '%s'", result)
	}
	if len(warnings) == 0 {
		t.Error("Expected warnings in loose mode")
	}
}

func TestResolveDirectCircular(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "A", "{{env:B}}")
	getter.setEnv("default", "B", "{{env:A}}")

	_, err := Resolve("{{env:A}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err == nil {
		t.Error("Should detect circular reference")
	}
	if _, ok := err.(*RefError); !ok {
		t.Errorf("Expected RefError, got %T: %v", err, err)
	}
}

func TestResolveIndirectCircular(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "A", "{{env:B}}")
	getter.setEnv("default", "B", "{{env:C}}")
	getter.setEnv("default", "C", "{{env:A}}")

	_, err := Resolve("{{env:A}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err == nil {
		t.Error("Should detect indirect circular reference")
	}
}

func TestResolveMaxDepth(t *testing.T) {
	getter := newMockGetter()
	// Create a chain longer than MaxDepth
	for i := 0; i < 15; i++ {
		if i < 14 {
			getter.setEnv("default", fmt.Sprintf("KEY_%d", i), fmt.Sprintf("{{env:KEY_%d}}", i+1))
		} else {
			getter.setEnv("default", fmt.Sprintf("KEY_%d", i), "final")
		}
	}

	_, err := Resolve("{{env:KEY_0}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err == nil {
		t.Error("Should error when max depth exceeded")
	}
	if !strings.Contains(err.Error(), "maximum reference depth") {
		t.Errorf("Expected max depth error, got: %v", err)
	}
}

func TestResolveMultipleRefsInOneValue(t *testing.T) {
	getter := newMockGetter()
	getter.setEnv("default", "A", "1")
	getter.setEnv("default", "B", "2")
	getter.setEnv("default", "C", "3")

	result, err := Resolve("{{env:A}}-{{env:B}}-{{env:C}}", getter, ResolveOptions{CurrentGroup: "default"})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "1-2-3" {
		t.Errorf("Expected '1-2-3', got '%s'", result)
	}
}

func TestParseRefPath(t *testing.T) {
	tests := []struct {
		path         string
		currentGroup string
		wantGroup    string
		wantKey      string
	}{
		{"KEY", "default", "default", "KEY"},
		{"prod:KEY", "default", "prod", "KEY"},
		{"secrets:DB_PASS", "default", "secrets", "DB_PASS"},
		{"KEY", "staging", "staging", "KEY"},
	}

	for _, tt := range tests {
		group, key := parseRefPath(tt.path, tt.currentGroup)
		if group != tt.wantGroup || key != tt.wantKey {
			t.Errorf("parseRefPath(%s, %s) = (%s, %s), want (%s, %s)",
				tt.path, tt.currentGroup, group, key, tt.wantGroup, tt.wantKey)
		}
	}
}

func TestHasReferences(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"plain text", false},
		{"{{env:KEY}}", true},
		{"{{text:secrets:KEY}}", true},
		{"{{not_a_ref}}", false},
		{`\{{env:KEY}}`, true}, // HasReferences sees the pattern, but Resolve handles escaping
		{"mix {{env:KEY}} and text", true},
	}

	for _, tt := range tests {
		got := HasReferences(tt.value)
		if got != tt.want {
			t.Errorf("HasReferences(%s) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestGetReferences(t *testing.T) {
	value := "{{env:KEY1}} and {{text:secrets:KEY2}} and {{env:KEY3}}"
	refs := GetReferences(value)

	if len(refs) != 3 {
		t.Fatalf("Expected 3 refs, got %d", len(refs))
	}

	expected := []string{"env:KEY1", "text:secrets:KEY2", "env:KEY3"}
	for i, exp := range expected {
		if refs[i] != exp {
			t.Errorf("Expected ref[%d] = '%s', got '%s'", i, exp, refs[i])
		}
	}
}

func TestResolveGroupNotFound(t *testing.T) {
	getter := newMockGetter()

	_, err := Resolve("{{env:unknown_group:KEY}}", getter, ResolveOptions{})
	if err == nil {
		t.Error("Should error when group not found")
	}
}

func TestResolveEnvRefText(t *testing.T) {
	// env references text
	getter := newMockGetter()
	getter.setText("secrets", "DB_PASS", "hunter2")

	result, err := Resolve("postgres://user:{{text:secrets:DB_PASS}}@host/db", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "postgres://user:hunter2@host/db" {
		t.Errorf("Expected URL with password, got '%s'", result)
	}
}

func TestResolveTextRefEnv(t *testing.T) {
	// text references env
	getter := newMockGetter()
	getter.setEnv("prod", "DATABASE_URL", "postgres://host/db")

	result, err := Resolve("url: {{env:prod:DATABASE_URL}}", getter, ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result != "url: postgres://host/db" {
		t.Errorf("Expected URL, got '%s'", result)
	}
}
