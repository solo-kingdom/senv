// Package conflict implements the interactive server-sync conflict resolver.
// It is intentionally write-free: provider mutation happens only after the
// caller receives a fully confirmed plan.
package conflict

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/provider"
)

type Mode string

const (
	ModeList    Mode = "list"
	ModeDetail  Mode = "detail"
	ModeHelp    Mode = "help"
	ModeConfirm Mode = "confirm"
	ModeResult  Mode = "result"
)

type Decision string

const (
	DecisionUnresolved Decision = "unresolved"
	DecisionLocal      Decision = "local"
	DecisionRemote     Decision = "remote"
	DecisionMerged     Decision = "merged"
)

type Item struct {
	Detail     provider.ConflictDetail
	Decision   Decision
	MergedData []byte
}

type Plan struct {
	Items           []Item
	Metadata        Decision
	MetadataPresent bool
}

func (p Plan) Complete() bool {
	if p.MetadataPresent && p.Metadata == DecisionUnresolved {
		return false
	}
	for _, item := range p.Items {
		if item.Decision == DecisionUnresolved {
			return false
		}
	}
	return true
}

type Model struct {
	items    []Item
	metadata Decision
	hasMeta  bool
	auth     renderconflict.Auth

	index           int
	mode            Mode
	reveal          bool
	help            bool
	editorRequested bool
	editorIndex     int
	editorSession   *renderconflict.MergeSession
	confirmed       bool
	quit            bool
	message         string
}

func New(err *provider.SyncConflictError, auth renderconflict.Auth) Model {
	items := make([]Item, len(err.Details))
	for i, detail := range err.Details {
		items[i] = Item{Detail: detail, Decision: DecisionUnresolved}
	}
	metadata := DecisionUnresolved
	if !err.MetadataConflict {
		metadata = DecisionRemote // No choice is required when metadata is unchanged.
	}
	return Model{
		items: items, metadata: metadata, hasMeta: err.MetadataConflict,
		auth: auth, mode: ModeList,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Plan() Plan {
	return Plan{Items: m.items, Metadata: m.metadata, MetadataPresent: m.hasMeta}
}

func (m Model) Confirmed() bool { return m.confirmed }
func (m Model) Quit() bool      { return m.quit }
func (m Model) EditorRequested() (int, bool) {
	return m.editorIndex, m.editorRequested && m.editorIndex >= 0 && m.editorIndex < len(m.items)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateKey(msg.String())
	case revealAuthMsg:
		m.auth = msg.auth
		return m, nil
	case editorFinishedMsg:
		return m.finishEditor(msg.err)
	default:
		return m, nil
	}
}

type revealAuthMsg struct{ auth renderconflict.Auth }

type editorFinishedMsg struct{ err error }

func (m Model) updateKey(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		m.quit = true
		return m, tea.Quit
	}
	switch m.mode {
	case ModeList:
		return m.updateListKey(key)
	case ModeDetail:
		return m.updateDetailKey(key)
	case ModeHelp:
		m.mode = ModeList
		return m, nil
	case ModeConfirm:
		switch key {
		case "y", "Y":
			m.confirmed = true
			m.mode = ModeResult
			m.message = "confirmed"
			return m, nil
		case "esc", "n", "N":
			m.mode = ModeList
			return m, nil
		}
	case ModeResult:
		if key == "enter" || key == "q" {
			m.quit = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) updateListKey(key string) (tea.Model, tea.Cmd) {
	m.message = ""
	switch key {
	case "q":
		m.quit = true
		return m, tea.Quit
	case "?":
		m.mode = ModeHelp
	case "j", "down":
		if len(m.items) > 0 {
			m.index = (m.index + 1) % len(m.items)
		}
	case "k", "up":
		if len(m.items) > 0 {
			m.index = (m.index - 1 + len(m.items)) % len(m.items)
		}
	case "enter":
		if len(m.items) > 0 {
			m.mode, m.reveal = ModeDetail, false
		}
	case "l":
		if len(m.items) > 0 {
			m.items[m.index].Decision = DecisionLocal
		}
	case "r":
		if len(m.items) > 0 {
			m.items[m.index].Decision = DecisionRemote
		}
	case "L":
		for i := range m.items {
			if m.items[i].Decision == DecisionUnresolved {
				m.items[i].Decision = DecisionLocal
			}
		}
		if m.hasMeta && m.metadata == DecisionUnresolved {
			m.metadata = DecisionLocal
		}
	case "R":
		for i := range m.items {
			if m.items[i].Decision == DecisionUnresolved {
				m.items[i].Decision = DecisionRemote
			}
		}
		if m.hasMeta && m.metadata == DecisionUnresolved {
			m.metadata = DecisionRemote
		}
	case "m":
		if len(m.items) > 0 {
			if err := mergeAllowed(m.items[m.index], m.auth); err != nil {
				m.message = err.Error()
			} else {
				return m.startEditor(m.index)
			}
		}
	case "y", "Y":
		if m.Plan().Complete() {
			m.mode = ModeConfirm
		} else {
			m.message = "plan is incomplete"
		}
	}
	return m, nil
}

func (m Model) updateDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.mode = ModeList
	case "v":
		m.reveal = !m.reveal
	case "l":
		m.items[m.index].Decision = DecisionLocal
	case "r":
		m.items[m.index].Decision = DecisionRemote
	case "m":
		if err := mergeAllowed(m.items[m.index], m.auth); err != nil {
			m.message = err.Error()
		} else {
			return m.startEditor(m.index)
		}
	}
	return m, nil
}

