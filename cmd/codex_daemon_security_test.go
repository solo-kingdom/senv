package cmd

import (
	"testing"

	"github.com/wii/senv/internal/codexdaemon"
)

func TestCodexDaemonRestartFiltersVarsAndScopesToRequiredKey(t *testing.T) {
	restarted, gotEnv := stubCodexDaemon(t, codexdaemon.Status{Running: true}, nil, nil)
	origVars := codexDaemonExpVars
	codexDaemonExpVars = func() map[string]string {
		return map[string]string{
			"DEEPSEEK_API_KEY": "sk-test",
			"DB_PASSWORD":      "pg-secret",
			"LD_PRELOAD":       "/tmp/evil.so",
			"DYLD_INSERT_LIBS": "/tmp/evil.dylib",
			"NODE_OPTIONS":     "--require=/tmp/evil.js",
		}
	}
	defer func() { codexDaemonExpVars = origVars }()
	setTerminal(t, true)
	answerYes(t)

	codexDaemonRestartPrompt(true, false, "DEEPSEEK_API_KEY")
	if !*restarted {
		t.Fatal("expected daemon restart")
	}
	if len(*gotEnv) != 1 || (*gotEnv)["DEEPSEEK_API_KEY"] != "sk-test" {
		t.Fatalf("expected only DEEPSEEK_API_KEY injected, got: %v", *gotEnv)
	}

	// Without requiredEnvKey, dangerous loader/runtime vars are still stripped.
	filtered := filterCodexDaemonVars(codexDaemonExpVars(), "")
	for _, bad := range []string{"LD_PRELOAD", "DYLD_INSERT_LIBS", "NODE_OPTIONS"} {
		if _, ok := filtered[bad]; ok {
			t.Fatalf("dangerous env %q was not filtered", bad)
		}
	}
	if filtered["DEEPSEEK_API_KEY"] == "" || filtered["DB_PASSWORD"] == "" {
		t.Fatalf("expected normal env vars preserved, got: %v", filtered)
	}
}
