package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

// skillTab 浏览 vault 中的私有 skill，并对 agents/claude 根做 apply / unapply。
// 三栏：分组侧栏（All + 各组）/ 当前组的 skill / 目标根应用状态。计划页确认后再写盘。
type skillTab struct {
	mgr           Managers
	width, height int
	loaded        bool
	loadErr       string
	warning       string

	entries   []storage.SkillEntry
	groupMeta []skill.GroupSummary
	groups    []skillGroupRow
	status    []skillTargetStatus

	groupIndex  int
	skillIndex  int
	targetIndex int
	focus       skillPane
	filterBox   Filter
	detail      *detailOverlay

	// scope：global / project；会话内有效，与 CLI 默认 global 一致。
	scope string

	form       *form
	formSubmit func(values map[string]string) tea.Cmd

	mode        skillMode
	pendingID   string
	planKind    string // "apply" | "unapply"
	planItems   []skill.PlanItem
	planAll     bool   // true = 全部目标根；false = 仅当前右栏目标
	planVersion string // 非空 = 按指定版本快照 apply

	versionsID     string
	versionsActive string
	versionsList   []storage.SkillVersion
	versionsIndex  int
}

type skillMode int

const (
	skillModeNormal skillMode = iota
	skillModeDelete
	skillModePlan
	skillModeVersions
)

// skillPane 是三栏焦点。零值是分组侧栏，和 Env 打开时停在 All 一致。
type skillPane int

const (
	skillPaneGroup skillPane = iota
	skillPaneList
	skillPaneTarget
)

// skillAllLabel 是侧栏顶部的 All 伪组标签。
const skillAllLabel = "All"

// skillGroupRow 是分组侧栏一行。count 随当前过滤变化。
type skillGroupRow struct {
	name        string
	description string
	count       int
	isAll       bool
}

type skillTargetStatus struct {
	ID            string // agents / claude
	Root          string
	Path          string
	State         string // not applied / applied / drifted / conflict / error
	VersionSuffix string // 如 " (v1)" / " (v1 → v2)" / " (v2, local)"
	Reason        string
}

type skillLoadedMsg struct {
	entries []storage.SkillEntry
	groups  []skill.GroupSummary
	err     error
}

type skillReloadMsg struct {
	toast string
	id    string
	warn  bool
}

func newSkillTab(mgr Managers) *skillTab {
	return &skillTab{mgr: mgr, focus: skillPaneGroup, scope: "global"}
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
	case skillModeVersions:
		return []KeyAction{
			{[]string{"↑↓/jk"}, "select", grpNav, false},
			{[]string{"enter/u"}, "use active", grpItem, false},
			{[]string{"x"}, "apply version", grpItem, false},
			{[]string{"d"}, "delete version", grpItem, false},
			{[]string{"esc"}, "close", grpConfirm, false},
		}
	}
	return append(navBindings(true),
		actNew,
		actDelete,
		actDetail,
		KeyAction{[]string{"v"}, "versions", grpItem, false},
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
		t.mode == skillModeDelete || t.mode == skillModePlan || t.mode == skillModeVersions
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
		groups, err := t.mgr.Skill.ListGroups()
		if err != nil {
			st.End(false)
			return skillLoadedMsg{err: err}
		}
		st.With("skills", len(entries), "groups", len(groups)).End(true)
		return skillLoadedMsg{entries: entries, groups: groups}
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
	entry, archive, targetVer, err := t.mgr.Skill.ArchiveVersion(id, "")
	if err != nil {
		return out, err.Error()
	}
	plan, err := applier.PlanWithOptions(id, archive, skill.ApplyOptions{
		ExtraInfo: entry.ExtraInfo,
		Version:   targetVer,
	})
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
		out[i].VersionSuffix = skillVersionSuffix(item)
	}
	return out, ""
}

