package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

// skillTab 浏览 vault 中的私有 skill，并对 agents/claude 根做 apply / unapply。
// 交互对齐 MCP Tab：左栏档案、右栏目标状态、计划页确认后再写盘。
type skillTab struct {
	mgr           Managers
	width, height int
	loaded        bool
	loadErr       string
	warning       string

	entries []storage.SkillEntry
	status  []skillTargetStatus

	skillIndex  int
	targetIndex int
	focusLeft   bool
	filterBox   Filter
	detail      *detailOverlay

	// scope：global / project；会话内有效，与 CLI 默认 global 一致。
	scope string

	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode      skillMode
	pendingID string
	planKind  string // "apply" | "unapply"
	planItems []skill.PlanItem
	planAll   bool // true = 全部目标根；false = 仅当前右栏目标
}

type skillMode int

const (
	skillModeNormal skillMode = iota
	skillModeDelete
	skillModePlan
)

type skillTargetStatus struct {
	ID     string // agents / claude
	Root   string
	Path   string
	State  string // not applied / applied / drifted / conflict / error
	Reason string
}

type skillLoadedMsg struct {
	entries []storage.SkillEntry
	status  []skillTargetStatus
	warning string
	err     error
}

type skillReloadMsg struct {
	toast string
	id    string
	warn  bool
}

func newSkillTab(mgr Managers) *skillTab {
	return &skillTab{mgr: mgr, focusLeft: true, scope: "global"}
}

func (t *skillTab) Title() string { return "Skill" }

func (t *skillTab) Bindings() []KeyAction {
	if t.detail != nil {
		return detailBindings()
	}
	if t.form != nil {
		return formBindings()
	}
	if t.filterBox.Active() {
		return filterBindings(true)
	}
	switch t.mode {
	case skillModeDelete:
		return confirmBindings()
	case skillModePlan:
		return []KeyAction{
			{[]string{"enter/y"}, "confirm", grpConfirm, false},
			{[]string{"esc/n"}, "cancel", grpConfirm, false},
		}
	}
	return append(navBindings(true),
		actNew,
		actDelete,
		actDetail,
		KeyAction{[]string{"s"}, "scope", grpItem, false},
		KeyAction{[]string{"x"}, "apply", grpItem, false},
		KeyAction{[]string{"X"}, "apply all targets", grpItem, false},
		KeyAction{[]string{"u"}, "unapply", grpItem, false},
		KeyAction{[]string{"U"}, "unapply all targets", grpItem, false},
		actFilter,
		actRefresh,
	)
}

func (t *skillTab) InputMode() bool {
	return t.form != nil || t.filterBox.Active() || t.detail != nil ||
		t.mode == skillModeDelete || t.mode == skillModePlan
}

func (t *skillTab) SetSize(width, height int) {
	t.width, t.height = width, height
	if t.form != nil {
		t.form.SetSize(width, height)
	}
	if t.detail != nil {
		t.detail.SetSize(width, height)
	}
}

func (t *skillTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *skillTab) Reload() tea.Cmd { return t.load() }

func (t *skillTab) scopeOrDefault() string {
	if t.scope == "" {
		return "global"
	}
	return t.scope
}

func (t *skillTab) load() tea.Cmd {
	return func() tea.Msg {
		st := perflog.Start("tui.load-skill")
		if t.mgr.Skill == nil {
			st.End(true)
			return skillLoadedMsg{}
		}
		entries, err := t.mgr.Skill.List("")
		if err != nil {
			st.End(false)
			return skillLoadedMsg{err: err}
		}
		status, warning := t.statusFor(entries)
		st.With("skills", len(entries)).End(true)
		return skillLoadedMsg{entries: entries, status: status, warning: warning}
	}
}

