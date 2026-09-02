package storage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
)

func TestNewMetadata(t *testing.T) {
	salt := "dGVzdC1zYWx0"        // base64 encoded "test-salt"
	passwordKey := "dGVzdC1rZXk=" // base64 encoded "test-key"

	metadata := NewMetadata(salt, passwordKey)

	if metadata.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", metadata.Version)
	}

	if metadata.Salt != salt {
		t.Errorf("Expected salt %s, got %s", salt, metadata.Salt)
	}

	if metadata.PasswordKey != passwordKey {
		t.Errorf("Expected passwordKey %s, got %s", passwordKey, metadata.PasswordKey)
	}

	if metadata.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	if metadata.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}

	if metadata.KDFIterations != crypto.DefaultIterations {
		t.Errorf("Expected KDFIterations %d, got %d", crypto.DefaultIterations, metadata.KDFIterations)
	}
}

func TestEffectiveIterations(t *testing.T) {
	// New-style metadata records the iteration count explicitly.
	md := &Metadata{KDFIterations: 600000}
	if got := md.EffectiveIterations(); got != 600000 {
		t.Errorf("EffectiveIterations() = %d, want 600000", got)
	}

	// Legacy metadata has no field; MUST be interpreted as 100000.
	legacyJSON := `{"version":"1.0","salt":"dGVzdC1zYWx0","password_key":"dGVzdC1rZXk="}`
	var legacy Metadata
	if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil {
		t.Fatalf("unmarshal legacy metadata: %v", err)
	}
	if got := legacy.EffectiveIterations(); got != crypto.LegacyIterations {
		t.Errorf("legacy EffectiveIterations() = %d, want %d", got, crypto.LegacyIterations)
	}
}

func TestMetadataKDFIterationsJSONRoundTrip(t *testing.T) {
	salt := "dGVzdC1zYWx0"
	md := NewMetadata(salt, "dGVzdC1rZXk=")

	data, err := ToJSON(md)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"kdf_iterations": 600000`) {
		t.Errorf("expected kdf_iterations in JSON, got: %s", data)
	}

	var back Metadata
	if err := FromJSON(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.KDFIterations != crypto.DefaultIterations {
		t.Errorf("round-trip lost KDFIterations: %d", back.KDFIterations)
	}
}

func TestNewSettings(t *testing.T) {
	settings := NewSettings()

	if settings.DefaultGroup != "default" {
		t.Errorf("Expected default group 'default', got %s", settings.DefaultGroup)
	}

	if len(settings.ActiveGroups) != 0 {
		t.Errorf("Expected empty active groups, got %v", settings.ActiveGroups)
	}

	if !settings.Session.Enabled {
		t.Error("Session should be enabled by default")
	}

	if settings.Session.Timeout != "8h" {
		t.Errorf("Expected default timeout '8h', got %s", settings.Session.Timeout)
	}
}

func TestNewEnvGroup(t *testing.T) {
	name := "test-group"
	envGroup := NewEnvGroup(name)

	if envGroup.Name != name {
		t.Errorf("Expected name %s, got %s", name, envGroup.Name)
	}

	if envGroup.Variables == nil {
		t.Error("Variables map should be initialized")
	}

	if len(envGroup.Variables) != 0 {
		t.Errorf("Expected empty variables map, got %v", envGroup.Variables)
	}

	if envGroup.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewConfigIndex(t *testing.T) {
	configIndex := NewConfigIndex()

	if configIndex.Configs == nil {
		t.Error("Configs map should be initialized")
	}

	if len(configIndex.Configs) != 0 {
		t.Errorf("Expected empty configs map, got %v", configIndex.Configs)
	}
}

func TestToJSON(t *testing.T) {
	envGroup := &EnvGroup{
		Name:      "test",
		Variables: map[string]string{"KEY": "value"},
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	jsonBytes, err := ToJSON(envGroup)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("JSON output should not be empty")
	}

	expected := `"name": "test"`
	if !contains(string(jsonBytes), expected) {
		t.Errorf("JSON should contain %s", expected)
	}
}

func TestFromJSON(t *testing.T) {
	jsonStr := `{"name": "test", "variables": {"KEY": "value"}, "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z"}`

	var envGroup EnvGroup
	err := FromJSON([]byte(jsonStr), &envGroup)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if envGroup.Name != "test" {
		t.Errorf("Expected name 'test', got %s", envGroup.Name)
	}

	if envGroup.Variables["KEY"] != "value" {
		t.Errorf("Expected KEY='value', got %s", envGroup.Variables["KEY"])
	}
}

func TestToFromJSONRoundTrip(t *testing.T) {
	original := NewSettings()

	jsonBytes, err := ToJSON(original)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var restored Settings
	err = FromJSON(jsonBytes, &restored)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if restored.DefaultGroup != original.DefaultGroup {
		t.Errorf("DefaultGroup mismatch: expected %s, got %s", original.DefaultGroup, restored.DefaultGroup)
	}

	if restored.Session.Enabled != original.Session.Enabled {
		t.Errorf("Session.Enabled mismatch")
	}

	if restored.Session.Timeout != original.Session.Timeout {
		t.Errorf("Session.Timeout mismatch: expected %s, got %s", original.Session.Timeout, restored.Session.Timeout)
	}
}

func TestFromJSONInvalid(t *testing.T) {
	invalidJSON := `{invalid json`

	var envGroup EnvGroup
	err := FromJSON([]byte(invalidJSON), &envGroup)
	if err == nil {
		t.Error("FromJSON should fail with invalid JSON")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
