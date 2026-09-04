package syncschema

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateIdentityAcceptsFiveKinds(t *testing.T) {
	tests := []struct {
		kind string
		grp  string
		key  string
	}{
		{KindEnv, "default", "API_KEY"},
		{KindEnvMeta, "prod", ""},
		{KindText, "secrets", "ssh-key"},
		{KindConfig, "", "database-prod"},
		{KindConfigIndex, "", ""},
	}
	for _, tt := range tests {
		if err := ValidateIdentity(tt.kind, tt.grp, tt.key); err != nil {
			t.Errorf("ValidateIdentity(%q, %q, %q) = %v", tt.kind, tt.grp, tt.key, err)
		}
	}
}

func TestValidateIdentityRejectsUnknownAndFieldMatrix(t *testing.T) {
	tests := []struct {
		name string
		kind string
		grp  string
		key  string
	}{
		{"unknown kind", "future_kind", "", ""},
		{"env missing grp", KindEnv, "", "KEY"},
		{"env missing key", KindEnv, "default", ""},
		{"env meta missing grp", KindEnvMeta, "", ""},
		{"env meta extra key", KindEnvMeta, "default", "KEY"},
		{"text missing grp", KindText, "", "KEY"},
		{"text missing key", KindText, "group", ""},
		{"config extra grp", KindConfig, "group", "name"},
		{"config missing key", KindConfig, "", ""},
		{"config index extra grp", KindConfigIndex, "group", ""},
		{"config index extra key", KindConfigIndex, "", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdentity(tt.kind, tt.grp, tt.key)
			if !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("error = %v, want ErrInvalidIdentity", err)
			}
		})
	}
}

func TestValidateIdentityRejectsPathAttacks(t *testing.T) {
	attacks := []string{
		"", ".", "..", "../x", "a/../../x", "/absolute", `a\b`, `C:\vault`,
		"nul\x00segment", "colon:name",
	}
	for _, attack := range attacks {
		t.Run(strings.ReplaceAll(attack, "\x00", "NUL"), func(t *testing.T) {
			for _, identity := range []struct {
				kind string
				grp  string
				key  string
			}{
				{KindEnv, attack, "KEY"},
				{KindEnv, "default", attack},
				{KindEnvMeta, attack, ""},
				{KindText, attack, "key"},
				{KindText, "group", attack},
				{KindConfig, "", attack},
			} {
				err := ValidateIdentity(identity.kind, identity.grp, identity.key)
				if !errors.Is(err, ErrInvalidIdentity) {
					t.Errorf("ValidateIdentity(%q, attack) error = %v, want ErrInvalidIdentity", identity.kind, err)
				}
				if attack != "" && strings.Contains(err.Error(), attack) {
					t.Errorf("validation error reflected unsafe identity %q: %v", attack, err)
				}
			}
		})
	}
}

func TestValidateIdentityRejectsInvalidEnvShellKeys(t *testing.T) {
	for _, key := range []string{"1KEY", "bad-key", "KEY.DOT", "KEY SPACE"} {
		t.Run(key, func(t *testing.T) {
			if err := ValidateIdentity(KindEnv, "default", key); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("ValidateIdentity(env, %q) error = %v, want ErrInvalidIdentity", key, err)
			}
		})
	}
}
