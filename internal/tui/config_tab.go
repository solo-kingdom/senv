package tui

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/config"
)

// configTab renders Config data as a single-column list (no group concept).
// Shows name / target path / updated time; content only via vim edit or detail.
type configTab struct {
	mgr           Managers
	width, height int

	items     []configRow
	itemIndex int
	loaded    bool

	filter    string
	filtering bool

	input         textinput.Model
	mode          configMode
	flash         string
	pendingName   string // staging create: name then source then target
	pendingSource string
	detail        *configDetail // set when viewing details
}

type configRow struct {
	name       string
	targetPath string
	updatedAt  string
}

type configDetail struct {
	name       string
	targetPath string
	createdAt  string
	updatedAt  string
}

type configMode int

const (
	configModeNormal configMode = iota
	configModeDetail
	configModeDeleteConfirm
	configModeExportPath
	configModeCreateName
	configModeCreateSource
	configModeCreateTarget
	configModeFilter
)

func newConfigTab(mgr Managers) *configTab {
	ti := textinput.New()
	ti.CharLimit = 0
	return &configTab{mgr: mgr, input: ti}
}

func (t *configTab) Title() string { return "Config" }

func (t *configTab) Help() string {
	return "↑↓/jk move · enter details · e vim edit · n new · x export · d del · / filter"
}

func (t *configTab) InputMode() bool {
	switch t.mode {
	case configModeFilter, configModeExportPath, configModeCreateName,
		configModeCreateSource, configModeCreateTarget:
		return true
	}
	return false
}

// --- data loading ---

type configLoadedMsg struct {
	items []configRow
	err   error
}

type configReloadMsg struct{}

// configDetailLoadedMsg carries the result of Get for the detail panel.
type configDetailLoadedMsg struct {
	name string
	det  *configDetail
	err  error
}

func (t *configTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

// focusJump positions the cursor at the given config name for search-result
// navigation. It also dismisses detail/filter modes.
func (t *configTab) focusJump(name string) {
	t.filter = ""
	t.mode = configModeNormal
	t.detail = nil
	for i, it := range t.items {
		if it.name == name {
			t.itemIndex = i
			break
		}
	}
}

func (t *configTab) load() tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if mgr == nil {
			return configLoadedMsg{err: fmt.Errorf("config manager unavailable")}
		}
		cfgs, err := mgr.List()
		if err != nil {
			return configLoadedMsg{err: err}
		}
		rows := make([]configRow, 0, len(cfgs))
		for _, c := range cfgs {
			rows = append(rows, configRow{name: c.Name, targetPath: c.TargetPath, updatedAt: c.UpdatedAt})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
		return configLoadedMsg{items: rows}
	}
}

func (t *configTab) filteredItems() []configRow {
	if t.filter == "" {
		return t.items
	}
	out := make([]configRow, 0, len(t.items))
	for _, it := range t.items {
		if matchKey(it.name, t.filter) {
			out = append(out, it)
		}
	}
	return out
}

func (t *configTab) currentItem() (configRow, bool) {
	items := t.filteredItems()
	if t.itemIndex < 0 || t.itemIndex >= len(items) {
		return configRow{}, false
	}
	return items[t.itemIndex], true
}

// --- update ---

func (t *configTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case configLoadedMsg:
		if msg.err != nil {
			err := msg.err
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.items = msg.items
		t.loaded = true
		t.itemIndex = clamp(t.itemIndex, 0, maxLen(t.filteredItems())-1)
		return t, nil

	case configReloadMsg:
		return t, t.load()

	case configDetailLoadedMsg:
		if msg.err != nil {
			err := msg.err
			t.mode = configModeNormal
			return t, func() tea.Msg { return errMsg{err: err} }
		}
		t.detail = msg.det
		t.mode = configModeDetail
		return t, nil

	case tea.KeyMsg:
		t.flash = ""

		if t.mode != configModeNormal && t.mode != configModeDetail {
			return t.handleModalKey(msg)
		}

		// Detail view: any key returns to the list (except esc which is explicit).
		if t.mode == configModeDetail {
			t.mode = configModeNormal
			t.detail = nil
			return t, nil
		}

		switch msg.String() {
		case "up", "k":
			t.itemIndex = clamp(t.itemIndex-1, 0, maxLen(t.filteredItems())-1)
		case "down", "j":
			t.itemIndex = clamp(t.itemIndex+1, 0, maxLen(t.filteredItems())-1)
		case "g":
			t.itemIndex = 0
		case "G":
			t.itemIndex = clamp(len(t.filteredItems())-1, 0, maxLen(t.filteredItems())-1)
		case "enter":
			return t.showDetail()
		case "e":
			return t.editCurrent()
		case "n":
			return t.enterCreateName()
		case "x":
			return t.doExportCurrent()
		case "d":
			return t.enterDeleteConfirm()
		case "/":
			return t.enterFilterMode()
		}
	}
	return t, nil
}