func (t *skillTab) statusFor(entries []storage.SkillEntry) ([]skillTargetStatus, string) {
	roots := append([]string(nil), skill.DefaultRoots...)
	out := make([]skillTargetStatus, 0, len(roots))
	for _, root := range roots {
		out = append(out, skillTargetStatus{
			ID:    skill.TargetShortName(root),
			Root:  root,
			State: "not applied",
		})
	}
	id := t.currentIDFrom(entries)
	if id == "" {
		return out, ""
	}
	applier, err := t.applier(roots)
	if err != nil {
		return out, err.Error()
	}
	entry, archive, err := t.mgr.Skill.Archive(id)
	if err != nil {
		return out, err.Error()
	}
	plan, err := applier.PlanWithExtra(id, archive, entry.ExtraInfo)
	if err != nil {
		return out, err.Error()
	}
	byRoot := map[string]skill.PlanItem{}
	for _, item := range plan {
		byRoot[item.Root] = item
	}
	for i := range out {
		item, ok := byRoot[out[i].Root]
		if !ok {
			continue
		}
		out[i].Path = item.Target
		out[i].Reason = item.Reason
		out[i].State = skillStateForAction(item.Action)
	}
	return out, ""
}

func skillStateForAction(a skill.Action) string {
	switch a {
	case skill.ActionSkip:
		return "applied"
	case skill.ActionBackupOverwrite:
		return "drifted"
	case skill.ActionSkipConflict:
		return "conflict"
	case skill.ActionCreate:
		return "not applied"
	default:
		return "error"
	}
}

func (t *skillTab) currentIDFrom(entries []storage.SkillEntry) string {
	visible := t.visibleEntriesFrom(entries)
	if len(visible) == 0 {
		return ""
	}
	idx := t.skillIndex
	if idx < 0 || idx >= len(visible) {
		idx = 0
	}
	return visible[idx].ID
}

func (t *skillTab) currentID() string {
	return t.currentIDFrom(t.entries)
}

func (t *skillTab) currentEntry() *storage.SkillEntry {
	visible := t.visibleEntries()
	if len(visible) == 0 {
		return nil
	}
	idx := t.skillIndex
	if idx < 0 || idx >= len(visible) {
		return nil
	}
	return &visible[idx]
}

func (t *skillTab) visibleEntries() []storage.SkillEntry {
	return t.visibleEntriesFrom(t.entries)
}

func (t *skillTab) visibleEntriesFrom(entries []storage.SkillEntry) []storage.SkillEntry {
	term := strings.ToLower(strings.TrimSpace(t.filterBox.Term()))
	if term == "" {
		return entries
	}
	out := make([]storage.SkillEntry, 0, len(entries))
	for _, e := range entries {
		hay := strings.ToLower(e.ID + " " + e.NormalizedGroup() + " " + e.Description)
		if strings.Contains(hay, term) {
			out = append(out, e)
		}
	}
	return out
}

func (t *skillTab) clampLeft() {
	n := len(t.visibleEntries())
	if t.skillIndex >= n {
		t.skillIndex = maxInt(n-1, 0)
	}
}

func (t *skillTab) clampRight() {
	n := len(t.status)
	if t.targetIndex >= n {
		t.targetIndex = maxInt(n-1, 0)
	}
}

func (t *skillTab) syncStatus() {
	status, warning := t.statusFor(t.entries)
	t.status = status
	t.warning = warning
	t.clampRight()
}

func (t *skillTab) homeDir() (string, error) {
	if t.mgr.SkillHome != "" {
		return t.mgr.SkillHome, nil
	}
	return os.UserHomeDir()
}

func (t *skillTab) projectDir() (string, error) {
	if t.mgr.SkillProject != "" {
		return t.mgr.SkillProject, nil
	}
	return os.Getwd()
}

func (t *skillTab) baseDir() (string, error) {
	if t.scopeOrDefault() == "project" {
		return t.projectDir()
	}
	return t.homeDir()
}

