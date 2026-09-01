package config

import (
	"os"
	"path/filepath"
	"testing"
)

// createConfigWithTarget creates a config whose target path is inside dir.
func createConfigWithTarget(t *testing.T, m *Manager, name, group, target, content string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), name+".src")
	writeFile(t, src, content)
	if err := m.Create(name, src, target, group, ""); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
}

func TestPlanInstallActions(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	// missing target -> create
	createConfigWithTarget(t, m, "new", "", filepath.Join(dir, "new.conf"), "a\n")
	// identical target -> skip
	sameTarget := filepath.Join(dir, "same.conf")
	writeFile(t, sameTarget, "b\n")
	createConfigWithTarget(t, m, "same", "", sameTarget, "b\n")
	// differing target -> backup_overwrite
	diffTarget := filepath.Join(dir, "diff.conf")
	writeFile(t, diffTarget, "old\n")
	createConfigWithTarget(t, m, "diff", "", diffTarget, "new\n")
	// undefined env var -> error
	createConfigWithTarget(t, m, "bad", "", "$SENV_UNDEFINED_VAR/x.conf", "x\n")

	plan, err := m.PlanInstall(Scope{All: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 4 {
		t.Fatalf("plan items = %d, want 4", len(plan.Items))
	}

	actions := map[string]string{}
	for _, item := range plan.Items {
		actions[item.Name] = item.Action
	}
	want := map[string]string{
		"new":  ActionCreate,
		"same": ActionSkip,
		"diff": ActionBackupOverwrite,
		"bad":  ActionError,
	}
	for name, wantAction := range want {
		if actions[name] != wantAction {
			t.Errorf("action[%s] = %q, want %q", name, actions[name], wantAction)
		}
	}
}

func TestPlanInstallScopeValidation(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	createConfigWithTarget(t, m, "a", "g1", filepath.Join(dir, "a.conf"), "a\n")
	createConfigWithTarget(t, m, "b", "g2", filepath.Join(dir, "b.conf"), "b\n")

	if _, err := m.PlanInstall(Scope{Name: "missing"}); err == nil {
		t.Error("expected error for missing name")
	}
	if _, err := m.PlanInstall(Scope{Group: "missing"}); err == nil {
		t.Error("expected error for missing group")
	}
	if _, err := m.PlanInstall(Scope{}); err == nil {
		t.Error("expected error for empty scope")
	}
	if _, err := m.PlanInstall(Scope{Name: "a", All: true}); err == nil {
		t.Error("expected error for conflicting scope")
	}

	plan, err := m.PlanInstall(Scope{Group: "g1"})
	if err != nil {
		t.Fatalf("plan group: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Name != "a" {
		t.Errorf("group plan = %+v, want only a", plan.Items)
	}
}

func TestExecuteInstall(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	// recursive directory creation
	deepTarget := filepath.Join(dir, "x", "y", "z", "deep.conf")
	createConfigWithTarget(t, m, "deep", "", deepTarget, "deep\n")
	// differing target -> backup
	diffTarget := filepath.Join(dir, "diff.conf")
	writeFile(t, diffTarget, "old\n")
	createConfigWithTarget(t, m, "diff", "", diffTarget, "new\n")
	// up-to-date target -> skip, no backup
	sameTarget := filepath.Join(dir, "same.conf")
	writeFile(t, sameTarget, "same\n")
	createConfigWithTarget(t, m, "same", "", sameTarget, "same\n")
	// error item is skipped without aborting the rest
	createConfigWithTarget(t, m, "bad", "", "$SENV_UNDEFINED_VAR/x.conf", "x\n")

	plan, err := m.PlanInstall(Scope{All: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := m.ExecuteInstall(plan); err == nil {
		t.Fatal("expected aggregated error from the error item")
	}

	// deep target created
	data, err := os.ReadFile(deepTarget)
	if err != nil || string(data) != "deep\n" {
		t.Errorf("deep target = %q, err=%v", string(data), err)
	}

	// diff target overwritten and backed up
	data, _ = os.ReadFile(diffTarget)
	if string(data) != "new\n" {
		t.Errorf("diff target = %q, want new", string(data))
	}
	backups, _ := filepath.Glob(diffTarget + ".senv-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want 1", backups)
	}
	backupData, _ := os.ReadFile(backups[0])
	if string(backupData) != "old\n" {
		t.Errorf("backup content = %q, want old", string(backupData))
	}

	// skip produced no backup
	sameBackups, _ := filepath.Glob(sameTarget + ".senv-backup-*")
	if len(sameBackups) != 0 {
		t.Errorf("skip item produced backups: %v", sameBackups)
	}
}

func TestExecuteInstallRechecksBeforeWrite(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "app.conf")
	// Plan says "create"...
	createConfigWithTarget(t, m, "app", "", target, "stored\n")
	plan, err := m.PlanInstall(Scope{Name: "app"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Items[0].Action != ActionCreate {
		t.Fatalf("action = %q, want create", plan.Items[0].Action)
	}
	// ...but the target appears (with different content) before execute.
	writeFile(t, target, "local edits\n")

	if err := m.ExecuteInstall(plan); err != nil {
		t.Fatalf("execute: %v", err)
	}
	backups, _ := filepath.Glob(target + ".senv-backup-*")
	if len(backups) != 1 {
		t.Fatalf("expected backup of the concurrently created file, got %v", backups)
	}
	backupData, _ := os.ReadFile(backups[0])
	if string(backupData) != "local edits\n" {
		t.Errorf("backup = %q, want local edits", string(backupData))
	}
}

func TestExportBackupsDifferingTarget(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "app.conf")
	writeFile(t, target, "local\n")
	createConfigWithTarget(t, m, "app", "", target, "stored\n")

	if err := m.Export("app", ""); err != nil {
		t.Fatalf("export: %v", err)
	}
	backups, _ := filepath.Glob(target + ".senv-backup-*")
	if len(backups) != 1 {
		t.Errorf("expected backup on export overwrite, got %v", backups)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "stored\n" {
		t.Errorf("target = %q, want stored", string(data))
	}
}