func skillVersionSuffix(item skill.PlanItem) string {
	switch item.Action {
	case skill.ActionSkip:
		if item.TargetVersion != "" {
			return " (" + item.TargetVersion + ")"
		}
	case skill.ActionBackupOverwrite:
		switch {
		case item.InstalledVersion != "" && item.TargetVersion != "" && item.InstalledVersion != item.TargetVersion:
			return fmt.Sprintf(" (%s → %s)", item.InstalledVersion, item.TargetVersion)
		case item.TargetVersion != "":
			return fmt.Sprintf(" (%s, local)", item.TargetVersion)
		}
	}
	return ""
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

func (t *skillTab) entryMatchesFilter(e storage.SkillEntry) bool {
	term := strings.ToLower(strings.TrimSpace(t.filterBox.Term()))
	if term == "" {
		return true
	}
	hay := strings.ToLower(e.ID + " " + e.NormalizedGroup() + " " + e.Description)
	return strings.Contains(hay, term)
}

func (t *skillTab) currentGroupRow() (skillGroupRow, bool) {
	if t.groupIndex < 0 || t.groupIndex >= len(t.groups) {
		return skillGroupRow{}, false
	}
	return t.groups[t.groupIndex], true
}

func (t *skillTab) selectedGroupName() string {
	if row, ok := t.currentGroupRow(); ok {
		return row.name
	}
	return skillAllLabel
}

// rebuildGroups 用已登记分组和条目上的分组名重建侧栏，计数只含当前过滤命中的条目。
// 已选分组名在重建后保留；第一次打开落在 All。
func (t *skillTab) rebuildGroups() {
	selected := t.selectedGroupName()
	counts := map[string]int{}
	for _, e := range t.entries {
		if t.entryMatchesFilter(e) {
			counts[e.NormalizedGroup()]++
		}
	}
	rows := make([]skillGroupRow, 0, len(t.groupMeta)+1)
	seen := map[string]bool{}
	for _, g := range t.groupMeta {
		name := g.Group.Name
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		rows = append(rows, skillGroupRow{
			name:        name,
			description: g.Group.Description,
			count:       counts[name],
		})
	}
	var extra []string
	for name := range counts {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		rows = append(rows, skillGroupRow{name: name, count: counts[name]})
	}
	total := 0
	for _, row := range rows {
		total += row.count
	}
	t.groups = append([]skillGroupRow{{name: skillAllLabel, isAll: true, count: total}}, rows...)
	t.groupIndex = 0
	for i, g := range t.groups {
		if g.name == selected {
			t.groupIndex = i
			break
		}
	}
}

func (t *skillTab) noteFilterChanged() {
	t.rebuildGroups()
	t.clampLeft()
	t.syncStatus()
}

func (t *skillTab) visibleEntriesFrom(entries []storage.SkillEntry) []storage.SkillEntry {
	group := ""
	if row, ok := t.currentGroupRow(); ok && !row.isAll {
		group = row.name
	}
	out := make([]storage.SkillEntry, 0, len(entries))
	for _, e := range entries {
		if group != "" && e.NormalizedGroup() != group {
			continue
		}
		if !t.entryMatchesFilter(e) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := out[i].NormalizedGroup(), out[j].NormalizedGroup()
		if gi != gj {
			return gi < gj
		}
		return out[i].ID < out[j].ID
	})
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
	t.planVersion = ""
	t.versionsID = ""
	t.versionsActive = ""
	t.versionsList = nil
	t.versionsIndex = 0
}

func (t *skillTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && t.filterBox.Active() && t.form == nil && t.mode == skillModeNormal {
		switch key.String() {
		case "esc":
			t.filterBox.Clear()
			t.noteFilterChanged()
			return t, nil
		case "enter":
			t.filterBox.Confirm()
			t.noteFilterChanged()
			return t, nil
		case "backspace":
			t.filterBox.Backspace()
			t.noteFilterChanged()
			return t, nil
		}
		if isPrintable(key) {
			t.filterBox.Append(key.String())
			t.noteFilterChanged()
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
			t.groupMeta = nil
			t.groups = nil
			t.status = nil
			return t, nil
		}
		t.entries = msg.entries
		t.groupMeta = msg.groups
		t.rebuildGroups()
		t.clampLeft()
		t.syncStatus()
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
		t.rebuildGroups()
		var target *storage.SkillEntry
		for i := range t.entries {
			if t.entries[i].ID == msg.id {
				target = &t.entries[i]
				break
			}
		}
		if target != nil {
			if row, ok := t.currentGroupRow(); ok && !row.isAll && row.name != target.NormalizedGroup() {
				for i, g := range t.groups {
					if g.name == target.NormalizedGroup() {
						t.groupIndex = i
						t.skillIndex = 0
						break
					}
				}
			}
		}
		for i, e := range t.visibleEntries() {
			if e.ID == msg.id {
				t.skillIndex = i
				t.focus = skillPaneList
				break
			}
		}
		t.syncStatus()
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
		case skillModeVersions:
			return t.handleVersionsKey(msg.String())
		}
		return t.handleKey(msg)
	}
	return t, nil
}

