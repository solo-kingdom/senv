package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ledger"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
	"github.com/wii/senv/internal/tree"
)

func newAll14Managers(t *testing.T) Managers {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	home := t.TempDir()
	key, err := sm.DeriveKeyFromPassword("pw")
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	return Managers{
		Env:       env.NewManager(sm, "pw"),
		Text:      text.NewManager(sm, "pw"),
		Backup:    backup.NewManager(sm, "pw"),
		Config:    config.NewManager(sm, "pw"),
		SSH:       ssh.NewManager(sm, "pw"),
		LLM:       llm.NewProviderManager(sm, "pw"),
		Skill:     skill.NewManager(sm, "pw"),
		SkillHome: home,
		MCP:       mcp.NewManager(sm, "pw"),
		Ledger:    ledger.NewManager(sm, key),
		Tree:      tree.NewManager(sm, key),
		MCPHome:   home,
		MCPLedger: filepath.Join(home, "mcp-exports.json"),
		History:   &fakeHistorySource{},
		Audit:     &fakeAuditSource{},
	}
}

func TestTabStripPrimaryFullAt120Cols(t *testing.T) {
	t.Cleanup(func() { applyIconSet(IconSetNerd) })
	applyIconSet(IconSetNerd)

	m := New(newAll14Managers(t))
	if len(m.tabs) != 14 {
		t.Fatalf("tabs = %d, want 14", len(m.tabs))
	}
	m.llmChrome = []llm.StatusRow{{Pointer: &llm.AgentPointer{Provider: "deepseek"}}}
	m.syncState = SyncState{Dirty: 0}

	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = out.(Model)
	// Restore badge after WindowSizeMsg (which calls refreshLLMChrome).
	m.llmChrome = []llm.StatusRow{{Pointer: &llm.AgentPointer{Provider: "deepseek"}}}

	strip := ansi.Strip(strings.Split(m.View(), "\n")[1])
	primaries := []string{"Env", "Text", "Providers", "Skill", "SSH", "KeyPair", "MCP", "Ledger", "Tree"}
	for _, title := range primaries {
		if !strings.Contains(strip, title) {
			t.Fatalf("120-col strip missing full primary title %q:\n%q", title, strip)
		}
	}
	if !strings.Contains(strip, iconMore()+"5") {
		t.Fatalf("120-col strip missing %q indicator:\n%q", iconMore()+"5", strip)
	}
	for _, hidden := range []string{"Config", "History", "Audit", "Backup", "Settings"} {
		if strings.Contains(strip, hidden) {
			t.Fatalf("120-col strip should hide inactive secondary tab %q:\n%q", hidden, strip)
		}
	}
}

func TestTabStripActiveSecondarySurfaces(t *testing.T) {
	m := New(newAll14Managers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = out.(Model)

	for _, target := range []string{"Config", "History", "Settings"} {
		idx := -1
		for i, tab := range m.tabs {
			if tab.Title() == target {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("missing tab %s", target)
		}
		m.active = idx
		view := m.View()
		if w, h := lipgloss.Width(view), lipgloss.Height(view); w != 120 || h != 30 {
			t.Fatalf("tab=%s view=%dx%d, want 120x30", target, w, h)
		}
		if !strings.Contains(view, renderTab(target, true)) {
			t.Fatalf("active secondary tab %s did not surface in top bar", target)
		}
	}
}

func TestTabSwitcherOpenFilterJumpAndClose(t *testing.T) {
	m := New(newAll14Managers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)

	// Not triggered while tab is in InputMode.
	out, _ = m.Update(runeKey("/"))
	m = out.(Model)
	if !m.tabs[m.active].InputMode() {
		t.Fatal("expected Env tab in InputMode after /")
	}
	for _, k := range []tea.KeyMsg{
		runeKey("0"),
		{Type: tea.KeyCtrlP},
		{Type: tea.KeyCtrlK},
	} {
		out, _ = m.Update(k)
		m = out.(Model)
		if m.switcher != nil {
			t.Fatalf("switcher must not open in InputMode for key %q", k.String())
		}
	}
	// Exit filter InputMode.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)

	// Each trigger opens switcher; esc closes without changing active tab.
	for _, k := range []tea.KeyMsg{
		runeKey("0"),
		{Type: tea.KeyCtrlP},
		{Type: tea.KeyCtrlK},
	} {
		out, _ = m.Update(k)
		m = out.(Model)
		if m.switcher == nil {
			t.Fatalf("expected switcher open after %q", k.String())
		}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "More") || !strings.Contains(view, "Primary") {
			t.Fatalf("empty filter switcher should show More and Primary sections:\n%s", view)
		}
		morePos := strings.Index(view, "More")
		primaryPos := strings.Index(view, "Primary")
		if morePos < 0 || primaryPos < 0 || morePos > primaryPos {
			t.Fatalf("More (%d) should appear before Primary (%d)", morePos, primaryPos)
		}
		out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = out.(Model)
		if cmd != nil {
			m = flushModel(m, cmd)
		}
		if m.switcher != nil {
			t.Fatalf("expected switcher closed on esc")
		}
		if m.active != 0 {
			t.Fatalf("active changed on esc: got %d, want 0", m.active)
		}
	}

	// Open switcher, type "backup" (contains 'k' which must type, not move cursor), jump with enter.
	out, _ = m.Update(runeKey("0"))
	m = out.(Model)
	for _, ch := range "backup" {
		out, _ = m.Update(runeKey(string(ch)))
		m = out.(Model)
	}
	if m.switcher.input != "backup" {
		t.Fatalf("switcher input = %q, want %q", m.switcher.input, "backup")
	}
	if len(m.switcher.filtered) != 1 || m.switcher.filtered[0].title != "Backup" {
		t.Fatalf("filtered = %+v, want [Backup]", m.switcher.filtered)
	}
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = out.(Model)
	if cmd != nil {
		m = flushModel(m, cmd)
	}
	if m.switcher != nil {
		t.Fatal("switcher should close after enter")
	}
	if got := m.tabs[m.active].Title(); got != "Backup" {
		t.Fatalf("active tab = %q, want Backup", got)
	}
	if !m.activated[m.active] {
		t.Fatal("expected jumped tab to be marked activated for lazy loading")
	}

	// Verify exact frame dimensions while switcher is open across terminal sizes.
	for _, sz := range [][2]int{{80, 24}, {120, 30}, {40, 12}} {
		out, _ = m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = out.(Model)
		out, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		m = out.(Model)
		v := m.View()
		if w, h := lipgloss.Width(v), lipgloss.Height(v); w != sz[0] || h != sz[1] {
			t.Fatalf("size=%dx%d switcher view=%dx%d, want exact %dx%d", sz[0], sz[1], w, h, sz[0], sz[1])
		}
		out, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = out.(Model)
		if cmd != nil {
			m = flushModel(m, cmd)
		}
	}
}

func TestCommaJumpsToSettings(t *testing.T) {
	m := New(newAll14Managers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)

	// Ignored in InputMode.
	out, _ = m.Update(runeKey("/"))
	m = out.(Model)
	out, _ = m.Update(runeKey(","))
	m = out.(Model)
	if m.active != 0 {
		t.Fatalf("comma must not jump in InputMode, active=%d", m.active)
	}
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)

	// Jumps straight to Settings when not in InputMode.
	out, _ = m.Update(runeKey(","))
	m = out.(Model)
	if got := m.tabs[m.active].Title(); got != "Settings" {
		t.Fatalf("active tab after comma = %q, want Settings", got)
	}
}
