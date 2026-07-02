package tui

import "testing"

func TestParseKeyAddress(t *testing.T) {
	tests := []struct {
		input, fallback string
		wantGroup, wantKey string
	}{
		{"API_KEY", "default", "default", "API_KEY"},
		{"prod:API_KEY", "default", "prod", "API_KEY"},
		{":API_KEY", "staging", "default", "API_KEY"},
		{"prod:", "default", "prod", ""},
	}
	for _, tt := range tests {
		g, k := parseKeyAddress(tt.input, tt.fallback)
		if g != tt.wantGroup || k != tt.wantKey {
			t.Errorf("parseKeyAddress(%q, %q) = (%q, %q), want (%q, %q)",
				tt.input, tt.fallback, g, k, tt.wantGroup, tt.wantKey)
		}
	}
}