func (t *skillTab) handleKey(msg tea.KeyMsg) (Tab, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		if t.focus > skillPaneGroup {
			t.focus--
		}
	case "right", "l":
		if t.focus < skillPaneTarget {
			t.focus++
		}
	case "up", "k":
		t.moveFocus(-1)
	case "down", "j":
		t.moveFocus(1)
	case "pgup":
		t.moveFocus(-pageStep(t.height))
	case "pgdown":
		t.moveFocus(pageStep(t.height))
	case "g":
		t.jumpFocus(true)
	case "G":
		t.jumpFocus(false)
	case "/":
		if t.focus != skillPaneTarget {
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
	case "v":
		return t.enterVersions()
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

func (t *skillTab) moveFocus(delta int) {
	switch t.focus {
	case skillPaneGroup:
		if len(t.groups) == 0 {
			return
		}
		next := clamp(t.groupIndex+delta, 0, len(t.groups)-1)
		if next == t.groupIndex {
			return
		}
		t.groupIndex = next
		t.skillIndex = 0
		t.syncStatus()
	case skillPaneList:
		n := len(t.visibleEntries())
		next := clamp(t.skillIndex+delta, 0, maxInt(n-1, 0))
		if next == t.skillIndex {
			return
		}
		t.skillIndex = next
		t.syncStatus()
	default:
		n := len(t.status)
		t.targetIndex = clamp(t.targetIndex+delta, 0, maxInt(n-1, 0))
	}
}

func (t *skillTab) jumpFocus(top bool) {
	switch t.focus {
	case skillPaneGroup:
		if len(t.groups) == 0 {
			return
		}
		if top {
			t.groupIndex = 0
		} else {
			t.groupIndex = maxInt(len(t.groups)-1, 0)
		}
		t.skillIndex = 0
		t.syncStatus()
	case skillPaneList:
		if top {
			t.skillIndex = 0
		} else {
			t.skillIndex = maxInt(len(t.visibleEntries())-1, 0)
		}
		t.syncStatus()
	default:
		if top {
			t.targetIndex = 0
		} else {
			t.targetIndex = maxInt(len(t.status)-1, 0)
		}
	}
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
		formField{key: "version", label: "version", kind: formText, optional: true, placeholder: "auto (v1 / frontmatter)"},
		formField{key: "note", label: "release note", kind: formText, optional: true, placeholder: "optional"},
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
		Version:     strings.TrimSpace(values["version"]),
		Note:        strings.TrimSpace(values["note"]),
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
	}
	if detail.DisplayVersion != "" {
		lines = append(lines, "version: "+detail.DisplayVersion+" (active)")
	}
	lines = append(lines,
		"description: "+detail.Entry.Description,
		fmt.Sprintf("archive: %d bytes", detail.ArchiveSize),
	)
	if len(detail.Entry.Versions) > 0 {
		lines = append(lines, fmt.Sprintf("versions (%d):", len(detail.Entry.Versions)))
		for i := len(detail.Entry.Versions) - 1; i >= 0; i-- {
			v := detail.Entry.Versions[i]
			mark := " "
			if v.Version == detail.Entry.CurrentVersion {
				mark = "*"
			}
			line := fmt.Sprintf("  %s %s (%d B, %s)", mark, v.Version, v.Size, v.CreatedAt.Format("2006-01-02 15:04"))
			if v.Note != "" {
				line += " — " + v.Note
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, "files:")
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

func (t *skillTab) enterVersions() (Tab, tea.Cmd) {
	id := t.currentID()
	if id == "" {
		return t, warnToast("no skill selected")
	}
	versions, active, err := t.mgr.Skill.ListVersions(id)
	if err != nil {
		return t, warnToast(err.Error())
	}
	if len(versions) == 0 {
		return t, warnToast("skill has no version snapshots")
	}
	// 最新版本置顶，方便浏览与切换。
	reversed := make([]storage.SkillVersion, 0, len(versions))
	for i := len(versions) - 1; i >= 0; i-- {
		reversed = append(reversed, versions[i])
	}
	t.versionsID = id
	t.versionsActive = active
	t.versionsList = reversed
	t.versionsIndex = 0
	for i, v := range reversed {
		if v.Version == active {
			t.versionsIndex = i
			break
		}
	}
	t.mode = skillModeVersions
	return t, nil
}

func (t *skillTab) handleVersionsKey(key string) (Tab, tea.Cmd) {
	switch key {
	case "esc", "q":
		t.cancelMode()
		return t, nil
	case "up", "k":
		if len(t.versionsList) > 0 {
			t.versionsIndex = clamp(t.versionsIndex-1, 0, len(t.versionsList)-1)
		}
		return t, nil
	case "down", "j":
		if len(t.versionsList) > 0 {
			t.versionsIndex = clamp(t.versionsIndex+1, 0, len(t.versionsList)-1)
		}
		return t, nil
	case "enter", "u":
		if t.versionsIndex < 0 || t.versionsIndex >= len(t.versionsList) {
			return t, nil
		}
		id := t.versionsID
		ver := t.versionsList[t.versionsIndex].Version
		mgrs := t.mgr
		t.cancelMode()
		return t, func() tea.Msg {
			entry, err := mgrs.Skill.UseVersion(id, ver)
			if err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id+"@"+ver, false, "version use failed")
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id+"@"+ver, true, "version use")
			return skillReloadMsg{toast: fmt.Sprintf("switched %s to %s", id, ver), id: id}
		}
	case "x":
		if t.versionsIndex < 0 || t.versionsIndex >= len(t.versionsList) {
			return t, nil
		}
		ver := t.versionsList[t.versionsIndex].Version
		t.cancelMode()
		return t.enterPlanForVersion("apply", false, ver)
	case "d":
		if t.versionsIndex < 0 || t.versionsIndex >= len(t.versionsList) {
			return t, nil
		}
		id := t.versionsID
		ver := t.versionsList[t.versionsIndex].Version
		mgrs := t.mgr
		return t, func() tea.Msg {
			entry, err := mgrs.Skill.RemoveVersion(id, ver)
			if err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id+"@"+ver, false, "version remove failed")
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			recordAudit(mgrs, session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id+"@"+ver, true, "version remove")
			return skillReloadMsg{toast: fmt.Sprintf("removed %s@%s", id, ver), id: id}
		}
	}
	return t, nil
}

func (t *skillTab) enterPlan(kind string, all bool) (Tab, tea.Cmd) {
	return t.enterPlanForVersion(kind, all, "")
}

func (t *skillTab) enterPlanForVersion(kind string, all bool, version string) (Tab, tea.Cmd) {
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
		entry, archive, resolvedVer, err := t.mgr.Skill.ArchiveVersion(id, version)
		if err != nil {
			return t, warnToast(err.Error())
		}
		items, err = applier.PlanWithOptions(id, archive, skill.ApplyOptions{
			ExtraInfo: entry.ExtraInfo,
			Version:   resolvedVer,
		})
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
	t.planVersion = version
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
	planVer := t.planVersion
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
			entry, archive, targetVer, err := mgrs.Skill.ArchiveVersion(id, planVer)
			if err != nil {
				recordAudit(mgrs, session.AuditOpSkill, "skill:"+id, false, "apply failed")
				return skillReloadMsg{toast: err.Error(), warn: true, id: id}
			}
			results, err := applier.ApplyWithOptions(id, archive, skill.ApplyOptions{
				ExtraInfo: entry.ExtraInfo,
				Version:   targetVer,
			})
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
	case t.mode == skillModeVersions:
		overlay = t.renderVersions()
	}
	body := t.renderPanes()
	if overlay != "" {
		return overlayBox(body, overlay, t.width, t.height)
	}
	return body
}

