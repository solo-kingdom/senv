package cmd

import "testing"

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input     string
		wantGroup string
		wantKey   string
		wantOk    bool
	}{
		{"group:key", "group", "key", true},
		{":key", "default", "key", true},
		{"group:", "group", "__default", true},
		{":", "default", "__default", true},
		{"noconn", "", "", false},
		{"", "", "", false},
		{"a:b:c", "a", "b:c", true}, // only first colon splits
	}

	for _, tt := range tests {
		group, key, ok := parseAddress(tt.input)
		if ok != tt.wantOk {
			t.Errorf("parseAddress(%q): ok=%v, want %v", tt.input, ok, tt.wantOk)
			continue
		}
		if ok {
			if group != tt.wantGroup {
				t.Errorf("parseAddress(%q): group=%q, want %q", tt.input, group, tt.wantGroup)
			}
			if key != tt.wantKey {
				t.Errorf("parseAddress(%q): key=%q, want %q", tt.input, key, tt.wantKey)
			}
		}
	}
}

func TestResolveAddressKey(t *testing.T) {
	tests := []struct {
		arg       string
		flagGroup string
		wantGroup string
		wantKey   string
	}{
		{":key", "other", "default", "key"},
		{"group:key", "other", "group", "key"},
		{"plainkey", "default", "default", "plainkey"},
		{"plainkey", "feg", "feg", "plainkey"},
		{"feg:ACCOUNT", "other", "feg", "ACCOUNT"},
	}

	for _, tt := range tests {
		group, key := resolveAddressKey(tt.arg, tt.flagGroup)
		if group != tt.wantGroup {
			t.Errorf("resolveAddressKey(%q, %q): group=%q, want %q", tt.arg, tt.flagGroup, group, tt.wantGroup)
		}
		if key != tt.wantKey {
			t.Errorf("resolveAddressKey(%q, %q): key=%q, want %q", tt.arg, tt.flagGroup, key, tt.wantKey)
		}
	}
}
