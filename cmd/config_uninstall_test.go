package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/storage"
)

func TestConfigUninstallYesPreservesModifiedTargetUnlessForceChanged(t *testing.T) {
	isolateSessionCache(t)
	cfgDir, dataDir := newInitializedProject(t, t.TempDir(), "pw")
	useProjectPaths(t, cfgDir, dataDir)

	origYes, origForce, origAll, origDry, origGroup := configYes, configForceChanged, configInstallAll, configDryRun, configListGroup
	origStdinTerm, origPrompt := stdinIsTerminal, authPrompt
	t.Cleanup(func() {
		configYes, configForceChanged, configInstallAll, configDryRun, configListGroup = origYes, origForce, origAll, origDry, origGroup
		stdinIsTerminal, authPrompt = origStdinTerm, origPrompt
	})

	authPrompt = func(string) (string, error) { return "pw", nil }
	stdinIsTerminal = func() bool { return true }

	work := t.TempDir()
	src := filepath.Join(work, "src.conf")
	target := filepath.Join(work, "target.conf")
	if err := os.WriteFile(src, []byte("managed-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	cm := config.NewManager(storage.NewManager(cfgDir, dataDir), "pw")
	if err := cm.Create("app", src, target, "", ""); err != nil {
		t.Fatal(err)
	}
	plan, err := cm.PlanInstall(config.Scope{Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.ExecuteInstall(plan); err != nil {
		t.Fatal(err)
	}

	// Simulate locally modified (or tampered-TargetPath) existing file.
	if err := os.WriteFile(target, []byte("user-modified-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. --yes alone must NOT delete ActionChanged target.
	configYes = true
	configForceChanged = false
	configInstallAll = false
	configDryRun = false
	configListGroup = ""
	if err := configUninstallCmd.RunE(configUninstallCmd, []string{"app"}); err != nil {
		t.Fatalf("uninstall --yes: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("--yes deleted modified target without --force-changed: %v", err)
	}

	// 2. --yes --force-changed deletes the modified target.
	configForceChanged = true
	if err := configUninstallCmd.RunE(configUninstallCmd, []string{"app"}); err != nil {
		t.Fatalf("uninstall --yes --force-changed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("--force-changed should delete target, stat err = %v", err)
	}
}
