package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// nastyValues 是会破坏「一逻辑行 = 一终端行」的记录内容样本：多行、超长、
// 制表符、ESC/CSI 序列、C1 控制字符（8-bit CSI / NEL）、零宽字符、两种
// 宽度模型不一致的字素（国旗、ZWJ 家庭、VS16、肤色修饰）、CJK、私用区。
var nastyValues = []string{
	"line1\nline2\r\nline3\rline4\n\n",
	strings.Repeat("abcdefghij", 60),
	"a\tb\t\tc" + strings.Repeat("\t", 20) + "end",
	"\x1b[2J\x1b[H\x1b]0;title\x07boom\x1b[31mred\x1b[0m\x1bP+q\x1b\\",
	"c1:\u0085next\u009b2Jcsi\u0090dcs",
	strings.Repeat("a\u200bb\u200dc\u2028d\u2029e\ufeff\u202e", 20),
	strings.Repeat("🇨🇳🇺🇸 ", 30),
	strings.Repeat("👨\u200d👩\u200d👧\u200d👦 ", 30),
	strings.Repeat("⚠️ ✔️ ❤️ 👍🏽 1️⃣ ", 20),
	strings.Repeat("中文配置值，全角标点。", 20),
	strings.Repeat("\ue0a0 \uf113 main ", 20),
	strings.Repeat("e\u0301\u0301 ", 60),
}

func nastyJoined() string { return strings.Join(nastyValues, " | ") }

// nastyShort 是控制在描述长度上限（2048 字节）内的怪异文本。
func nastyShort() string {
	parts := make([]string, 0, len(nastyValues))
	for _, v := range nastyValues {
		r := []rune(v)
		if len(r) > 24 {
			r = r[:24]
		}
		parts = append(parts, string(r))
	}
	return strings.Join(parts, " | ")
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// assertFrameInvariants 校验 bubbletea 行差分渲染器依赖的整帧不变量：
// 恰好 h 行；每行在 grapheme 与 wcwidth 两种宽度模型下都恰好 w 列；除
// SGR 外没有任何转义序列或控制字符。
func assertFrameInvariants(t *testing.T, label, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want exactly %d\n%s", label, len(lines), h, dumpChrome(view))
	}
	for i, l := range lines {
		if gw := ansi.StringWidth(l); gw != w {
			t.Fatalf("%s: line %d grapheme width %d, want %d: %q", label, i, gw, w, l)
		}
		if ww := ansi.StringWidthWc(l); ww != w {
			t.Fatalf("%s: line %d wcwidth %d, want %d: %q", label, i, ww, w, l)
		}
		rest := sgrRe.ReplaceAllString(l, "")
		for _, r := range rest {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x2028 || r == 0x2029 {
				t.Fatalf("%s: line %d contains control rune %U: %q", label, i, r, l)
			}
		}
	}
}

// runModelCmd 执行 cmd 并把产生的消息（含 batch）喂回 model。tea.Tick 之类
// 的延时命令在 100ms 内不返回就丢弃（只关心数据装载）。
func runModelCmd(m Model, cmd tea.Cmd) Model {
	queue := []tea.Cmd{}
	if cmd != nil {
		queue = append(queue, cmd)
	}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(100 * time.Millisecond):
			continue
		}
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			continue
		}
		next, nc := m.Update(msg)
		m = next.(Model)
		if nc != nil {
			queue = append(queue, nc)
		}
	}
	return m
}

func pressKey(m Model, k string) Model {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return runModelCmd(next.(Model), cmd)
}

