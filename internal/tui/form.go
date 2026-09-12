package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// This file implements the reusable structured form used by every multi-field
// edit in the TUI (SSH host, LLM provider, config metadata, group rename, ...).
//
// Contract (see the tui-forms capability):
//   - fields render in order; tab/shift+tab and up/down move between them;
//   - enter submits, esc cancels (cancel performs no write);
//   - validation errors render inline next to their field and keep the form
//     open with all input preserved;
//   - while a form is open the tab reports InputMode, so global keys (digits,
//     q, ?, S) never hijack typing.
//
// A form never touches storage: it collects and validates strings, then hands
// them back to the owning tab through formSubmitMsg.

// formFieldKind selects the widget used for one field.
type formFieldKind int

const (
	// formText is a single-line free text field.
	formText formFieldKind = iota
	// formSecret is a single-line field echoed as a mask.
	formSecret
	// formEnum picks one value out of a fixed candidate list.
	formEnum
	// formRef picks one identifier out of caller-supplied candidates (such as
	// an existing env/text entry or keypair); it never copies the referenced
	// content.
	formRef
	// formPath is a single-line filesystem path field.
	formPath
	// formEditor edits multi-line or free-form content through $EDITOR.
	formEditor
)

// formField is one editable slot of a form.
type formField struct {
	key         string // stable identifier used in the submitted values map
	label       string
	kind        formFieldKind
	value       string
	options     []string // candidates for formEnum / formRef
	optional    bool     // prepends an empty "无" choice for enum/ref
	validate    func(string) error
	placeholder string
	// preview, if set, replaces the default editor-field summary so a tab can
	// show identifiers (env keys) without echoing the field value.
	preview func(string) string
	// visible, if set, hides the field unless it returns true for the current
	// form values. Hidden fields are skipped by navigation, validation and
	// rendering, but keep their value (the tab decides what to submit).
	visible func(values map[string]string) bool
}

// enumOptions returns the selectable candidates, with an explicit empty choice
// first when the field is optional.
func (f formField) enumOptions() []string {
	if !f.optional {
		return f.options
	}
	return append([]string{""}, f.options...)
}

// formSubmitMsg carries the values of a successfully validated form.
type formSubmitMsg struct{ values map[string]string }

// formCancelMsg reports that the user dismissed the form with esc.
type formCancelMsg struct{}

// formEditorDoneMsg carries the result of an external-editor round trip back
// into the open form.
type formEditorDoneMsg struct {
	index int
	value string
	err   error
}

// form is a small field-list editor. It is embedded by value-less pointer in
// the tabs (nil == closed).
type form struct {
	title  string
	fields []formField
	index  int
	errs   []string
	input  textinput.Model

	// editExternal is supplied by the tab when the form has editor fields: it
	// opens $EDITOR on the current value and returns a command that yields
	// formEditorDoneMsg.
	editExternal func(index int, current string) tea.Cmd
}

func newForm(title string, fields ...formField) *form {
	ti := textinput.New()
	ti.CharLimit = 0
	ti.Prompt = ""
	f := &form{title: title, fields: fields, errs: make([]string, len(fields)), input: ti}
	f.syncInput()
	return f
}

// SetSize gives the form the available content area.
func (f *form) SetSize(width, height int) {
	f.input.Width = width - 8
	if f.input.Width < 8 {
		f.input.Width = 8
	}
}

// fieldVisible reports whether the field at index i participates in the form.
func (f *form) fieldVisible(i int) bool {
	if f.fields[i].visible == nil {
		return true
	}
	return f.fields[i].visible(f.Values())
}

// Values returns the collected field values keyed by formField.key.
func (f *form) Values() map[string]string {
	out := make(map[string]string, len(f.fields))
	for _, field := range f.fields {
		out[field.key] = field.value
	}
	return out
}

// SetValue overwrites one field value (used to prefill from the current row).
func (f *form) SetValue(key, value string) {
	for i := range f.fields {
		if f.fields[i].key == key {
			f.fields[i].value = value
		}
	}
	f.syncInput()
}

// fieldIndex returns the index of the named field, or -1.
func (f *form) fieldIndex(key string) int {
	for i := range f.fields {
		if f.fields[i].key == key {
			return i
		}
	}
	return -1
}

// InputMode is always true: an open form owns every keystroke.
func (f *form) InputMode() bool { return true }

