package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	configmanager "github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/storage"
	textmanager "github.com/wii/senv/internal/text"
)

const securityBoundaryChildEnv = "SENV_SECURITY_BOUNDARY_CHILD"

func TestSecurityBoundaryCLI(t *testing.T) {
	if os.Getenv(securityBoundaryChildEnv) == "1" {
		runSecurityBoundaryCLIChild()
		return
	}

	const password = "correct-secret"

	t.Run("text and config mode", func(t *testing.T) {
		cfg, data := newInitializedProject(t, t.TempDir(), password)
		store := storage.NewManager(cfg, data)
		if err := textmanager.NewManager(store, password).Set("default", "SECRET", "text-secret"); err != nil {
			t.Fatalf("seed text: %v", err)
		}
		source := filepath.Join(t.TempDir(), "app.yaml")
		if err := os.WriteFile(source, []byte("token: config-secret\n"), 0o600); err != nil {
			t.Fatalf("write config source: %v", err)
		}
		if err := configmanager.NewManager(store, password).Create("app", source, filepath.Join(t.TempDir(), "installed.yaml"), "default", ""); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		textOutput := filepath.Join(t.TempDir(), "secret.txt")
		output, code := runSecurityBoundaryCLI(t, cfg, data, password+"\n", nil,
			"--path", data, "text", "get", "SECRET", "--output", textOutput, "--mode", "0644")
		requireCLIExit(t, output, code, 0)
		assertExport(t, textOutput, "text-secret", 0o644)

		configOutput := filepath.Join(t.TempDir(), "app-export.yaml")
		output, code = runSecurityBoundaryCLI(t, cfg, data, password+"\n", nil,
			"--path", data, "config", "export", "app", "--path", configOutput, "--mode", "0644")
		requireCLIExit(t, output, code, 0)
		assertExport(t, configOutput, "token: config-secret\n", 0o644)

		badTextOutput := filepath.Join(t.TempDir(), "bad-text")
		badConfigOutput := filepath.Join(t.TempDir(), "bad-config")
		for _, tc := range []struct {
			name string
			args []string
			path string
		}{
			{name: "text invalid mode", args: []string{"--path", data, "text", "get", "SECRET", "--output", badTextOutput, "--mode", "4755"}, path: badTextOutput},
			{name: "config invalid mode", args: []string{"--path", data, "config", "export", "app", "--path", badConfigOutput, "--mode", "0o644"}, path: badConfigOutput},
		} {
			t.Run(tc.name, func(t *testing.T) {
				output, code := runSecurityBoundaryCLI(t, cfg, data, "", nil, tc.args...)
				requireCLIError(t, output, code, "invalid file mode")
				if _, err := os.Lstat(tc.path); !os.IsNotExist(err) {
					t.Fatalf("invalid --mode changed output path %q: %v", tc.path, err)
				}
			})
		}
	})

	t.Run("unsafe runtime", func(t *testing.T) {
		cfg, data := newInitializedProject(t, t.TempDir(), password)
		diskRuntime := t.TempDir()
		if runtime.GOOS == "linux" {
			diskRuntime = "/proc"
		}
		output, code := runSecurityBoundaryCLI(t, cfg, data, password+"\n", map[string]string{"XDG_RUNTIME_DIR": diskRuntime},
			"--path", data, "session", "start", "--timeout", "never")
		requireCLIError(t, output, code, "no secure session store available")
		requireCLIError(t, output, code, "--insecure-cache")
		if strings.Contains(output, "Session started") {
			t.Fatalf("unsafe runtime reported success: %s", output)
		}
	})

	t.Run("invalid KDF", func(t *testing.T) {
		cfg, data := newInitializedProject(t, t.TempDir(), password)
		corruptKDFMetadataForCommandTest(t, cfg, data, 1_000_001)
		output, code := runSecurityBoundaryCLI(t, cfg, data, "", nil, "--path", data, "env", "list")
		requireCLIError(t, output, code, "invalid or unsupported metadata KDF parameters")
		if strings.Contains(strings.ToLower(output), "invalid password") {
			t.Fatalf("invalid KDF was misreported as password failure: %s", output)
		}
	})

	t.Run("rekey recovery fail closed", func(t *testing.T) {
		cfg, data := newInitializedProject(t, t.TempDir(), password)
		manifest := filepath.Join(cfg, ".senv-rekey-manifest.json")
		if err := os.WriteFile(manifest, []byte("{"), 0o600); err != nil {
			t.Fatalf("write corrupt rekey manifest: %v", err)
		}
		output, code := runSecurityBoundaryCLI(t, cfg, data, "", nil, "--path", data, "env", "list")
		requireCLIError(t, output, code, "unfinished rekey requires recovery")
		if !strings.Contains(output, "run senv doctor") {
			t.Fatalf("rekey recovery error lacks doctor guidance: %s", output)
		}
		contents, err := os.ReadFile(manifest)
		if err != nil || string(contents) != "{" {
			t.Fatalf("failed recovery did not preserve manifest: contents=%q err=%v", contents, err)
		}
	})
}

func runSecurityBoundaryCLIChild() {
	cfg := os.Getenv("SENV_SECURITY_BOUNDARY_CONFIG")
	configPathFn = func() string { return cfg }
	dataPath = os.Getenv("SENV_SECURITY_BOUNDARY_DATA")
	stdinIsTerminal = func() bool { return true }
	stdoutIsTerminal = func() bool { return true }
	clearAuthMemo()
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator == len(os.Args)-1 {
		fmt.Fprintln(os.Stderr, "security boundary child received no CLI arguments")
		os.Exit(2)
	}
	rootCmd.SetArgs(os.Args[separator+1:])
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runSecurityBoundaryCLI(t *testing.T, cfg, data, stdin string, overrides map[string]string, args ...string) (string, int) {
	t.Helper()
	childArgs := append([]string{"-test.run=^TestSecurityBoundaryCLI$", "--"}, args...)
	command := exec.Command(os.Args[0], childArgs...)
	values := map[string]string{
		securityBoundaryChildEnv:        "1",
		"SENV_SECURITY_BOUNDARY_CONFIG": cfg,
		"SENV_SECURITY_BOUNDARY_DATA":   data,
	}
	for key, value := range overrides {
		values[key] = value
	}
	command.Env = replaceEnvironment(os.Environ(), values)
	command.Stdin = strings.NewReader(stdin)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if err == nil {
		return output.String(), 0
	}
	var exitErr *exec.ExitError
	if !strings.Contains(err.Error(), "exit status") || !asExitError(err, &exitErr) {
		t.Fatalf("run CLI subprocess: %v; output=%s", err, output.String())
	}
	return output.String(), exitErr.ExitCode()
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func replaceEnvironment(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func requireCLIExit(t *testing.T, output string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("CLI exit code=%d, want %d; output=%s", got, want, output)
	}
}

func requireCLIError(t *testing.T, output string, code int, want string) {
	t.Helper()
	if code == 0 {
		t.Fatalf("CLI unexpectedly succeeded; output=%s", output)
	}
	if !strings.Contains(output, want) {
		t.Fatalf("CLI error %q missing from output: %s", want, output)
	}
}

func assertExport(t *testing.T, path, want string, mode os.FileMode) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export %q: %v", path, err)
	}
	if string(contents) != want {
		t.Fatalf("export %q contents=%q, want %q", path, contents, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat export %q: %v", path, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != mode {
		t.Fatalf("export %q mode=%04o, want %04o", path, info.Mode().Perm(), mode)
	}
}
