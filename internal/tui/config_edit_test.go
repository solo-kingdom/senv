package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/config"
)

func newLoadedConfigTab(t *testing.T, mgr *config.Manager) *configTab {
	t.Helper()
	tab := newConfigTab(Managers{Config: mgr})
	tab.SetSize(80, 20)
	return flushConfig(tab, tab.load())
}

func TestConfigRenameViaForm(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "server: 8080\n")
	if err := mgr.Create("app", src, "/etc/app.conf", "ops", "demo"); err != nil {
		t.Fatalf("create: %v", err)
	}
	tab := newLoadedConfigTab(t, mgr)
	tab.itemIndex = 0

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*configTab)
	if tab.form == nil {
		t.Fatal("r should open the rename form")
	}
	if !tab.InputMode() {
		t.Error("open form must report InputMode")
	}
	tab.form = replaceFormText(t, tab.form, "renamed")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*configTab)
	tab = flushConfig(tab, cmd)
	if tab.form != nil {
		t.Fatal("submit should close the form")
	}
	if !hasConfigItem(tab, "renamed") {
		t.Fatalf("renamed config missing: %#v", allConfigItems(tab))
	}
	if hasConfigItem(tab, "app") {
		t.Error("old name still listed")
	}
	// Rename must not touch target/group/description.
	info, err := mgr.Get("renamed")
	if err != nil {
		t.Fatalf("get renamed: %v", err)
	}
	if info.TargetPath != "/etc/app.conf" || info.Group != "ops" || info.Description != "demo" {
		t.Errorf("metadata changed by rename: %+v", info)
	}
}

func TestConfigRenameConflictKeepsFormOpen(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "x=1\n")
	if err := mgr.Create("app", src, "/etc/app.conf", "", ""); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := mgr.Create("cli", src, "/etc/cli.conf", "", ""); err != nil {
		t.Fatalf("create cli: %v", err)
	}
	tab := newLoadedConfigTab(t, mgr)
	tab.itemIndex = 0

	out, _ := tab.Update(runeKey("r"))
	tab = out.(*configTab)
	tab.form = replaceFormText(t, tab.form, "cli")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*configTab)
	if tab.form == nil {
		t.Fatal("conflicting rename must keep the form open")
	}
	if !strings.Contains(tab.View(), "already exists") {
		t.Errorf("inline conflict error missing: %q", tab.View())
	}
	if cmd != nil {
		if msgs := runCmd(cmd); len(msgs) > 0 {
			t.Fatalf("invalid submit produced commands: %#v", msgs)
		}
	}
}

func TestConfigMetaFormUpdatesGroupAndDescription(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "x=1\n")
	if err := mgr.Create("app", src, "/etc/app.conf", "ops", "old"); err != nil {
		t.Fatalf("create: %v", err)
	}
	tab := newLoadedConfigTab(t, mgr)
	tab.itemIndex = 0

	out, _ := tab.Update(runeKey("m"))
	tab = out.(*configTab)
	if tab.form == nil {
		t.Fatal("m should open the metadata form")
	}
	tab.form = replaceFormText(t, tab.form, "infra")
	tab.form, _ = tab.form.Update(tea.KeyMsg{Type: tea.KeyTab})
	tab.form = replaceFormText(t, tab.form, "new desc")
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*configTab)
	tab = flushConfig(tab, cmd)

	info, err := mgr.Get("app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Group != "infra" || info.Description != "new desc" {
		t.Fatalf("meta not updated: %+v", info)
	}
	if !hasConfigItem(tab, "app") {
		t.Error("entry disappeared after metadata edit")
	}
}

func TestConfigDetailShowsMetadata(t *testing.T) {
	mgr := newTestConfigManager(t)
	src := writeSourceFile(t, "x=1\n")
	if err := mgr.Create("app", src, "/etc/app.conf", "ops", "demo desc"); err != nil {
		t.Fatalf("create: %v", err)
	}
	tab := newLoadedConfigTab(t, mgr)
	tab.itemIndex = 0

	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab = out.(*configTab)
	tab = flushConfig(tab, cmd)
	view := tab.View()
	for _, want := range []string{"config detail", "ops", "demo desc", "/etc/app.conf"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q: %q", want, view)
		}
	}
}
