package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/storage"
)

// TestPanesNeverWrapLongValues pins the hard rule that pane content is
// truncated, never wrapped: a long value must not add rows or widen the frame.
func TestPanesNeverWrapLongValues(t *testing.T) {
	mgrs := newFullManagers(t)
	long := strings.Repeat("very-long-host-", 12)
	if err := mgrs.SSH.AddHost(&storage.HostEntry{Alias: long, Hostname: long}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	models := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		models = append(models, fmt.Sprintf("model-%02d-%s", i, strings.Repeat("x", 30)))
	}
	if _, err := mgrs.LLM.AddProvider(llm.AddProviderOptions{
		Alias: "main", BaseURL: "https://api.example.com/some/very/long/path/v1",
		APIKey: "k", Models: models,
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	for _, tc := range []struct {
		name string
		tab  Tab
	}{
		{"ssh", newSSHTab(mgrs)},
		{"ai", newAITab(mgrs)},
	} {
		tab := tc.tab
		tab.SetSize(80, 20)
		if cmd := tab.Init(); cmd != nil {
			msg := cmd()
			if msg != nil {
				next, _ := tab.Update(msg)
				tab = next
			}
		}
		view := tab.View()
		if h := lipgloss.Height(view); h > paneBudget(20) {
			t.Fatalf("%s: rendered height = %d, want ≤ %d (content wrapped)", tc.name, h, paneBudget(20))
		}
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > 80 {
				t.Fatalf("%s: line %d width %d > 80: %q", tc.name, i, w, line)
			}
		}
	}
}

func TestToastBarRendersAndExpires(t *testing.T) {
	m := New(newFullManagers(t))
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)

	out, cmd := m.Update(toastMsg{text: "已复制 FOO", level: toastSuccess})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("toast did not schedule an expiry")
	}
	if !strings.Contains(m.View(), "已复制 FOO") {
		t.Fatal("toast not rendered in the bottom bar")
	}

	// A stale expiry timer must not clear a newer toast.
	out, _ = m.Update(toastMsg{text: "新提示", level: toastSuccess})
	m = out.(Model)
	stale := m.toastSeq - 1
	out, _ = m.Update(clearToastMsg{seq: stale})
	m = out.(Model)
	if !strings.Contains(m.View(), "新提示") {
		t.Fatal("stale expiry cleared a newer toast")
	}

	out, _ = m.Update(clearToastMsg{seq: m.toastSeq})
	m = out.(Model)
	if strings.Contains(m.View(), "新提示") {
		t.Fatal("toast did not expire")
	}

	// Errors take precedence over a success toast.
	out, _ = m.Update(toastMsg{text: "成功提示", level: toastSuccess})
	m = out.(Model)
	out, _ = m.Update(errMsg{err: errOf("boom")})
	m = out.(Model)
	if v := m.View(); !strings.Contains(v, "boom") {
		t.Fatalf("error must take precedence over the toast:\n%s", v)
	}
}
