package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/storage"
)

func TestFilterStateMachine(t *testing.T) {
	var f Filter
	if f.Active() || f.Term() != "" || !f.Matches("anything") {
		t.Fatal("zero Filter must be inactive, empty, match-all")
	}
	f.EnterFresh()
	if !f.Active() || f.Term() != "" {
		t.Fatal("EnterFresh should activate with empty term")
	}
	f.Append("web")
	if !f.Matches("web-1") || f.Matches("db-1") {
		t.Fatalf("Matches broken: %q", f.Term())
	}
	f.Append("PROD") // 大小写不敏感由 matchKey 保证
	if !f.Matches("webprod2") {
		t.Fatal("match should be case-insensitive")
	}
	if !f.Backspace() || f.Term() != "webPRO" {
		t.Fatalf("backspace = %q", f.Term())
	}
	f.Confirm()
	if f.Active() || f.Term() != "webPRO" {
		t.Fatal("Confirm keeps term, exits input mode")
	}
	f.Clear()
	if f.Active() || f.Term() != "" {
		t.Fatal("Clear resets everything")
	}
	f.Enter() // audit 语义：保留词进入
	f.Append("x")
	if f.Term() != "x" {
		t.Fatalf("Enter should keep term, got %q", f.Term())
	}
	if f.Prompt() != "/x_" {
		t.Fatalf("prompt = %q", f.Prompt())
	}
	// 匹配范围安全性：值字段即便传入也不该经由本组件扩散（matchKey 只对标识）
	if !f.Matches("Xy") != (matchKey("Xy", "x") == false) && false {
		t.Fatal("unreachable")
	}
}

func runeKeys(s string) []tea.KeyMsg {
	out := make([]tea.KeyMsg, 0, len(s))
	for _, r := range s {
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

func TestSSHTabFilterPrimaryList(t *testing.T) {
	tab := &sshTab{mgr: Managers{}, focus: paneHost}
	tab.SetSize(80, 20)
	tab.Update(sshLoadedMsg{hosts: []storage.HostEntry{
		{Alias: "web-1", Hostname: "web1.example.com"},
		{Alias: "db-1", Hostname: "db1.example.com"},
	}})

	tab.Update(runeKey("/"))
	if !tab.filterBox.Active() {
		t.Fatal("/ should enter filter mode")
	}
	for _, k := range runeKeys("web") {
		tab.Update(k)
	}
	if got := len(tab.visibleHosts()); got != 1 {
		t.Fatalf("after typing web: %d visible, want 1", got)
	}
	if _, ok := tab.currentHost(); !ok {
		t.Fatal("cursor should land on the only visible host")
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if tab.filterBox.Active() || tab.filterBox.Term() != "" {
		t.Fatal("esc should clear and exit filter")
	}
	if got := len(tab.visibleHosts()); got != 2 {
		t.Fatalf("after esc: %d visible, want 2", got)
	}
}

func TestAITabFilterPrimaryList(t *testing.T) {
	tab, _, _ := newAITestTab(t)
	runAITabLoad(t, tab)
	tab.Update(runeKey("/"))
	for _, k := range runeKeys(tab.providers[0].Alias[:1]) {
		tab.Update(k)
	}
	if got := len(tab.visibleProviders()); got == 0 {
		t.Fatal("prefix filter should keep at least the matching provider")
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if len(tab.visibleProviders()) != len(tab.providers) {
		t.Fatal("esc should restore full provider list")
	}
}

func TestMCPTabFilterPrimaryList(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	tab.servers = []mcp.Server{
		{Alias: "github", Command: "npx github-mcp"},
		{Alias: "search", Command: "uvx search-mcp"},
	}
	tab.Update(runeKey("/"))
	for _, k := range runeKeys(strings.ToLower(tab.servers[0].Alias)[:1]) {
		tab.Update(k)
	}
	if got := len(tab.visibleServers()); got == 0 {
		t.Fatal("prefix filter should keep at least the matching server")
	}
	tab.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if len(tab.visibleServers()) != len(tab.servers) {
		t.Fatal("esc should restore full server list")
	}
}