// --- modal handling ---

func (t *configTab) handleModalKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	if t.mode == configModeDeleteConfirm {
		switch msg.String() {
		case "enter", "y":
			it, ok := t.currentItem()
			t.mode = configModeNormal
			if !ok {
				return t, nil
			}
			return t, t.doDelete(it.name)
		default:
			t.mode = configModeNormal
			return t, nil
		}
	}

	switch msg.String() {
	case "esc":
		if t.mode == configModeFilter {
			t.filter = ""
		}
		t.mode = configModeNormal
		t.input.Blur()
		return t, nil
	case "enter":
		return t.submitModal()
	}

	if t.mode == configModeFilter {
		switch msg.String() {
		case "backspace":
			if len(t.filter) > 0 {
				t.filter = t.filter[:len(t.filter)-1]
			}
			t.itemIndex = 0
			return t, nil
		}
		if isPrintable(msg) {
			t.filter += msg.String()
			t.itemIndex = 0
		}
		return t, nil
	}

	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return t, cmd
}

func (t *configTab) submitModal() (Tab, tea.Cmd) {
	switch t.mode {
	case configModeExportPath:
		path := t.input.Value()
		it, ok := t.currentItem()
		t.mode = configModeNormal
		t.input.Blur()
		if !ok {
			return t, nil
		}
		return t, t.doExport(it.name, path)
	case configModeCreateName:
		name := t.input.Value()
		if name == "" {
			t.flash = "name cannot be empty"
			return t, nil
		}
		t.pendingName = name
		t.input.SetValue("")
		t.input.Placeholder = "source file path"
		t.mode = configModeCreateSource
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateSource:
		src := t.input.Value()
		if src == "" {
			t.flash = "source path cannot be empty"
			return t, nil
		}
		t.pendingSource = src
		t.input.SetValue("")
		t.input.Placeholder = "target file path"
		t.mode = configModeCreateTarget
		t.input.Focus()
		return t, textinput.Blink
	case configModeCreateTarget:
		target := t.input.Value()
		name := t.pendingName
		src := t.pendingSource
		t.mode = configModeNormal
		t.pendingName = ""
		t.pendingSource = ""
		t.input.Blur()
		if target == "" {
			t.flash = "target path cannot be empty"
			return t, nil
		}
		return t, t.doCreate(name, src, target)
	}
	t.mode = configModeNormal
	return t, nil
}

// --- entry points ---

func (t *configTab) showDetail() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item selected"
		return t, nil
	}
	mgr := t.mgr.Config
	name := it.name
	return t, func() tea.Msg {
		ci, err := mgr.Get(name)
		if err != nil {
			return configDetailLoadedMsg{name: name, err: err}
		}
		return configDetailLoadedMsg{name: name, det: &configDetail{
			name: ci.Name, targetPath: ci.TargetPath,
			createdAt: ci.CreatedAt, updatedAt: ci.UpdatedAt,
		}}
	}
}

func (t *configTab) editCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to edit"
		return t, nil
	}
	mgr := t.mgr.Config
	if mgr == nil {
		return t, func() tea.Msg { return errMsg{err: fmt.Errorf("config manager unavailable")} }
	}
	session, err := mgr.PrepareEdit(it.name)
	if err != nil {
		err := err
		return t, func() tea.Msg { return errMsg{err: err} }
	}
	t.flash = "opening editor…"
	return t, tea.ExecProcess(session.EditorCommand(), func(runErr error) tea.Msg {
		return t.finishAfterEdit(session, runErr)
	})
}

// finishAfterEdit is the post-editor callback for config: on editor failure it
// cleans up the temp file and reports an error without persisting; otherwise it
// commits the edit. Extracted so task 11.3 is unit-testable.
func (t *configTab) finishAfterEdit(session *config.ConfigEditSession, runErr error) tea.Msg {
	if runErr != nil {
		os.Remove(session.TmpPath)
		return errMsg{err: fmt.Errorf("editor failed: %w", runErr)}
	}
	if _, ferr := t.mgr.Config.FinishEdit(session); ferr != nil {
		return errMsg{err: ferr}
	}
	return configReloadMsg{}
}

func (t *configTab) enterCreateName() (Tab, tea.Cmd) {
	t.mode = configModeCreateName
	t.pendingName = ""
	t.pendingSource = ""
	t.input.SetValue("")
	t.input.Placeholder = "config name"
	t.input.Focus()
	return t, textinput.Blink
}