func (t *skillTab) renderVersions() string {
	var b strings.Builder
	for i, v := range t.versionsList {
		mark := " "
		if v.Version == t.versionsActive {
			mark = "*"
		}
		line := fmt.Sprintf("%s %s  %d B  %s", mark, v.Version, v.Size, v.CreatedAt.Format("2006-01-02 15:04"))
		if v.Note != "" {
			line += " — " + v.Note
		}
		b.WriteString(cursorLine(truncateWidth(line, maxInt(t.width-12, 24)), i == t.versionsIndex))
		b.WriteByte('\n')
	}
	return modalBox(t.width, t.height, "versions · "+t.versionsID,
		strings.TrimRight(b.String(), "\n"),
		"enter/u switch active · x apply · d delete · esc close")
}

func (t *skillTab) renderPlan() string {
	var b strings.Builder
	skillLabel := t.pendingID
	if t.planVersion != "" {
		skillLabel += "@" + t.planVersion
	}
	fmt.Fprintf(&b, "skill: %s\nscope: %s\n", skillLabel, t.scopeOrDefault())
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
	height := maxInt(t.height, 3)
	// applyPane 每个面板在内容宽之外再加 2 列边框，栏间各留 1 列。
	inner := t.width - 8
	if inner < 40 {
		inner = 40
	}
	groupW, targetW := 20, 28
	if groupW+targetW+16 > inner {
		groupW, targetW = 16, 18
	}
	skillW := inner - groupW - targetW
	group := applyPane(t.renderGroups(groupW, height), groupW, height, t.focus == skillPaneGroup)
	mid := applyPane(t.renderSkills(skillW, height), skillW, height, t.focus == skillPaneList)
	right := applyPane(t.renderTargets(targetW, height), targetW, height, t.focus == skillPaneTarget)
	gap := lipgloss.NewStyle().Width(1).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Top, group, gap, mid, gap, right)
}

