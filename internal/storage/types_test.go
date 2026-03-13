package storage

import (
	"testing"
	"time"
)

func TestNewMetadata(t *testing.T) {
	salt := "dGVzdC1zYWx0" // base64 encoded "test-salt"
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
