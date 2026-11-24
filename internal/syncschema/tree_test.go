package syncschema

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateIdentityTreeKinds(t *testing.T) {
	for _, tt := range []struct{ kind, grp, key string }{
		{KindTree, "deploy", "tcbj/caddy/Caddyfile"},
		{KindTree, "wireguard", "script/render.sh"},
		{KindTree, "deploy", "BACKUP.md"},
		{KindTreeMeta, "deploy", ""},
	} {
		if err := ValidateIdentity(tt.kind, tt.grp, tt.key); err != nil {
			t.Errorf("ValidateIdentity(%q, %q, %q) = %v", tt.kind, tt.grp, tt.key, err)
		}
	}
}

func TestValidateIdentityRejectsTreeAttacks(t *testing.T) {
	badPaths := []string{
		"",
		"/abs/path",
		"trailing/",
		"a//b",
		".",
		"..",
		"a/../b",
		`a\b`,
		"a:b",
		"a\x00b",
		strings.Repeat("a/", 17) + "f",
		strings.Repeat("x", MaxTreePathBytes+1),
	}
	for _, p := range badPaths {
		if err := ValidateIdentity(KindTree, "deploy", p); !errors.Is(err, ErrInvalidIdentity) {
			t.Errorf("ValidateIdentity(tree, deploy, %q) = %v, want ErrInvalidIdentity", p, err)
		}
	}
	for _, tt := range []struct{ kind, grp, key string }{
		{KindTree, "", "a/b"},
		{KindTree, "../x", "a/b"},
		{KindTreeMeta, "", ""},
		{KindTreeMeta, "deploy", "a/b"},
	} {
		if err := ValidateIdentity(tt.kind, tt.grp, tt.key); !errors.Is(err, ErrInvalidIdentity) {
			t.Errorf("ValidateIdentity(%q, %q, %q) = %v, want ErrInvalidIdentity", tt.kind, tt.grp, tt.key, err)
		}
	}
}
