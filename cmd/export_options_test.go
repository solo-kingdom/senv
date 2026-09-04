package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/exportfile"
)

func TestExportPathResolution(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	absolute := filepath.Join(t.TempDir(), "absolute", "secret.txt")

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "basename", raw: "secret.txt", want: filepath.Join(cwd, "secret.txt")},
		{name: "relative", raw: filepath.Join("missing", "nested", "secret.txt"), want: filepath.Join(cwd, "missing", "nested", "secret.txt")},
		{name: "absolute", raw: absolute, want: absolute},
		{name: "home", raw: "~/keys/id.pem", want: filepath.Join(home, "keys", "id.pem")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeParent := filepath.Dir(tt.want)
			_, beforeErr := os.Lstat(beforeParent)
			got, err := exportfile.ResolvePath(tt.raw)
			if err != nil {
				t.Fatalf("ResolvePath(%q): %v", tt.raw, err)
			}
			if got.Absolute != filepath.Clean(tt.want) {
				t.Fatalf("ResolvePath(%q).Absolute = %q, want %q", tt.raw, got.Absolute, filepath.Clean(tt.want))
			}
			if got.Base != filepath.Base(tt.want) {
				t.Fatalf("ResolvePath(%q).Base = %q, want %q", tt.raw, got.Base, filepath.Base(tt.want))
			}
			_, afterErr := os.Lstat(beforeParent)
			if os.IsNotExist(beforeErr) && !os.IsNotExist(afterErr) {
				t.Fatalf("ResolvePath(%q) changed filesystem state at %q", tt.raw, beforeParent)
			}
		})
	}
}

func TestFileModeParsing(t *testing.T) {
	valid := map[string]os.FileMode{
		"0000": 0o000,
		"0600": 0o600,
		"0644": 0o644,
		"0777": 0o777,
	}
	for raw, want := range valid {
		t.Run("valid_"+raw, func(t *testing.T) {
			got, err := exportfile.ParseFileMode(raw)
			if err != nil {
				t.Fatalf("ParseFileMode(%q): %v", raw, err)
			}
			if got != want {
				t.Fatalf("ParseFileMode(%q) = %04o, want %04o", raw, got, want)
			}
		})
	}

	for _, raw := range []string{"", "600", "0648", "0o600", "-0600", "01000", "04755", "4755", "sticky"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := exportfile.ParseFileMode(raw); err == nil {
				t.Fatalf("ParseFileMode(%q) succeeded", raw)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "unchanged" {
				t.Fatalf("invalid mode parsing changed target: data=%q err=%v", data, err)
			}
		})
	}
}

func TestTextCLIExportModeFlag(t *testing.T) {
	flag := textGetCmd.Flags().Lookup("mode")
	if flag == nil {
		t.Fatal("text get --mode flag is missing")
	}
	if flag.DefValue != "0600" {
		t.Fatalf("text get --mode default = %q, want 0600", flag.DefValue)
	}
}

func TestConfigCLIFileModeFlags(t *testing.T) {
	for name, command := range map[string]*cobra.Command{
		"export":  configExportCmd,
		"install": configInstallCmd,
	} {
		flag := command.Flags().Lookup("mode")
		if flag == nil {
			t.Fatalf("config %s --mode flag is missing", name)
		}
		if flag.DefValue != "0600" {
			t.Fatalf("config %s --mode default = %q, want 0600", name, flag.DefValue)
		}
	}
}
