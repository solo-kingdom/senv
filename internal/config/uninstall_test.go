package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanUninstallActions(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	// absent target -> noop
	createConfigWithTarget(t, m, "absent", "", filepath.Join(dir, "absent.conf"), "a\n")
	// identical target -> delete
	sameTarget := filepath.Join(dir, "same.conf")
	writeFile(t, sameTarget, "b\n")
	createConfigWithTarget(t, m, "same", "", sameTarget, "b\n")
	// modified target -> changed
	modTarget := filepath.Join(dir, "mod.conf")
	writeFile(t, modTarget, "local edits\n")
	createConfigWithTarget(t, m, "mod", "", modTarget, "stored\n")

	plan, err := m.PlanUninstall(Scope{All: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	actions := map[string]string{}
	for _, item := range plan.Items {
		actions[item.Name] = item.Action
	}
	want := map[string]string{
		"absent": ActionNoop,
		"same":   ActionDelete,
		"mod":    ActionChanged,
	}
	for name, wantAction := range want {
		if actions[name] != wantAction {
			t.Errorf("action[%s] = %q, want %q", name, actions[name], wantAction)
		}
	}
	if !plan.HasChanged() {
		t.Error("HasChanged = false, want true")
	}
}

func TestExecuteUninstall(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()

	sameTarget := filepath.Join(dir, "same.conf")
	writeFile(t, sameTarget, "b\n")
	createConfigWithTarget(t, m, "same", "", sameTarget, "b\n")

	modTarget := filepath.Join(dir, "mod.conf")
	writeFile(t, modTarget, "local edits\n")
	createConfigWithTarget(t, m, "mod", "", modTarget, "stored\n")

	plan, err := m.PlanUninstall(Scope{All: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Reject the changed item: it must be kept.
	if err := m.ExecuteUninstall(plan, func(UninstallItem) bool { return false }); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := os.Stat(sameTarget); !os.IsNotExist(err) {
		t.Error("identical target should have been deleted")
	}
	if _, err := os.Stat(modTarget); err != nil {
		t.Error("changed target rejected for deletion must be kept")
	}

	// Storage entry must survive uninstall.
	if _, err := m.Get("same"); err != nil {
		t.Error("storage entry must not be removed by uninstall")
	}

	// Confirm the changed item: it gets deleted.
	if err := m.ExecuteUninstall(plan, func(UninstallItem) bool { return true }); err != nil {
		t.Fatalf("execute confirmed: %v", err)
	}
	if _, err := os.Stat(modTarget); !os.IsNotExist(err) {
		t.Error("changed target confirmed for deletion should be removed")
	}
}

func TestUninstallThenReinstall(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "app.conf")
	writeFile(t, target, "v1\n")
	createConfigWithTarget(t, m, "app", "", target, "v1\n")

	plan, err := m.PlanUninstall(Scope{Name: "app"})
	if err != nil {
		t.Fatalf("plan uninstall: %v", err)
	}
	if err := m.ExecuteUninstall(plan, nil); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	iplan, err := m.PlanInstall(Scope{Name: "app"})
	if err != nil {
		t.Fatalf("plan install: %v", err)
	}
	if iplan.Items[0].Action != ActionCreate {
		t.Errorf("reinstall action = %q, want create", iplan.Items[0].Action)
	}
	if err := m.ExecuteInstall(iplan); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "v1\n" {
		t.Errorf("reinstalled content = %q, want v1", string(data))
	}
}

func TestUninstallRejectsSymlinkTarget(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.conf")
	writeFile(t, outside, "stored\n")
	target := filepath.Join(dir, "linked.conf")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	createConfigWithTarget(t, m, "linked", "", target, "stored\n")

	plan, err := m.PlanUninstall(Scope{Name: "linked"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionError {
		t.Fatalf("symlink plan = %+v, want one error item", plan.Items)
	}
	if err := m.ExecuteUninstall(plan, func(UninstallItem) bool { return true }); err == nil {
		t.Fatal("uninstall accepted a symlink target")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "stored\n" {
		t.Fatalf("outside target changed: %q, %v", data, err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat target symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("target mode=%v, want symlink", info.Mode())
	}
}
