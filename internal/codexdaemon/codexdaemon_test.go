package codexdaemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// stubCLI replaces the subprocess seams for one test.
func stubCLI(t *testing.T, found bool, versionOut string, versionErr error, restartOut string, restartErr error) *restartCall {
	t.Helper()
	call := &restartCall{}
	originalLookPath, originalVersion, originalRestart := lookPath, runVersion, runRestart
	lookPath = func(string) (string, error) {
		if !found {
			return "", fmt.Errorf("not found")
		}
		return "/fake/codex", nil
	}
	runVersion = func(context.Context, string) ([]byte, error) {
		return []byte(versionOut), versionErr
	}
	runRestart = func(_ context.Context, _ string, env []string) ([]byte, error) {
		call.env = env
		return []byte(restartOut), restartErr
	}
	t.Cleanup(func() {
		lookPath, runVersion, runRestart = originalLookPath, originalVersion, originalRestart
	})
	return call
}

type restartCall struct {
	env []string
}

func TestProbeIsSilentWithoutCodex(t *testing.T) {
	stubCLI(t, false, "", nil, "", nil)
	status, err := Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe returned an error for a missing binary: %v", err)
	}
	if status.Running {
		t.Fatal("Probe reported a running daemon without codex installed")
	}
}

func TestProbeStaysSilentWhenNoDaemonIsRunning(t *testing.T) {
	// codex answers "no daemon" with a non-zero exit status.
	stubCLI(t, true, "", fakeExitError(t, 1), "", nil)
	status, err := Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe must treat a non-zero exit as \"no daemon\": %v", err)
	}
	if status.Running {
		t.Fatal("Probe reported a running daemon after a failed version call")
	}
}

func TestProbeReportsRunningDaemon(t *testing.T) {
	stubCLI(t, true, `{"status":"running","appServerVersion":"0.165.0"}`, nil, "", nil)
	status, err := Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !status.Running {
		t.Fatal("Probe did not report the running daemon")
	}
	if status.Version != "0.165.0" {
		t.Fatalf("version = %q, want 0.165.0", status.Version)
	}
}

func TestProbeReportsStoppedStatus(t *testing.T) {
	stubCLI(t, true, `{"status":"stopped"}`, nil, "", nil)
	status, err := Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if status.Running {
		t.Fatal("Probe reported a running daemon for status=stopped")
	}
}

func TestProbeFailsOnUnparseableOutput(t *testing.T) {
	stubCLI(t, true, "not json", nil, "", nil)
	if _, err := Probe(context.Background()); err == nil {
		t.Fatal("Probe accepted unparseable daemon status")
	}
}

func TestRestartInjectsExportVariables(t *testing.T) {
	call := stubCLI(t, true, "", nil, "", nil)
	env := map[string]string{
		"DEEPSEEK_API_KEY": "sk-test",
		"SENV_API_API_KEY": "sk-other",
	}
	if err := Restart(context.Background(), env); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	got := envMap(call.env)
	for name, want := range env {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
}

func TestRestartSurfacesDaemonError(t *testing.T) {
	stubCLI(t, true, "", nil, "some noise\nno running managed daemon\n", fakeExitError(t, 1))
	err := Restart(context.Background(), nil)
	if err == nil {
		t.Fatal("Restart swallowed the daemon error")
	}
	if !strings.Contains(err.Error(), "no running managed daemon") {
		t.Fatalf("error lost codex's reason: %v", err)
	}
}

func TestRestartRequiresCodexOnPath(t *testing.T) {
	stubCLI(t, false, "", nil, "", nil)
	if err := Restart(context.Background(), nil); err == nil {
		t.Fatal("Restart succeeded without codex on PATH")
	}
}

func TestMergeEnvOverridesAndKeepsUnrelated(t *testing.T) {
	base := []string{"PATH=/bin", "DEEPSEEK_API_KEY=old", "MALFORMED"}
	merged := envMap(mergeEnv(base, map[string]string{"DEEPSEEK_API_KEY": "new"}))

	if merged["PATH"] != "/bin" {
		t.Errorf("PATH = %q, want /bin", merged["PATH"])
	}
	if merged["DEEPSEEK_API_KEY"] != "new" {
		t.Errorf("override did not win: %q", merged["DEEPSEEK_API_KEY"])
	}
	if _, ok := merged["MALFORMED"]; ok {
		t.Error("entry without '=' was carried over")
	}
}

func TestMergeEnvHasOneValuePerName(t *testing.T) {
	merged := mergeEnv([]string{"KEY=old"}, map[string]string{"KEY": "new"})
	count := 0
	for _, entry := range merged {
		if strings.HasPrefix(entry, "KEY=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d entries for KEY, want 1: %v", count, merged)
	}
}

func TestRestartWithNoVarsKeepsProcessEnvironment(t *testing.T) {
	call := stubCLI(t, true, "", nil, "", nil)
	t.Setenv("SENV_TEST_MARKER", "kept")
	if err := Restart(context.Background(), nil); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := envMap(call.env)["SENV_TEST_MARKER"]; got != "kept" {
		t.Fatalf("process environment not inherited: %q", got)
	}
}

func TestLastLine(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"only":                 "only",
		"first\nsecond\n\n":    "second",
		"  padded  \n  tail  ": "tail",
	}
	for input, want := range cases {
		if got := lastLine(input); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProbeReportsNonExitErrors(t *testing.T) {
	// A timeout is not "no daemon"; it means the probe itself failed.
	stubCLI(t, true, "", fmt.Errorf("run: %w", context.DeadlineExceeded), "", nil)
	if _, err := Probe(context.Background()); err == nil {
		t.Fatal("Probe swallowed a deadline failure")
	}
}

// fakeExitError builds an *exec.ExitError without spawning a process, using
// the same ProcessState shape a real failed command carries.
func fakeExitError(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	return cmd.Run()
}

func envMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if found {
			out[name] = value
		}
	}
	return out
}