func (t *skillTab) applier(roots []string) (*skill.Applier, error) {
	base, err := t.baseDir()
	if err != nil {
		return nil, err
	}
	home, err := t.homeDir()
	if err != nil {
		return nil, err
	}
	a := skill.NewApplier(base)
	if len(roots) > 0 {
		a.Roots = append([]string(nil), roots...)
	}
	if t.scopeOrDefault() == "project" {
		a.StateDir = filepath.Join(home, ".local", "state", "senv", "skill-apply")
	}
	return a, nil
}

func (t *skillTab) selectedRoots(all bool) []string {
	if all || len(t.status) == 0 {
		return append([]string(nil), skill.DefaultRoots...)
	}
	idx := t.targetIndex
	if idx < 0 || idx >= len(t.status) {
		idx = 0
	}
	return []string{t.status[idx].Root}
}

type skillJumpMsg struct{ id string }

func (t *skillTab) cancelMode() {
	t.mode = skillModeNormal
	t.planItems = nil
	t.pendingID = ""
}

func (t *skillTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && t.filterBox.Active() && t.form == nil && t.mode == skillModeNormal {
		switch key.String() {
		case "esc":
			t.filterBox.Clear()
			t.clampLeft()
			t.syncStatus()
			return t, nil
		case "enter":
			t.filterBox.Confirm()
			t.syncStatus()
			return t, nil
		case "backspace":
			t.filterBox.Backspace()
			t.clampLeft()
			t.syncStatus()
			return t, nil
		}
		if isPrintable(key) {
			t.filterBox.Append(key.String())
			t.clampLeft()
			t.syncStatus()
			return t, nil
		}
	}
	switch msg := msg.(type) {
	case formSubmitMsg:
		submit := t.formSubmit
		t.form = nil
		t.formSubmit = nil
		if submit == nil {
			return t, nil
		}
		return t, submit(msg.values)
	case formCancelMsg:
		t.form = nil
		t.formSubmit = nil
		return t, warnToast("cancelled")
	}
	if t.form != nil {
		next, cmd := t.form.Update(msg)
		t.form = next
		return t, cmd
	}
	switch msg := msg.(type) {
	case skillLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			t.entries = nil
			t.status = nil
			return t, nil
		}
		t.entries = msg.entries
		t.status = msg.status
		t.warning = msg.warning
		t.clampLeft()
		t.clampRight()
		return t, nil
	case skillReloadMsg:
		t.cancelMode()
		cmd := t.load()
		batch := []tea.Cmd{cmd}
		if msg.toast != "" {
			if msg.warn {
				batch = append([]tea.Cmd{warnToast(msg.toast)}, batch...)
			} else {
				batch = append([]tea.Cmd{okToast(msg.toast)}, batch...)
			}
		}
		if msg.id != "" {
			batch = append(batch, func() tea.Msg { return skillJumpMsg{id: msg.id} })
		}
		return t, tea.Batch(batch...)
	case skillJumpMsg:
		t.filterBox.Clear()
		for i, e := range t.visibleEntries() {
			if e.ID == msg.id {
				t.skillIndex = i
				t.syncStatus()
				break
			}
		}
		return t, nil
	case detailCloseMsg:
		t.detail = nil
		return t, nil
	case tea.KeyMsg:
		if t.detail != nil {
			var cmd tea.Cmd
			t.detail, cmd = t.detail.Update(msg)
			return t, cmd
		}
		switch t.mode {
		case skillModeDelete:
			return t.handleDeleteKey(msg.String())
		case skillModePlan:
			return t.handlePlanKey(msg.String())
		}
		return t.handleKey(msg)
	}
	return t, nil
}

