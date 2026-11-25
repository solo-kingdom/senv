package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

func writeSkillDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	body := "---\nname: " + name + "\ndescription: test skill\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newSkillTestTab(t *testing.T) (*skillTab, string) {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	home := t.TempDir()
	tab := newSkillTab(Managers{
		Env:       env.NewManager(sm, "pw"),
		Text:      text.NewManager(sm, "pw"),
		Skill:     skill.NewManager(sm, "pw"),
		SkillHome: home,
	})
	tab.SetSize(120, 24)
	return tab, home
}

func loadSkillTab(t *testing.T, tab *skillTab) *skillTab {
	t.Helper()
	out := flushTab(tab, tab.load()).(*skillTab)
	if out.loadErr != "" {
		t.Fatalf("load error: %s", out.loadErr)
	}
	return out
}

func TestSkillTabOrderAfterProviders(t *testing.T) {
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	m := New(Managers{
		Env:    env.NewManager(sm, "pw"),
		Text:   text.NewManager(sm, "pw"),
		Config: config.NewManager(sm, "pw"),
		LLM:    llm.NewProviderManager(sm, "pw"),
		Skill:  skill.NewManager(sm, "pw"),
	})
	titles := make([]string, 0, len(m.tabs))
	for _, tab := range m.tabs {
		titles = append(titles, tab.Title())
	}
	prov, skillAt := -1, -1
	for i, title := range titles {
		if title == "Providers" {
			prov = i
		}
		if title == "Skill" {
			skillAt = i
		}
	}
	if prov < 0 || skillAt != prov+1 {
		t.Fatalf("Skill must follow Providers, got %v", titles)
	}
}

func TestSkillTabApplyUnapplyPlanAndStatus(t *testing.T) {
	tab, home := newSkillTestTab(t)
	src := writeSkillDir(t, "demo-skill")
	if _, err := tab.mgr.Skill.Add(skill.AddOptions{ID: "demo-skill", SourceDir: src, Description: "demo"}); err != nil {
		t.Fatal(err)
	}
	tab = loadSkillTab(t, tab)
	if len(tab.entries) != 1 || tab.entries[0].ID != "demo-skill" {
		t.Fatalf("entries = %+v", tab.entries)
	}
	if len(tab.status) != 2 || tab.status[0].State != "not applied" {
		t.Fatalf("status before apply = %+v", tab.status)
	}

	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	tab = out.(*skillTab)
	if tab.mode != skillModePlan || tab.planKind != "apply" || len(tab.planItems) != 2 {
		t.Fatalf("apply plan mode=%v kind=%s items=%+v", tab.mode, tab.planKind, tab.planItems)
	}
	for _, it := range tab.planItems {
		if it.Action != skill.ActionCreate {
			t.Fatalf("expected create, got %+v", it)
		}
	}
	view := tab.View()
	if !strings.Contains(view, "apply plan") || !strings.Contains(view, "demo-skill") {
		t.Fatalf("plan view = %q", view)
	}

	tab = flushTab(tab, func() tea.Cmd {
		_, c := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		return c
	}()).(*skillTab)
	_ = cmd
	tab = loadSkillTab(t, tab)
	for _, st := range tab.status {
		if st.State != "applied" {
			t.Fatalf("status after apply = %+v", tab.status)
		}
	}
	for _, root := range skill.DefaultRoots {
		marker := filepath.Join(home, root, "demo-skill", skill.OwnedMarker)
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("missing owned marker %s: %v", marker, err)
		}
	}

	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	tab = out.(*skillTab)
	if tab.mode != skillModePlan || tab.planKind != "unapply" {
		t.Fatalf("unapply plan mode=%v kind=%s", tab.mode, tab.planKind)
	}
	tab = flushTab(tab, func() tea.Cmd {
		_, c := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		return c
	}()).(*skillTab)
	tab = loadSkillTab(t, tab)
	for _, st := range tab.status {
		if st.State != "not applied" {
			t.Fatalf("status after unapply = %+v", tab.status)
		}
	}
	for _, root := range skill.DefaultRoots {
		path := filepath.Join(home, root, "demo-skill")
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("skill dir still present: %s (%v)", path, err)
		}
	}
}

