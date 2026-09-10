package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// sshTab is a read-only browser for host records and keypair metadata.
// Private-key content is never loaded into the tab model.
type sshTab struct {
	mgr           Managers
	width, height int
	loaded        bool

	hosts     []storage.HostEntry
	keyPairs  []ssh.KeyPairSummary
	focusLeft bool
	hostIndex int
	keyIndex  int
	loadErr   string
}

type sshLoadedMsg struct {
	hosts    []storage.HostEntry
	keyPairs []ssh.KeyPairSummary
	err      error
}

func newSSHTab(mgr Managers) *sshTab {
	return &sshTab{mgr: mgr, focusLeft: true}
}

func (t *sshTab) Title() string { return "SSH" }

func (t *sshTab) Help() string {
	return "↑↓/jk move · ←→/hl panes · r refresh · read-only (private keys masked)"
}

func (t *sshTab) InputMode() bool { return false }

func (t *sshTab) SetSize(width, height int) {
	t.width, t.height = width, height
}

func (t *sshTab) Init() tea.Cmd {
	if t.loaded {
		return nil
	}
	return t.load()
}

func (t *sshTab) load() tea.Cmd {
	mgr := t.mgr.SSH
	return func() tea.Msg {
		if mgr == nil {
			return sshLoadedMsg{}
		}
		hosts, err := mgr.ListHosts()
		if err != nil {
			return sshLoadedMsg{err: err}
		}
		keyPairs, err := mgr.ListKeyPairs()
		if err != nil {
			return sshLoadedMsg{err: err}
		}
		values := make([]storage.HostEntry, 0, len(hosts))
		for _, host := range hosts {
			values = append(values, *host)
		}
		return sshLoadedMsg{hosts: values, keyPairs: keyPairs}
	}
}

func (t *sshTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case sshLoadedMsg:
		t.loaded = true
		t.loadErr = ""
		if msg.err != nil {
			t.loadErr = msg.err.Error()
			return t, nil
		}
		t.hosts = msg.hosts
		t.keyPairs = msg.keyPairs
		t.clamp()
		return t, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if t.focusLeft && t.hostIndex > 0 {
				t.hostIndex--
			} else if !t.focusLeft && t.keyIndex > 0 {
				t.keyIndex--
			}
		case "down", "j":
			if t.focusLeft && t.hostIndex < len(t.hosts)-1 {
				t.hostIndex++
			} else if !t.focusLeft && t.keyIndex < len(t.keyPairs)-1 {
				t.keyIndex++
			}
		case "left", "h":
			t.focusLeft = true
		case "right", "l":
			t.focusLeft = false
		case "r":
			t.loaded = false
			return t, t.load()
		}
	}
	return t, nil
}

func (t *sshTab) clamp() {
	if t.hostIndex >= len(t.hosts) {
		t.hostIndex = len(t.hosts) - 1
	}
	if t.hostIndex < 0 {
		t.hostIndex = 0
	}
	if t.keyIndex >= len(t.keyPairs) {
		t.keyIndex = len(t.keyPairs) - 1
	}
	if t.keyIndex < 0 {
		t.keyIndex = 0
	}
}

func (t *sshTab) View() string {
	if t.loadErr != "" {
		return paneTitleStyle.Render("SSH") + "\n" + truncateRunes("⚠ "+t.loadErr, max(t.width, 1))
	}
	hostLines := make([]string, 0, len(t.hosts))
	for i, host := range t.hosts {
		line := fmt.Sprintf("%s → %s", host.Alias, host.Hostname)
		hostLines = append(hostLines, cursorLine(line, i == t.hostIndex))
	}
	keyLines := make([]string, 0, len(t.keyPairs))
	for i, key := range t.keyPairs {
		label := key.Fingerprint
		if label == "" {
			label = "pubkey: none"
		}
		line := fmt.Sprintf("%s · %s", key.Name, label)
		keyLines = append(keyLines, cursorLine(line, i == t.keyIndex))
	}
	half := t.width / 2
	if half < 20 {
		half = max(t.width/2, 1)
	}
	// Leave room for the 1-column gap and the two panes' outside borders.
	leftW := half
	rightW := t.width - half - 5
	if rightW < 1 {
		rightW = 1
	}
	hosts := windowedPane("Hosts", hostLines, t.hostIndex, t.height, leftW)
	keys := windowedPane("KeyPairs · private keys masked", keyLines, t.keyIndex, t.height, rightW)
	if t.focusLeft {
		hosts = activePaneStyle.Width(half).Height(t.height).Render(hosts)
		keys = paneStyle.Width(rightW).Height(t.height).Render(keys)
	} else {
		hosts = paneStyle.Width(half).Height(t.height).Render(hosts)
		keys = activePaneStyle.Width(rightW).Height(t.height).Render(keys)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, hosts, strings.Repeat(" ", 1), keys)
}

// cursorLine renders a selectable list row with a fixed-width selection marker.
func cursorLine(line string, selected bool) string {
	if selected {
		return selectedLineStyle.Render(cursorPrefix(true) + line)
	}
	return cursorPrefix(false) + line
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