// newNastyManagers 在全部 Tab 的数据源里塞入 nastyValues。
func newNastyManagers(t *testing.T) Managers {
	t.Helper()
	mgrs := newFullManagers(t)
	val := nastyJoined()
	short := nastyShort()
	for _, add := range []func(string, string) error{mgrs.Env.AddGroup, mgrs.Text.AddGroup, mgrs.Backup.AddGroup} {
		if err := add("grp", short); err != nil {
			t.Fatalf("add group: %v", err)
		}
	}
	for i, v := range nastyValues {
		key := fmt.Sprintf("KEY_%d", i)
		desc := short
		if err := mgrs.Env.SetWithDescription("grp", key, v, &desc); err != nil {
			t.Fatalf("env set: %v", err)
		}
		if err := mgrs.Text.SetWithDescription("grp", key, v, &desc); err != nil {
			t.Fatalf("text set: %v", err)
		}
		if err := mgrs.Backup.SetWithDescription("grp", key, v, &desc); err != nil {
			t.Fatalf("backup set: %v", err)
		}
	}
	src := filepath.Join(t.TempDir(), "app.conf")
	if err := os.WriteFile(src, []byte(val), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mgrs.Config.Create("app", src, filepath.Join(t.TempDir(), "out.conf"), "grp", short); err != nil {
		t.Fatalf("config create: %v", err)
	}
	if err := mgrs.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod",
		Description: short, Tags: []string{"🇨🇳", "a\tb"}}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	if err := mgrs.MCP.Add(&storage.MCPServerEntry{Alias: "srv", Transport: storage.MCPTransportStdio,
		Command: "npx", Args: []string{"-y", "x\ny"}, Description: short}); err != nil {
		t.Fatalf("mcp add: %v", err)
	}

	var hist []provider.HistoryVersion
	var audit []session.AuditEntry
	base := time.Date(2026, 10, 23, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		v := nastyValues[i%len(nastyValues)]
		hist = append(hist, provider.HistoryVersion{Kind: "config", Grp: "g\x1b[2J" + v, Key: fmt.Sprintf("k%d", i),
			Revision: int64(i), CreatedAt: base.Add(time.Duration(i) * time.Minute)})
		audit = append(audit, session.AuditEntry{Timestamp: base, EventType: "op_set",
			Success: i%3 != 0, Target: v, Message: v})
	}
	mgrs.History = &fakeHistorySource{rows: hist, value: val}
	mgrs.Audit = &fakeAuditSource{rows: audit}
	return mgrs
}

var robustSizes = []struct{ w, h int }{
	{30, 7}, {40, 12}, {80, 24}, {100, 30}, {157, 48}, {29, 40}, {200, 6},
}

// TestEveryTabFrameInvariantsWithNastyData 是「双 header 残影」的回归测试：
// 每个 Tab、每种尺寸、塞满恶意/怪异记录内容时，整帧仍满足渲染不变量。
// 修复前，History 里一条含国旗/ZWJ emoji 或 C1 控制字符的记录就会产出
// wcwidth 宽于终端或会移动光标的行，真实终端折行滚屏、旧帧残留。
func TestEveryTabFrameInvariantsWithNastyData(t *testing.T) {
	mgrs := newNastyManagers(t)
	for _, sz := range robustSizes {
		m := New(mgrs)
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(Model)
		m = runModelCmd(m, m.Init())
		for i := range m.tabs {
			title := m.tabs[i].Title()
			if i < 9 {
				m = pressKey(m, fmt.Sprint(i+1))
			} else {
				next, cmd := m.activateTab(i)
				m = next.(Model)
				m.active = i
				m = runModelCmd(m, cmd)
			}
			label := fmt.Sprintf("%dx%d %s", sz.w, sz.h, title)
			assertFrameInvariants(t, label, m.View(), sz.w, sz.h)
			// 列表下移、打开详情等交互态也必须守住不变量。
			for _, k := range []string{"down", "j", "enter"} {
				m = pressKey(m, k)
				assertFrameInvariants(t, label+" after "+k, m.View(), sz.w, sz.h)
			}
			m = pressKey(m, "enter")
			assertFrameInvariants(t, label+" after enter²", m.View(), sz.w, sz.h)
			for j := 0; j < 3; j++ {
				m = pressKey(m, "esc")
			}
			if m.tabs[m.active].InputMode() {
				continue
			}
			m = pressKey(m, "?")
			assertFrameInvariants(t, label+" help", m.View(), sz.w, sz.h)
			m = pressKey(m, "esc")
		}
		// 底栏错误/提示也来自外部（错误信息可能含记录内容）。
		next, _ = m.Update(errMsg{err: fmt.Errorf("%s", nastyJoined())})
		m = next.(Model)
		assertFrameInvariants(t, fmt.Sprintf("%dx%d err bar", sz.w, sz.h), m.View(), sz.w, sz.h)
	}
}

