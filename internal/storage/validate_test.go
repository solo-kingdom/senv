package storage

import (
	"testing"
)

func TestValidateEnvKey(t *testing.T) {
	valid := []string{
		"API_KEY",
		"_PRIVATE",
		"a",
		"A1_B2",
		"DATABASE_URL",
		"__default",
	}
	invalid := []string{
		"",
		"123KEY",
		"my-key",
		"foo.bar",
		"a/b",
		"with space",
		"colon:name",
		"openviking/root_api_key",
		"dash-leading-",
		"$IGNSYM",
	}

	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			if err := ValidateEnvKey(name); err != nil {
				t.Errorf("ValidateEnvKey(%q) = %v, want nil", name, err)
			}
		})
	}

	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			if err := ValidateEnvKey(name); err == nil {
				t.Errorf("ValidateEnvKey(%q) = nil, want error", name)
			}
		})
	}
}