func (t *skillTab) handleKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		t.focusLeft = true
	case "right", "l":
		t.focusLeft = false
	case "up", "k":
		if t.focusLeft {
			if t.skillIndex > 0 {
				t.skillIndex--
				t.syncStatus()
			}
		} else if t.targetIndex > 0 {
			t.targetIndex--
		}
	case "down", "j":
		if t.focusLeft {
			if t.skillIndex+1 < len(t.visibleEntries()) {
				t.skillIndex++
				t.syncStatus()
			}
		} else if t.targetIndex+1 < len(t.status) {
			t.targetIndex++
		}
	case "g":
		if t.focusLeft {
			t.skillIndex = 0
			t.syncStatus()
		} else {
			t.targetIndex = 0
		}
	case "G":
		if t.focusLeft {
			t.skillIndex = maxInt(len(t.visibleEntries())-1, 0)
			t.syncStatus()
		} else {
			t.targetIndex = maxInt(len(t.status)-1, 0)
		}
	case "/":
		if t.focusLeft {
			t.filterBox.EnterFresh()
		}
	case "ctrl+r":
		return t, t.load()
	case "n":
		return t.enterAddForm()
	case "d":
		return t.enterDelete()
	case "enter":
		return t.openDetail()
	case "s":
		if t.scopeOrDefault() == "project" {
			t.scope = "global"
		} else {
			t.scope = "project"
		}
		t.syncStatus()
	case "x":
		return t.enterPlan("apply", false)
	case "X":
		return t.enterPlan("apply", true)
	case "u":
		return t.enterPlan("unapply", false)
	case "U":
		return t.enterPlan("unapply", true)
	}
	return t, nil
}

func (t *skillTab) enterAddForm() (Tab, tea.Cmd) {
	t.form = newForm("add skill",
		formField{key: "id", label: "id", kind: formText, validate: storage.ValidateSkillID},
		formField{key: "source", label: "source dir", kind: formText, placeholder: "/path/to/skill",
			validate: func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("source is required")
				}
				return nil
			}},
		optionalDescriptionField(""),
		formField{key: "group", label: "group", kind: formText, optional: true, placeholder: "default"},
		formField{key: "force", label: "force replace", kind: formEnum, options: []string{"no", "yes"}, value: "no"},
	)
	t.form.SetSize(t.width, t.height)
	t.formSubmit = t.submitAdd
	return t, nil
}

func (t *skillTab) submitAdd(values map[string]string) tea.Cmd {
	id := strings.TrimSpace(values["id"])
	source := strings.TrimSpace(values["source"])
	if strings.HasPrefix(source, "~/") {
		if home, err := t.homeDir(); err == nil {
			source = filepath.Join(home, source[2:])
		}
	}
	opts := skill.AddOptions{
		ID:          id,
		SourceDir:   source,
		Group:       strings.TrimSpace(values["group"]),
		Description: values["description"],
		Force:       values["force"] == "yes",
	}
	mgrs := t.mgr
	return func() tea.Msg {
		if mgrs.Skill == nil {
			return skillReloadMsg{toast: "skill manager unavailable", warn: true}
		}
		entry, err := mgrs.Skill.Add(opts)
		if err != nil {
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, false, "add failed")
			return skillReloadMsg{toast: err.Error(), warn: true}
		}
		recordAudit(mgrs, session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id, true, "add")
		return skillReloadMsg{toast: "added " + id, id: id}
	}
}

func (t *skillTab) enterDelete() (Tab, tea.Cmd) {
	id := t.currentID()
	if id == "" {
		return t, warnToast("no skill selected")
	}
	t.pendingID = id
	t.mode = skillModeDelete
	return t, nil
}

func (t *skillTab) handleDeleteKey(key string) (Tab, tea.Cmd) {
	switch key {
	case "esc", "n":
		t.mode = skillModeNormal
		t.pendingID = ""
		return t, nil
	case "enter", "y":
		id := t.pendingID
		t.mode = skillModeNormal
		t.pendingID = ""
		mgrs := t.mgr
		return t, func() tea.Msg {
			if err := mgrs.Skill.Remove(id); err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, false, "delete failed")
				return skillReloadMsg{toast: err.Error(), warn: true}
			}
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, true, "delete")
			return skillReloadMsg{toast: "removed " + id + " from vault (local applies kept)"}
		}
	}
	return t, nil
}