// syncInput loads the active field into the shared text input.
func (f *form) syncInput() {
	field := &f.fields[f.index]
	f.input.SetValue(field.value)
	f.input.CursorEnd()
	if field.kind == formSecret {
		f.input.EchoMode = textinput.EchoPassword
	} else {
		f.input.EchoMode = textinput.EchoNormal
	}
	f.input.Placeholder = field.placeholder
	f.input.Focus()
}

// commitInput stores the shared input back into the active field.
func (f *form) commitInput() {
	if f.isTextLike() {
		f.fields[f.index].value = f.input.Value()
	}
}

// isTextLike reports whether the active field is edited through the text input
// (as opposed to a candidate list or the external editor).
func (f *form) isTextLike() bool {
	switch f.fields[f.index].kind {
	case formText, formSecret, formPath:
		return true
	}
	return false
}

// moveFocus changes the active field without wrapping, skipping hidden fields.
func (f *form) moveFocus(delta int) {
	f.commitInput()
	for {
		next := f.index + delta
		if next < 0 || next >= len(f.fields) {
			return
		}
		f.index = next
		if f.fieldVisible(f.index) {
			break
		}
	}
	f.syncInput()
}

// cycleOption moves the caret inside an enum/ref candidate list.
func (f *form) cycleOption(delta int) {
	field := &f.fields[f.index]
	options := field.enumOptions()
	if len(options) == 0 {
		return
	}
	current := 0
	for i, opt := range options {
		if opt == field.value {
			current = i
			break
		}
	}
	next := (current + delta + len(options)) % len(options)
	field.value = options[next]
	f.syncInput()
}

// validateAll runs each field validator and records inline errors. Hidden
// fields are not validated: their stale values must not block a submit.
func (f *form) validateAll() bool {
	ok := true
	for i := range f.fields {
		f.errs[i] = ""
		if f.fields[i].validate == nil || !f.fieldVisible(i) {
			continue
		}
		if err := f.fields[i].validate(f.fields[i].value); err != nil {
			f.errs[i] = err.Error()
			ok = false
		}
	}
	return ok
}

// Update handles one message. It returns the (possibly replaced) form plus a
// command; formSubmitMsg / formCancelMsg are delivered to the owning tab.
func (f *form) Update(msg tea.Msg) (*form, tea.Cmd) {
	switch msg := msg.(type) {
	case formEditorDoneMsg:
		if msg.index < 0 || msg.index >= len(f.fields) {
			return f, nil
		}
		if msg.err != nil {
			f.errs[msg.index] = msg.err.Error()
			return f, nil
		}
		f.fields[msg.index].value = msg.value
		f.errs[msg.index] = ""
		f.syncInput()
		return f, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return f, func() tea.Msg { return formCancelMsg{} }
		case "enter":
			f.commitInput()
			if !f.validateAll() {
				return f, nil
			}
			values := f.Values()
			return f, func() tea.Msg { return formSubmitMsg{values: values} }
		case "tab", "down":
			f.moveFocus(1)
			return f, nil
		case "shift+tab", "up":
			f.moveFocus(-1)
			return f, nil
		case "ctrl+o":
			return f, f.openEditor()
		}

		field := &f.fields[f.index]
		if !f.isTextLike() {
			switch msg.String() {
			case "left", "h", "right", "l", " ", "space":
				delta := 1
				if msg.String() == "left" || msg.String() == "h" {
					delta = -1
				}
				f.cycleOption(delta)
				return f, nil
			case "e":
				if field.kind == formEditor {
					return f, f.openEditor()
				}
			}
			return f, nil
		}

		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		f.fields[f.index].value = f.input.Value()
		return f, cmd
	}
	return f, nil
}

// openEditor hands the active editor field to the tab-provided $EDITOR hook.
func (f *form) openEditor() tea.Cmd {
	if f.fields[f.index].kind != formEditor {
		return nil
	}
	if f.editExternal == nil {
		return func() tea.Msg {
			return toastMsg{text: "this form does not support an external editor", level: toastWarn}
		}
	}
	f.commitInput()
	return f.editExternal(f.index, f.fields[f.index].value)
}

// View renders the form as a modal box.
func (f *form) View() string {
	lines := make([]string, 0, len(f.fields)*2+1)
	for i, field := range f.fields {
		if !f.fieldVisible(i) {
			continue
		}
		cursor := "  "
		label := field.label
		if i == f.index {
			cursor = "> "
			label = selectedLineStyle.Render(field.label)
		}
		lines = append(lines, cursor+label+": "+f.renderValue(i))
		if f.errs[i] != "" {
			lines = append(lines, "    "+warnBarStyle.Render("⚠ "+f.errs[i]))
		}
		// A focused enum/ref shows a short window of its candidates so the
		// user can see what arrow keys cycle through instead of discovering
		// options one keystroke at a time.
		lines = append(lines, f.enumPreview(i)...)
	}
	return modalBox(f.title, strings.Join(lines, "\n"), f.help())
}

