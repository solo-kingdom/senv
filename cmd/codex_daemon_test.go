package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/wii/senv/internal/codexdaemon"
)

// stubCodexDaemon replaces the probe/restart seams for one test and reports
// whether a restart was attempted.
func stubCodexDaemon(t *testing.T, status codexdaemon.Status, probeErr error, restartErr error) (*bool, *map[string]string) {
	t.Helper()
	restarted := false
	var gotEnv map[string]string
	originalProbe, originalRestart, originalVars := codexDaemonProbe, codexDaemonRestart, codexDaemonExpVars
	codexDaemonProbe = func(context.Context) (codexdaemon.Status, error) {
		return status, probeErr
	}
	codexDaemonRestart = func(_ context.Context, env map[string]string) error {
		restarted = true
		gotEnv = env
		return restartErr
	}
	codexDaemonExpVars = func() map[string]string {
		return map[string]string{"DEEPSEEK_API_KEY": "sk-test"}
	}
	t.Cleanup(func() {
		codexDaemonProbe, codexDaemonRestart, codexDaemonExpVars = originalProbe, originalRestart, originalVars
	})
	return &restarted, &gotEnv
}

// setTerminal pins stdinIsTerminal so the prompt path is deterministic.
func setTerminal(t *testing.T, isTTY bool) {
	t.Helper()
	original := stdinIsTerminal
	stdinIsTerminal = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTerminal = original })
}

// answerYes / answerNo make the confirmation deterministic without touching
// the real stdin.
func answerYes(t *testing.T) { setConfirm(t, true) }

func answerNo(t *testing.T) { setConfirm(t, false) }

func setConfirm(t *testing.T, answer bool) {
	t.Helper()
	original := codexDaemonConfirm
	codexDaemonConfirm = func(string) bool { return answer }
	t.Cleanup(func() { codexDaemonConfirm = original })
}

func TestCodexDaemonPromptIsSilentWithoutCodexHit(t *testing.T) {
	restarted, _ := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, nil)
	setTerminal(t, true)
	answerYes(t)

	codexDaemonRestartPrompt(false, false)

	if *restarted {
		t.Fatal("restarted the daemon although codex was not written")
	}
}

func TestCodexDaemonPromptIsSilentWithoutRunningDaemon(t *testing.T) {
	restarted, _ := stubCodexDaemon(t, codexdaemon.Status{}, nil, nil)
	setTerminal(t, true)
	answerYes(t)

	codexDaemonRestartPrompt(true, false)

	if *restarted {
		t.Fatal("restarted a daemon that is not running")
	}
}

func TestCodexDaemonPromptIsSilentOnProbeFailure(t *testing.T) {
	restarted, _ := stubCodexDaemon(t, codexdaemon.Status{}, errors.New("probe failed"), nil)
	setTerminal(t, true)
	answerYes(t)

	codexDaemonRestartPrompt(true, false)

	if *restarted {
		t.Fatal("restarted after a failed probe")
	}
}

func TestCodexDaemonPromptRestartsWhenConfirmed(t *testing.T) {
	restarted, gotEnv := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, nil)
	setTerminal(t, true)
	answerYes(t)

	codexDaemonRestartPrompt(true, false)

	if !*restarted {
		t.Fatal("confirmation did not restart the daemon")
	}
	if (*gotEnv)["DEEPSEEK_API_KEY"] != "sk-test" {
		t.Fatalf("restart did not receive the export variables: %v", *gotEnv)
	}
}

func TestCodexDaemonPromptSkipsWhenDeclined(t *testing.T) {
	restarted, _ := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, nil)
	setTerminal(t, true)
	answerNo(t)

	codexDaemonRestartPrompt(true, false)

	if *restarted {
		t.Fatal("declining the prompt still restarted the daemon")
	}
}

// Non-interactive runs must never restart a shared process on their own.
func TestCodexDaemonPromptNeverRestartsNonInteractively(t *testing.T) {
	for _, tc := range []struct {
		name     string
		nonInter bool
		terminal bool
	}{
		{"--yes", true, true},
		{"piped stdin", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restarted, _ := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, nil)
			setTerminal(t, tc.terminal)
			answerYes(t)

			codexDaemonRestartPrompt(true, tc.nonInter)

			if *restarted {
				t.Fatal("non-interactive run restarted the daemon")
			}
		})
	}
}

func TestCodexDaemonPromptSurvivesRestartFailure(t *testing.T) {
	restarted, _ := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, errors.New("daemon died"))
	setTerminal(t, true)
	answerYes(t)

	// A failed restart is reported, not fatal: the configuration write already
	// succeeded, so the command must still exit successfully.
	codexDaemonRestartPrompt(true, false)

	if !*restarted {
		t.Fatal("restart was not attempted")
	}
}