func (t *skillTab) openDetail() (Tab, tea.Cmd) {
	id := t.currentID()
	if id == "" {
		return t, warnToast("no skill selected")
	}
	detail, err := t.mgr.Skill.Show(id)
	if err != nil {
		return t, warnToast(err.Error())
	}
	lines := []string{
		"id: " + detail.Entry.ID,
		"group: " + detail.Entry.NormalizedGroup(),
		"description: " + detail.Entry.Description,
		fmt.Sprintf("archive: %d bytes", detail.ArchiveSize),
		"files:",
	}
	for _, e := range detail.Entries {
		kind := "file"
		if e.Dir {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("  %s %s (%d)", kind, e.Path, e.Size))
	}
	t.detail = newDetailOverlay("Skill "+id, lines)
	t.detail.SetSize(t.width, t.height)
	return t, nil
}

func (t *skillTab) enterPlan(kind string, all bool) (Tab, tea.Cmd) {
	id := t.currentID()
	if id == "" {
		return t, warnToast("no skill selected")
	}
	roots := t.selectedRoots(all)
	applier, err := t.applier(roots)
	if err != nil {
		return t, warnToast(err.Error())
	}
	var items []skill.PlanItem
	switch kind {
	case "apply":
		entry, archive, err := t.mgr.Skill.Archive(id)
		if err != nil {
			return t, warnToast(err.Error())
		}
		items, err = applier.PlanWithExtra(id, archive, entry.ExtraInfo)
		if err != nil {
			return t, warnToast(err.Error())
		}
	case "unapply":
		items = applier.UnapplyPlan(id)
	default:
		return t, warnToast("unknown plan kind")
	}
	t.planKind = kind
	t.planAll = all
	t.planItems = items
	t.pendingID = id
	t.mode = skillModePlan
	return t, nil
}

func (t *skillTab) handlePlanKey(key string) (Tab, tea.Cmd) {
	switch key {
	case "esc", "n":
		t.mode = skillModeNormal
		t.planItems = nil
		t.pendingID = ""
		return t, nil
	case "enter", "y":
		return t, t.executePlan()
	}
	return t, nil
}

func (t *skillTab) executePlan() tea.Cmd {
	id := t.pendingID
	kind := t.planKind
	all := t.planAll
	roots := t.selectedRoots(all)
	mgrs := t.mgr
	scope := t.scopeOrDefault()
	home := t.mgr.SkillHome
	project := t.mgr.SkillProject
	return func() tea.Msg {
		base := home
		if scope == "project" {
			base = project
			if base == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return skillReloadMsg{toast: err.Error(), warn: true, id: id}
				}
				base = cwd
			}
		} else if base == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			base = h
		}
		stateHome := home
		if stateHome == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			stateHome = h
		}
		applier := skill.NewApplier(base)
		applier.Roots = roots
		if scope == "project" {
			applier.StateDir = filepath.Join(stateHome, ".local", "state", "senv", "skill-apply")
		}
		changes := 0
		failed := false
		switch kind {
		case "apply":
			entry, archive, err := mgrs.Skill.Archive(id)
			if err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, false, "apply failed")
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			results, err := applier.ApplyWithExtra(id, archive, entry.ExtraInfo)
			if err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, false, "apply failed")
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			for _, r := range results {
				if r.Err != nil {
					failed = true
				} else if r.Action.Changes() {
					changes++
				}
			}
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, !failed, fmt.Sprintf("apply %d changes", changes))
			if failed {
				return skillReloadMsg{toast: "apply completed with errors", warn: true, id: id}
			}
			if changes == 0 {
				return skillReloadMsg{toast: "nothing to apply", id: id}
			}
			return skillReloadMsg{toast: fmt.Sprintf("applied %s (%d)", id, changes), id: id}
		case "unapply":
			results := applier.Unapply(id)
			for _, r := range results {
				if r.Err != nil {
					failed = true
				} else if r.Action.Changes() {
					changes++
				}
			}
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, !failed, fmt.Sprintf("unapply %d changes", changes))
			if failed {
				return skillReloadMsg{toast: "unapply completed with errors", warn: true, id: id}
			}
			if changes == 0 {
				return skillReloadMsg{toast: "nothing to unapply", id: id}
			}
			return skillReloadMsg{toast: fmt.Sprintf("unapplied %s (%d)", id, changes), id: id}
		default:
			return skillReloadMsg{toast: "unknown plan kind", warn: true, id: id}
		}
	}
}

