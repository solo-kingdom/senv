package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func formKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// newTestForm builds a form mirroring the real multi-field edits: a text field,
// a secret field and an enum with an empty choice.
func newTestForm() *form {
	return newForm("测试表单",
		formField{key: "name", label: "名称", kind: formText, validate: func(v string) error {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("名称不能为空")
			}
			return nil
		}},
		formField{key: "token", label: "令牌", kind: formSecret},
		formField{key: "mode", label: "模式", kind: formEnum, options: []string{"a", "b"}, optional: true},
	)
}

func typeIntoForm(f *form, keys ...string) (*form, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		f, cmd = f.Update(formKey(k))
	}
	return f, cmd
}

func TestFormTabTraversalAndSubmit(t *testing.T) {
	f := newTestForm()
	f.SetSize(80, 20)

	f, _ = typeIntoForm(f, "a", "p", "p")
	f, _ = f.Update(formKey("tab"))
	f, _ = typeIntoForm(f, "s", "e", "c")
	f, _ = f.Update(formKey("tab"))
	// Enum field: choose "b" via the candidate list.
	f, _ = f.Update(formKey("right"))
	f, _ = f.Update(formKey("right"))

	f, cmd := f.Update(formKey("enter"))
	if cmd == nil {
		t.Fatal("enter should submit a valid form")
	}
	msg, ok := cmd().(formSubmitMsg)
	if !ok {
		t.Fatalf("submit produced %T, want formSubmitMsg", cmd())
	}
	if msg.values["name"] != "app" || msg.values["token"] != "sec" || msg.values["mode"] != "b" {
		t.Fatalf("submitted values = %#v", msg.values)
	}
}

func TestFormValidationKeepsInputAndBlocksSubmit(t *testing.T) {
	f := newTestForm()
	f.SetSize(80, 20)

	// "name" is required; submit with it empty.
	f, cmd := f.Update(formKey("enter"))
	if cmd != nil {
		if _, isSubmit := cmd().(formSubmitMsg); isSubmit {
			t.Fatal("invalid form must not submit")
		}
	}
	if !strings.Contains(f.View(), "名称不能为空") {
		t.Errorf("view should show the inline validation error, got %q", f.View())
	}

	// Fill in the other fields, then go back and fix the first one: nothing is
	// lost by a failed validation.
	f, _ = f.Update(formKey("tab"))
	f, _ = typeIntoForm(f, "k", "e", "y")
	f, _ = f.Update(formKey("shift+tab"))
	if f.index != 0 {
		t.Fatalf("shift+tab moved to %d, want 0", f.index)
	}
	f, _ = typeIntoForm(f, "n", "m")
	f, cmd = f.Update(formKey("enter"))
	if cmd == nil {
		t.Fatal("expected submit command after fixing the field")
	}
	msg, ok := cmd().(formSubmitMsg)
	if !ok {
		t.Fatalf("got %T, want formSubmitMsg", cmd())
	}
	if msg.values["name"] != "nm" || msg.values["token"] != "key" {
		t.Fatalf("values after re-submit = %#v", msg.values)
	}
}

func TestFormCancelProducesNoSubmit(t *testing.T) {
	f := newTestForm()
	f.SetSize(80, 20)
	f, _ = typeIntoForm(f, "x")
	f, cmd := f.Update(formKey("esc"))
	if cmd == nil {
		t.Fatal("esc should return a cancel command")
	}
	if _, ok := cmd().(formCancelMsg); !ok {
		t.Fatalf("esc produced %T, want formCancelMsg", cmd())
	}
}

func TestFormSecretNeverEchoesPlaintext(t *testing.T) {
	f := newTestForm()
	f.SetSize(80, 20)
	f, _ = f.Update(formKey("tab")) // focus the secret field
	f, _ = typeIntoForm(f, "h", "u", "n", "t", "e", "r", "2")
	view := f.View()
	if strings.Contains(view, "hunter2") {
		t.Fatalf("secret plaintext leaked into the form: %q", view)
	}
	if !strings.Contains(view, "****") {
		t.Errorf("secret should render as a mask, got %q", view)
	}
}

func TestFormEnumIncludesEmptyChoice(t *testing.T) {
	f := newForm("t", formField{key: "k", label: "l", kind: formEnum, options: []string{"a"}, optional: true})
	f.SetSize(80, 20)
	f.SetValue("k", "a")
	f, _ = f.Update(formKey("right")) // a -> "" (wrap)
	if got := f.Values()["k"]; got != "" {
		t.Fatalf("optional enum should cycle to the empty choice, got %q", got)
	}
	f, _ = f.Update(formKey("left"))
	if got := f.Values()["k"]; got != "a" {
		t.Fatalf("cycling back should select a, got %q", got)
	}
}

func TestFormEditorFieldUsesExternalHook(t *testing.T) {
	called := 0
	f := newForm("t", formField{key: "extra", label: "额外属性", kind: formEditor})
	f.SetSize(80, 20)
	f.editExternal = func(index int, current string) tea.Cmd {
		called++
		if index != 0 {
			t.Errorf("editor hook index = %d, want 0", index)
		}
		return func() tea.Msg { return formEditorDoneMsg{index: index, value: "a=1\nb=2\n"} }
	}
	f, cmd := f.Update(formKey("e"))
	if called != 1 || cmd == nil {
		t.Fatalf("editor hook not invoked (called=%d cmd=%v)", called, cmd)
	}
	f, _ = f.Update(cmd())
	if got := f.Values()["extra"]; got != "a=1\nb=2\n" {
		t.Fatalf("editor result not stored, got %q", got)
	}
	if !strings.Contains(f.View(), "2 行") {
		t.Errorf("editor field should render a line summary, got %q", f.View())
	}
}

func TestFormInputModeBlocksGlobalKeys(t *testing.T) {
	f := newTestForm()
	if !f.InputMode() {
		t.Fatal("an open form must report InputMode")
	}
	// While focused on a text field, digits and q are plain input.
	f, _ = typeIntoForm(f, "7", "q", "?")
	if got := f.Values()["name"]; got != "7q?" {
		t.Fatalf("global keys were hijacked, name = %q", got)
	}
}

func TestEditorTempFileIsPrivateAndRoundTrips(t *testing.T) {
	path, err := writeEditorTempFile("senv-form-test-*", "line1\nline2\n")
	if err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("temp file mode = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Errorf("temp content = %q", data)
	}
}