func TestSkillTabGroupSidebar(t *testing.T) {
	tab, _ := newSkillTestTab(t)
	if _, err := tab.mgr.Skill.AddGroup(skill.GroupOptions{Name: "empty", Description: "unused"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.mgr.Skill.Add(skill.AddOptions{ID: "alpha-skill", SourceDir: writeSkillDir(t, "alpha-skill"), Group: "ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tab.mgr.Skill.Add(skill.AddOptions{ID: "beta-skill", SourceDir: writeSkillDir(t, "beta-skill"), Group: "tools"}); err != nil {
		t.Fatal(err)
	}
	tab = loadSkillTab(t, tab)
	if tab.focus != skillPaneGroup || tab.selectedGroupName() != "All" {
		t.Fatalf("focus=%v group=%s", tab.focus, tab.selectedGroupName())
	}
	view := tab.View()
	for _, want := range []string{"All", "empty", "ops", "tools", "ops/alpha-skill", "tools/beta-skill"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	names := make([]string, len(tab.groups))
	for i, g := range tab.groups {
		names[i] = g.name
	}
	if strings.Join(names, ",") != "All,empty,ops,tools" {
		t.Fatalf("group order = %v", names)
	}
	if tab.groups[1].count != 0 || tab.groups[0].count != 2 {
		t.Fatalf("counts = %+v", tab.groups)
	}

	tab = skillKey(t, tab, "down")
	tab = skillKey(t, tab, "down")
	if tab.selectedGroupName() != "ops" {
		t.Fatalf("group = %s", tab.selectedGroupName())
	}
	ids := skillIDs(tab)
	if len(ids) != 1 || ids[0] != "alpha-skill" {
		t.Fatalf("ops skills = %v", ids)
	}
	if strings.Contains(tab.View(), "beta-skill") {
		t.Fatal("ops pane still shows beta-skill")
	}

	tab = skillKey(t, tab, "/")
	tab = skillKey(t, tab, "b")
	tab = skillKey(t, tab, "e")
	if got := groupCount(tab, "All"); got != 1 {
		t.Fatalf("filtered All count = %d", got)
	}
	if got := groupCount(tab, "ops"); got != 0 {
		t.Fatalf("filtered ops count = %d", got)
	}
	if got := groupCount(tab, "tools"); got != 1 {
		t.Fatalf("filtered tools count = %d", got)
	}
	if len(skillIDs(tab)) != 0 {
		t.Fatalf("ops filter should hide alpha, got %v", skillIDs(tab))
	}

	tab = skillKey(t, tab, "right")
	if tab.focus != skillPaneList {
		t.Fatalf("focus after right = %v", tab.focus)
	}
	tab = skillKey(t, tab, "right")
	if tab.focus != skillPaneTarget {
		t.Fatalf("focus after second right = %v", tab.focus)
	}
}

func skillKey(t *testing.T, tab *skillTab, key string) *skillTab {
	t.Helper()
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return out.(*skillTab)
}

func skillIDs(tab *skillTab) []string {
	entries := tab.visibleEntries()
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

func groupCount(tab *skillTab, name string) int {
	for _, g := range tab.groups {
		if g.name == name {
			return g.count
		}
	}
	return -1
}

func TestSkillTabScopeToggle(t *testing.T) {
	tab, _ := newSkillTestTab(t)
	tab = loadSkillTab(t, tab)
	if tab.scopeOrDefault() != "global" {
		t.Fatalf("default scope = %q", tab.scope)
	}
	out, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	tab = out.(*skillTab)
	if tab.scopeOrDefault() != "project" {
		t.Fatalf("after s scope = %q", tab.scope)
	}
}