func (t *skillTab) renderGroups(width, height int) string {
	if !t.loaded {
		return emptyStateStyle.Render("loading groups…")
	}
	rows := make([]SidebarRow, 0, len(t.groups))
	focused := t.focus == skillPaneGroup
	for i, g := range t.groups {
		marker := " "
		name := g.name
		if g.isAll {
			marker = "◯"
			name = "All"
		}
		rows = append(rows, SidebarRow{
			Marker:   marker,
			Name:     name,
			Count:    g.count,
			Selected: focused && i == t.groupIndex,
		})
	}
	return renderSidebar(rows, t.groupIndex, height, width)
}

func (t *skillTab) renderSkills(width, height int) string {
	title := t.skillPaneTitle()
	if t.filterBox.Active() {
		title += "  " + t.filterBox.Prompt()
	}
	return windowedPane(title, t.skillLines(maxInt(width-4, 8)), t.skillIndex, height, width)
}

func (t *skillTab) skillPaneTitle() string {
	n := len(t.visibleEntries())
	row, ok := t.currentGroupRow()
	if !ok || row.isAll {
		return fmt.Sprintf("All (%d)", n)
	}
	title := fmt.Sprintf("%s (%d)", row.name, n)
	if row.description != "" {
		title += " — " + row.description
	}
	return title
}

func (t *skillTab) renderTargets(width, height int) string {
	return windowedPane(fmt.Sprintf("targets · apply status (scope: %s)", t.scopeOrDefault()),
		t.targetLines(maxInt(width-4, 8)), t.targetIndex, height, width)
}

func (t *skillTab) skillLines(width int) []string {
	entries := t.visibleEntries()
	all := true
	if row, ok := t.currentGroupRow(); ok {
		all = row.isAll
	}
	lines := make([]string, 0, len(entries))
	for i, e := range entries {
		desc := e.Description
		if desc == "" {
			desc = "-"
		}
		name := e.ID
		if all {
			name = e.NormalizedGroup() + "/" + e.ID
		}
		if e.CurrentVersion != "" {
			name += "@" + e.CurrentVersion
		}
		line := fmt.Sprintf("%s · %s", name, desc)
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
		line := fmt.Sprintf("%s · %s%s", row.ID, row.State, row.VersionSuffix)
		lines = append(lines, cursorLine(truncateWidth(line, width), i == t.targetIndex))
	}
	return lines
}