func (t *skillTab) View() string {
	if t.loadErr != "" {
		return mutedStyle().Render("Skill: " + t.loadErr)
	}
	var overlay string
	switch {
	case t.form != nil:
		overlay = t.form.View()
	case t.detail != nil:
		overlay = t.detail.View()
	case t.mode == skillModeDelete:
		overlay = modalBox(t.width, t.height, "delete "+t.pendingID+" from vault?",
			"only removes the vault entry; applied skill directories stay on disk.\nuse u/U to unapply managed trees.",
			"enter/y confirm · esc/n cancel")
	case t.mode == skillModePlan:
		overlay = t.renderPlan()
	}
	body := t.renderPanes()
	if overlay != "" {
		return overlayBox(body, overlay, t.width, t.height)
	}
	return body
}

func (t *skillTab) renderPlan() string {
	var b strings.Builder
	fmt.Fprintf(&b, "skill: %s\nscope: %s\n", t.pendingID, t.scopeOrDefault())
	for _, it := range t.planItems {
		fmt.Fprintf(&b, "  [%s] %s\n    %s\n", it.Action, it.Target, it.Reason)
	}
	title := t.planKind + " plan"
	if t.planAll {
		title += " (all targets)"
	}
	return modalBox(t.width, t.height, title, strings.TrimRight(b.String(), "\n"),
		"enter/y confirm · esc/n cancel")
}

func (t *skillTab) renderPanes() string {
	leftW := t.width * 55 / 100
	if leftW < 24 {
		leftW = 24
	}
	rightW := t.width - leftW - 1
	if rightW < 20 {
		rightW = 20
		leftW = t.width - rightW - 1
	}
	height := maxInt(t.height, 3)
	leftTitle := fmt.Sprintf("skills (%d)", len(t.visibleEntries()))
	if t.filterBox.Active() {
		leftTitle += "  " + t.filterBox.Prompt()
	}
	left := windowedPane(leftTitle, t.skillLines(maxInt(leftW-4, 8)), t.skillIndex, height, leftW)
	right := windowedPane(fmt.Sprintf("targets · apply status (scope: %s)", t.scopeOrDefault()),
		t.targetLines(maxInt(rightW-4, 8)), t.targetIndex, height, rightW)
	if t.focusLeft {
		left = activePaneStyle.Width(leftW).Height(height).Render(left)
		right = paneStyle.Width(rightW).Height(height).Render(right)
	} else {
		left = paneStyle.Width(leftW).Height(height).Render(left)
		right = activePaneStyle.Width(rightW).Height(height).Render(right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", 1), right)
}

func (t *skillTab) skillLines(width int) []string {
	entries := t.visibleEntries()
	lines := make([]string, 0, len(entries))
	for i, e := range entries {
		desc := e.Description
		if desc == "" {
			desc = "-"
		}
		line := fmt.Sprintf("%s · [%s] · %s", e.ID, e.NormalizedGroup(), desc)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.skillIndex))
	}
	return lines
}

func (t *skillTab) targetLines(width int) []string {
	lines := make([]string, 0, len(t.status)+1)
	if t.warning != "" {
		lines = append(lines, mutedStyle().Render(truncateWidth("WARN "+t.warning, width)))
	}
	for i, row := range t.status {
		line := fmt.Sprintf("%s · %s", row.ID, row.State)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.targetIndex))
	}
	return lines
}
