package securefs

import (
	"errors"
	"testing"
)

func TestValidateSegmentRejectsUnsafeNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		segment string
	}{
		{name: "empty", segment: ""},
		{name: "dot", segment: "."},
		{name: "dot dot", segment: ".."},
		{name: "nul", segment: "secret\x00suffix"},
		{name: "colon", segment: "group:key"},
		{name: "unix separator", segment: "group/key"},
		{name: "windows separator", segment: `group\key`},
		{name: "unix traversal", segment: "../outside"},
		{name: "embedded traversal", segment: "a/../../outside"},
		{name: "windows traversal", segment: `..\outside`},
		{name: "unix absolute", segment: "/var/lib/senv"},
		{name: "double slash absolute", segment: "//server/share"},
		{name: "windows drive absolute", segment: `C:\vault`},
		{name: "windows drive relative", segment: `C:vault`},
		{name: "windows volume", segment: `C:`},
		{name: "windows UNC", segment: `\\server\share`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSegment(tt.segment)
			if !errors.Is(err, ErrInvalidSegment) {
				t.Fatalf("ValidateSegment(%q) error = %v, want ErrInvalidSegment", tt.segment, err)
			}
			var pathErr *PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("ValidateSegment(%q) error type = %T, want *PathError", tt.segment, err)
			}
		})
	}
}

func TestValidateSegmentAcceptsPortableNames(t *testing.T) {
	t.Parallel()

	for _, segment := range []string{
		"default",
		"prod",
		"my-group",
		"team_1",
		"API_KEY",
		"database.json.enc",
		".private",
		"a..b",
		"配置",
		"name with spaces",
	} {
		t.Run(segment, func(t *testing.T) {
			if err := ValidateSegment(segment); err != nil {
				t.Fatalf("ValidateSegment(%q) unexpected error: %v", segment, err)
			}
		})
	}
}
