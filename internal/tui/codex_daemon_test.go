package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// codexPromptModel builds a model with a confirmation overlay already staged.
func codexPromptModel(t *testing.T) Model {
	t.Helper()
	m := New(Managers{})
	m.width, m.height = 100, 40
	m.codexPrompt = &codexDaemonPrompt{source: "senv ai switch codex"}
	m.codexPromptStage = codexPromptConfirm
	return m
}

func TestCodexPromptCheckStartsProbe(t *testing.T) {
	m := New(Managers{})
	next, cmd := m.Update(codexDaemonCheckMsg{source: "senv mcp export"})
	got := next.(Model)
	if got.codexPrompt == nil {
		t.Fatal("check message did not stage an overlay")
	}
	if got.codexPromptStage != codexPromptProbing {
		t.Fatalf("stage = %v, want probing", got.codexPromptStage)
	}
	if cmd == nil {
		t.Fatal("check message produced no probe command")
	}
}

// No daemon means nothing to restart: the overlay must disappear quietly rather
// than report a failure the user cannot act on.
func TestCodexPromptStaysSilentWhenNoDaemonRuns(t *testing.T) {
	m := New(Managers{})
	m.codexPrompt = &codexDaemonPrompt{source: "senv mcp export"}
	m.codexPromptStage = codexPromptProbing

	next, _ := m.Update(codexDaemonStateMsg{running: false})
	if got := next.(Model); got.codexPrompt != nil {
		t.Fatal("overlay survived a stopped probe")
	}
}

func TestCodexPromptStaysSilentOnProbeFailure(t *testing.T) {
	m := New(Managers{})
	m.codexPrompt = &codexDaemonPrompt{source: "senv mcp export"}
	m.codexPromptStage = codexPromptProbing

	next, _ := m.Update(codexDaemonStateMsg{err: errors.New("boom")})
	if got := next.(Model); got.codexPrompt != nil {
		t.Fatal("overlay survived a failed probe")
	}
}

func TestCodexPromptAsksForConfirmationWhenDaemonRuns(t *testing.T) {
	m := New(Managers{})
	m.codexPrompt = &codexDaemonPrompt{source: "senv ai switch codex"}
	m.codexPromptStage = codexPromptProbing

	next, _ := m.Update(codexDaemonStateMsg{running: true})
	got := next.(Model)
	if got.codexPrompt == nil {
		t.Fatal("running daemon did not keep the overlay")
	}
	if got.codexPromptStage != codexPromptConfirm {
		t.Fatalf("stage = %v, want confirm", got.codexPromptStage)
	}
	got.width, got.height = 100, 40
	view := got.renderCodexPrompt()
	if !strings.Contains(view, "restart") {
		t.Fatalf("prompt does not mention restart:\n%s", view)
	}
}

// Keys pressed while the probe is in flight must not count as consent.
func TestCodexPromptIgnoresKeysWhileProbing(t *testing.T) {
	m := New(Managers{})
	m.codexPrompt = &codexDaemonPrompt{source: "senv mcp export"}
	m.codexPromptStage = codexPromptProbing

	for _, key := range []string{"y", "enter"} {
		next, cmd := m.Update(runeKey(key))
		got := next.(Model)
		if got.codexPrompt == nil {
			t.Fatalf("%q dismissed the overlay while probing", key)
		}
		if cmd != nil {
			t.Fatalf("%q triggered a command while probing", key)
		}
	}
}

func TestCodexPromptCancelSkipsRestart(t *testing.T) {
	m := codexPromptModel(t)

	next, _ := m.Update(runeKey("n"))
	got := next.(Model)
	if got.codexPrompt != nil {
		t.Fatal("cancel did not close the overlay")
	}
}

func TestCodexPromptEnterRunsRestart(t *testing.T) {
	m := codexPromptModel(t)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.codexPrompt != nil {
		t.Fatal("confirm did not close the overlay")
	}
	if cmd == nil {
		t.Fatal("confirm produced no restart command")
	}
}

func TestCodexPromptOverlayOwnsKeys(t *testing.T) {
	m := codexPromptModel(t)
	// A global shortcut must not reach the tab strip while the modal is open.
	next, _ := m.Update(runeKey("q"))
	if got := next.(Model); got.codexPrompt == nil {
		t.Fatal("global q closed the modal")
	}
}

func TestCodexPromptRestartErrorSurfaces(t *testing.T) {
	m := New(Managers{})
	next, cmd := m.Update(codexDaemonRestartMsg{err: errors.New("daemon died")})
	if cmd == nil {
		t.Fatal("restart failure produced no error command")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("restart failure produced %T, want errMsg", msg)
	}
	if got := next.(Model); got.codexPrompt != nil {
		t.Fatal("restart result left the overlay open")
	}
}

func TestCodexPromptRestartSuccessToasts(t *testing.T) {
	m := New(Managers{})
	_, cmd := m.Update(codexDaemonRestartMsg{})
	if cmd == nil {
		t.Fatal("successful restart produced no toast")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("restart success produced %T, want toastMsg", msg)
	}
	if !strings.Contains(toast.text, "codex app-server daemon") {
		t.Fatalf("toast = %q", toast.text)
	}
}

func TestCodexPromptBindingsAreConfirmation(t *testing.T) {
	bs := codexPromptBindings()
	if len(bs) != 2 {
		t.Fatalf("bindings = %v, want confirm/cancel only", bs)
	}
}