// enumPreviewLines is how many candidate rows a focused enum/ref shows.
const enumPreviewLines = 5

// enumPreview renders a windowed candidate list for the focused enum/ref field.
// Non-enum fields, and every unfocused field, produce no extra rows.
func (f *form) enumPreview(i int) []string {
	if i != f.index {
		return nil
	}
	field := f.fields[i]
	if field.kind != formEnum && field.kind != formRef {
		return nil
	}
	options := field.enumOptions()
	if len(options) < 2 {
		return nil
	}
	current := 0
	for j, opt := range options {
		if opt == field.value {
			current = j
			break
		}
	}
	start, end := visibleRange(len(options), current, enumPreviewLines)
	lines := make([]string, 0, end-start)
	for j := start; j < end; j++ {
		label := options[j]
		if label == "" {
			label = "(none)"
		}
		if j == current {
			lines = append(lines, "      "+selectedLineStyle.Render("● "+label))
			continue
		}
		lines = append(lines, "      "+mutedStyle().Render("  "+label))
	}
	return lines
}

// renderValue renders one field's current value according to its kind. Secret
// values are never echoed, not even their length.
func (f *form) renderValue(i int) string {
	field := f.fields[i]
	switch field.kind {
	case formSecret:
		if field.value == "" {
			return mutedStyle().Render("(not set)")
		}
		return maskedValueStyle.Render("********")
	case formEnum, formRef:
		if field.value == "" {
			return mutedStyle().Render("(none)")
		}
		if i != f.index {
			return field.value
		}
		return selectedLineStyle.Render("‹ " + field.value + " ›")
	case formEditor:
		// Multi-line content is never echoed into the form; only a summary,
		// because the full text belongs in $EDITOR.
		if field.preview != nil {
			return mutedStyle().Render(field.preview(field.value))
		}
		lines := 0
		if trimmed := strings.TrimRight(field.value, "\n"); strings.TrimSpace(trimmed) != "" {
			lines = strings.Count(trimmed, "\n") + 1
		}
		return mutedStyle().Render(editorSummary(lines))
	default:
		if i == f.index {
			return f.input.View()
		}
		if field.value == "" {
			return mutedStyle().Render("(empty)")
		}
		return field.value
	}
}

// editorSummary describes multi-line content without dumping it into the form
// (long content belongs in $EDITOR, not in a one-line modal).
func editorSummary(lines int) string {
	if lines == 0 {
		return "(empty, press e to edit in $EDITOR)"
	}
	return "(" + strconv.Itoa(lines) + " lines, press e to edit in $EDITOR)"
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
}

// help returns the footer hint, including the editor key when relevant.
func (f *form) help() string {
	hint := "tab/↑↓ switch field · enter submit · esc cancel"
	for _, field := range f.fields {
		if field.kind == formEditor {
			hint += " · e/$EDITOR edit multi-line field"
			break
		}
	}
	return hint
}

// editorExecutable resolves the editor the same way the other edit paths do.
func editorExecutable() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	return "vim"
}

// externalEditorCmd opens $EDITOR on content through a 0600 temp file and
// returns a command that reports the edited text back as formEditorDoneMsg.
// It reuses the same closed loop as text/config editing (create 0600 -> edit ->
// read back -> remove) and adds no new crypto or temp-file path.
func externalEditorCmd(index int, prefix, content string) tea.Cmd {
	fail := func(err error) tea.Cmd {
		return func() tea.Msg { return formEditorDoneMsg{index: index, err: err} }
	}
	path, err := writeEditorTempFile(prefix, content)
	if err != nil {
		return fail(err)
	}
	cmd := exec.Command(editorExecutable(), path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		defer os.Remove(path)
		if runErr != nil {
			return formEditorDoneMsg{index: index, err: fmt.Errorf("editor exited abnormally: %w", runErr)}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return formEditorDoneMsg{index: index, err: fmt.Errorf("failed to read edited content: %w", err)}
		}
		return formEditorDoneMsg{index: index, value: string(data)}
	})
}

// writeEditorTempFile creates the 0600 scratch file handed to $EDITOR. Callers
// own cleanup; the content is written before any editor runs.
func writeEditorTempFile(prefix, content string) (string, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("failed to set temp file permissions: %w", err)
	}
	return path, nil
}