// TestHistoryToAuditSwitchKeepsSingleTabStrip 复现截图：History 有一条超长/
// 含怪异字符的记录，切到 Audit 后整帧仍只有一条 tab 栏，且不变量成立。
func TestHistoryToAuditSwitchKeepsSingleTabStrip(t *testing.T) {
	for _, v := range nastyValues {
		hist := []provider.HistoryVersion{{Kind: "env", Grp: "g", Key: "K", Revision: 1, CreatedAt: time.Now()}}
		mgrs := Managers{History: &fakeHistorySource{rows: hist, value: v}, Audit: &fakeAuditSource{rows: sampleAuditRows()}}
		m := New(mgrs)
		next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m = next.(Model)
		var hIdx, aIdx int
		for i, tab := range m.tabs {
			switch tab.Title() {
			case "History":
				hIdx = i
			case "Audit":
				aIdx = i
			}
		}
		m = pressKey(m, fmt.Sprint(hIdx+1))
		assertFrameInvariants(t, "history "+fmt.Sprintf("%q", v), m.View(), 100, 30)
		m = pressKey(m, fmt.Sprint(aIdx+1))
		view := m.View()
		assertFrameInvariants(t, "audit", view, 100, 30)
		strips := 0
		for _, l := range strings.Split(view, "\n") {
			p := ansi.Strip(l)
			if strings.Contains(p, "History") && strings.Contains(p, "Audit") {
				strips++
			}
		}
		if strips != 1 {
			t.Fatalf("tab strip rows = %d, want 1\n%s", strips, dumpChrome(view))
		}
	}
}

// garbageTab 模拟一个写错的 Tab：输出超高、超宽、含换行/控制字符/转义。
type garbageTab struct{ chromeDummyTab }

func (g *garbageTab) View() string {
	var b strings.Builder
	for i := 0; i < g.height*3+10; i++ {
		b.WriteString(nastyValues[i%len(nastyValues)])
		b.WriteString(strings.Repeat("W", g.width*2))
		b.WriteString("\n")
	}
	return b.String()
}

// TestLayoutBoxesMisbehavingTab 证明鲁棒性来自布局层而非各 Tab 自律：
// 即使 Tab 视图完全无视分配的尺寸，整帧仍恰好 W×H、tab 栏与底栏不被挤走。
func TestLayoutBoxesMisbehavingTab(t *testing.T) {
	for _, sz := range robustSizes {
		g := &garbageTab{chromeDummyTab{title: "Bad"}}
		m := Model{tabs: []Tab{&chromeDummyTab{title: "Env"}, g}, active: 1}
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(Model)
		view := m.View()
		assertFrameInvariants(t, fmt.Sprintf("garbage %dx%d", sz.w, sz.h), view, sz.w, sz.h)
		if computeLayout(sz.w, sz.h).tooSmall() {
			continue
		}
		lines := strings.Split(view, "\n")
		if p := ansi.Strip(lines[1]); !strings.Contains(p, "Env") {
			t.Fatalf("%dx%d: tab strip pushed away: %q", sz.w, sz.h, p)
		}
		if p := ansi.Strip(lines[len(lines)-1]); !strings.HasPrefix(p, "╰") {
			t.Fatalf("%dx%d: bottom border missing: %q", sz.w, sz.h, p)
		}
	}
}