func (m Model) startEditor(index int) (tea.Model, tea.Cmd) {
	session, err := renderconflict.PrepareMergeEditor(m.items[index].Detail, m.auth.Key)
	if err != nil {
		m.message = err.Error()
		return m, nil
	}
	m.editorSession = session
	m.editorRequested, m.editorIndex = true, index
	return m, tea.ExecProcess(session.EditorCommand(), func(runErr error) tea.Msg {
		return editorFinishedMsg{err: runErr}
	})
}

func (m Model) finishEditor(runErr error) (tea.Model, tea.Cmd) {
	session := m.editorSession
	m.editorSession, m.editorRequested = nil, false
	if session == nil {
		return m, nil
	}
	var problems []error
	if runErr != nil {
		problems = append(problems, runErr)
	} else {
		ciphertext, err := session.Finish(m.auth.Key)
		if err != nil {
			problems = append(problems, err)
		} else {
			m.items[m.editorIndex].MergedData = ciphertext
			m.items[m.editorIndex].Decision = DecisionMerged
			m.mode = ModeList
		}
	}
	if err := session.Close(); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		m.message = problems[0].Error()
	}
	return m, nil
}

func mergeAllowed(item Item, auth renderconflict.Auth) error {
	if len(auth.Key) == 0 {
		return fmt.Errorf("manual merge requires a vault key")
	}
	if !auth.RemoteKeyCompatible {
		return fmt.Errorf("remote metadata uses a different vault key; choose an existing whole-side strategy")
	}
	if item.Detail.Local.Deleted || item.Detail.Remote.Deleted {
		return fmt.Errorf("deleted conflicts cannot be editor-merged")
	}
	return nil
}

func (m Model) itemLabel(i int) string {
	d := m.items[i].Detail
	choice := string(m.items[i].Decision)
	if i == m.index {
		return fmt.Sprintf("> %s/%s/%s [%s]", d.Kind, d.Grp, d.Key, choice)
	}
	return fmt.Sprintf("  %s/%s/%s [%s]", d.Kind, d.Grp, d.Key, choice)
}

func (m Model) View() string {
	var b strings.Builder
	switch m.mode {
	case ModeHelp:
		b.WriteString("Conflict resolver help\n")
		b.WriteString("  j/k or arrows - move\n  Enter - details\n  v - reveal/mask\n")
		b.WriteString("  l/r - use local/remote\n  L/R - default all unresolved local/remote\n")
		b.WriteString("  m - editor merge (eligible entries)\n  y - review plan\n  q - quit\n")
	case ModeDetail:
		if len(m.items) == 0 {
			b.WriteString("No conflict details\n")
			break
		}
		detail := m.items[m.index].Detail
		b.WriteString(renderconflict.RenderDetail(detail, m.auth, m.reveal))
		b.WriteString("\n[v] reveal/mask  [m] merge  [l] local  [r] remote  [esc] back\n")
	case ModeConfirm:
		b.WriteString("Apply conflict resolution plan?\n")
		for _, item := range m.items {
			d := item.Detail
			fmt.Fprintf(&b, "  %s/%s/%s -> %s\n", d.Kind, d.Grp, d.Key, item.Decision)
		}
		if m.hasMeta {
			fmt.Fprintf(&b, "  vault metadata -> %s\n", m.metadata)
		}
		b.WriteString("\n[y] apply  [n/esc] cancel\n")
	case ModeResult:
		b.WriteString("Conflict resolution confirmed. The caller will apply it safely.\n")
	default:
		b.WriteString("Sync conflicts\n")
		for i := range m.items {
			b.WriteString(m.itemLabel(i) + "\n")
		}
		if m.hasMeta {
			fmt.Fprintf(&b, "  vault metadata [%s]\n", m.metadata)
		}
		if m.message != "" {
			fmt.Fprintf(&b, "\n%s\n", m.message)
		}
		b.WriteString("\n[j/k] select  [Enter] detail  [l/r] choose  [L/R] default all  [m] merge  [y] plan  [?] help  [q] quit\n")
	}
	return b.String()
}