func (t *configTab) enterDeleteConfirm() (Tab, tea.Cmd) {
	if _, ok := t.currentItem(); !ok {
		t.flash = "no item to delete"
		return t, nil
	}
	t.mode = configModeDeleteConfirm
	return t, nil
}

func (t *configTab) enterFilterMode() (Tab, tea.Cmd) {
	t.mode = configModeFilter
	t.filter = ""
	return t, nil
}

// --- operations ---

func (t *configTab) doDelete(name string) tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if err := mgr.Delete(name); err != nil {
			return errMsg{err: err}
		}
		return configReloadMsg{}
	}
}

// doExportCurrent exports to the config's default target path.
func (t *configTab) doExportCurrent() (Tab, tea.Cmd) {
	it, ok := t.currentItem()
	if !ok {
		t.flash = "no item to export"
		return t, nil
	}
	return t, t.doExport(it.name, "") // empty -> config's TargetPath
}

func (t *configTab) doExport(name, path string) tea.Cmd {
	mgr := t.mgr.Config
	t.flash = "exported " + name
	return func() tea.Msg {
		if err := mgr.Export(name, path); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (t *configTab) doCreate(name, source, target string) tea.Cmd {
	mgr := t.mgr.Config
	return func() tea.Msg {
		if err := mgr.Create(name, source, target); err != nil {
			return errMsg{err: err}
		}
		return configReloadMsg{}
	}
}

// --- view ---

func (t *configTab) SetSize(w, h int) { t.width, t.height = w, h }

func (t *configTab) View() string {
	if t.mode == configModeDetail && t.detail != nil {
		return t.renderDetail()
	}
	base := t.renderList()
	if t.mode == configModeNormal {
		if t.flash != "" {
			return lipgloss.JoinVertical(lipgloss.Left, base,
				statusBarStyle.Foreground(lipgloss.Color(colorSuccess)).Render(t.flash))
		}
		return base
	}
	return lipgloss.JoinVertical(lipgloss.Left, base, t.renderModal())
}

func (t *configTab) renderList() string {
	if !t.loaded {
		return emptyStateStyle.Render("loading configs...")
	}
	items := t.filteredItems()
	header := fmt.Sprintf("Configs (%d)", len(items))
	if t.filter != "" {
		header += "  /" + t.filter
	}
	title := paneTitleStyle.Render(header)
	if len(items) == 0 {
		hint := "no configuration files"
		if t.filter != "" {
			hint = "no names match /" + t.filter
		}
		return lipgloss.JoinVertical(lipgloss.Left, title, emptyStateStyle.Render(hint))
	}
	var lines []string
	for i, it := range items {
		line := fmt.Sprintf("%-18s %-24s %s", it.name, truncPath(it.targetPath), it.updatedAt)
		if i == t.itemIndex {
			line = selectedLineStyle.Render("▸ " + line)
		}
		lines = append(lines, line)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width(t.width-2) because lipgloss draws the pane border outside Width;
	// total rendered width = t.width, fitting the content area exactly.
	return activePaneStyle.Width(t.width - 2).Height(t.height).Render(
		lipgloss.JoinVertical(lipgloss.Left, title, body))
}

func (t *configTab) renderDetail() string {
	d := t.detail
	body := fmt.Sprintf(
		"Name:    %s\nTarget:  %s\nCreated: %s\nUpdated: %s",
		d.name, d.targetPath, d.createdAt, d.updatedAt)
	box := modalBox("Config details", body, "any key to close")
	return box
}

func (t *configTab) renderModal() string {
	switch t.mode {
	case configModeDeleteConfirm:
		it, _ := t.currentItem()
		return modalBox("Delete "+it.name+"?", "", "enter/y confirm · esc/n cancel")
	case configModeExportPath:
		return modalBox("Export to file", t.input.View(), "enter export · esc cancel")
	case configModeCreateName:
		return modalBox("New config — name", t.input.View(), "enter next · esc cancel")
	case configModeCreateSource:
		return modalBox("Source file path for "+t.pendingName, t.input.View(), "enter next · esc cancel")
	case configModeCreateTarget:
		return modalBox("Target file path for "+t.pendingName, t.input.View(), "enter create · esc cancel")
	case configModeFilter:
		return modalBox("Filter names (case-insensitive)", "/"+t.filter+"_", "esc to clear")
	}
	return ""
}

// truncPath shortens a long target path for list display.
func truncPath(p string) string {
	r := []rune(p)
	if len(r) <= 24 {
		return p
	}
	return "…" + string(r[len(r)-23:])
}

// ensure config package is referenced (Manager used via Managers; ConfigInfo
// referenced indirectly through load). This guard keeps the import meaningful.
var _ = config.ConfigInfo{}

// Compile-time guard: *configTab satisfies Tab.
var _ Tab = (*configTab)(nil)
