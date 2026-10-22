package syncschema

import (
	"errors"
	"testing"
)

func TestValidateIdentitySkillKinds(t *testing.T) {
	for _, tt := range []struct{ kind, grp, key string }{
		{KindSkill, "default", "my-skill"},
		{KindSkillIndex, "", ""},
	} {
		if err := ValidateIdentity(tt.kind, tt.grp, tt.key); err != nil {
			t.Errorf("ValidateIdentity(%q, %q, %q) = %v", tt.kind, tt.grp, tt.key, err)
		}
	}
}

func TestValidateIdentityRejectsSkillMatrixAndPaths(t *testing.T) {
	for _, tt := range []struct {
		name, kind, grp, key string
	}{
		{"skill missing grp", KindSkill, "", "id"},
		{"skill missing key", KindSkill, "default", ""},
		{"skill traversal grp", KindSkill, "..", "id"},
		{"skill traversal key", KindSkill, "default", "../id"},
		{"skill slash key", KindSkill, "default", "a/b"},
		{"skill colon key", KindSkill, "default", "a:b"},
		{"skill index extra grp", KindSkillIndex, "default", ""},
		{"skill index extra key", KindSkillIndex, "", "id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateIdentity(tt.kind, tt.grp, tt.key); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("error = %v, want ErrInvalidIdentity", err)
			}
		})
	}
}
