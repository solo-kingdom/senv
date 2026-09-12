package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
)

// newSelEnvTab：真实 env.Manager + 已装载列表（批量删除走真删）。
func newSelEnvTab(t *testing.T) *envTab {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	envMgr := env.NewManager(sm, "pw")
	for _, kv := range [][2]string{{"A_KEY", "1"}, {"B_KEY", "2"}, {"C_KEY", "3"}} {
		if err := envMgr.Set("default", kv[0], kv[1]); err != nil {
			t.Fatalf("seed %s: %v", kv[0], err)
		}
	}
	tab := newEnvTab(Managers{Env: envMgr})
	tab.SetSize(80, 20)
	tab = flush(tab, tab.load())
	tab.focusLeft = false // 条目栏（多选集只作用于条目栏）
	return tab
}

// mcpUpdate applies a message and returns the typed tab.
func mcpUpdate(t *testing.T, tab *mcpTab, msg interface{}) *mcpTab {
	t.Helper()
	next, _ := tab.Update(msg)
	return next.(*mcpTab)
}

// flushMCP runs the command and feeds the resulting message back once.
func flushMCP(t *testing.T, tab *mcpTab, cmd tea.Cmd) *mcpTab {
	t.Helper()
	if cmd == nil {
		return tab
	}
	msg := drainCmd(t, cmd)
	next, _ := tab.Update(msg)
	return next.(*mcpTab)
}

func TestEnvMultiSelectBatchDelete(t *testing.T) {
	tab := newSelEnvTab(t)

	// space 勾选两条：游标 0 勾选、下移、勾选
	tab = envUpdate(tab, runeKey(" "))
	tab = envUpdate(tab, runeKey("j"))
	tab = envUpdate(tab, runeKey(" "))
	if got := tab.sel.SelectionCount(); got != 2 {
		t.Fatalf("selection = %d, want 2", got)
	}

	// d 进入批量确认（一次确认列全部目标）
	tab = envUpdate(tab, runeKey("d"))
	if tab.mode != envModeBatchDeleteConfirm {
		t.Fatalf("mode = %v, want batchDeleteConfirm", tab.mode)
	}
	view := tab.View()
	for _, key := range []string{"A_KEY", "B_KEY"} {
		if !strings.Contains(view, key) {
			t.Fatalf("confirm page missing %s:\n%s", key, view)
		}
	}

	// enter 提交：选择集清空、条目删除
	_, cmd := tab.Update(runeKey("enter"))
	tab = flush(tab, cmd)
	tab = flush(tab, tab.load()) // 处理重载
	if tab.sel.SelectionCount() != 0 {
		t.Fatalf("selection not cleared after submit: %d", tab.sel.SelectionCount())
	}
	if len(tab.itemsByGroup["default"]) != 1 {
		t.Fatalf("remaining items = %d, want 1", len(tab.itemsByGroup["default"]))
	}
	if !hasEnvItem(tab, "default", "C_KEY", "3") {
		t.Fatal("C_KEY should survive")
	}
}

func TestEnvMultiSelectFilteredAllAndPersist(t *testing.T) {
	tab := newSelEnvTab(t)

	// 过滤 B 后 a：只选可见集
	tab = envUpdate(tab, runeKey("/"))
	for _, k := range runeKeys("B") {
		tab = envUpdate(tab, k)
	}
	tab = envUpdate(tab, runeKey("enter")) // 确认过滤（保留词，退出输入态）
	tab = envUpdate(tab, runeKey("a"))
	if tab.sel.SelectionCount() != 1 {
		t.Fatalf("filtered a selection = %d, want 1", tab.sel.SelectionCount())
	}
	if tab.filterBox.Term() != "B" {
		t.Fatalf("confirm should keep term, got %q", tab.filterBox.Term())
	}

	// 重新 `/` + esc 清过滤：选择跨过滤持久
	tab = envUpdate(tab, runeKey("/"))
	tab = envUpdate(tab, runeKey("esc"))
	if tab.filterBox.Term() != "" {
		t.Fatalf("filter should be cleared, got %q", tab.filterBox.Term())
	}
	if tab.sel.SelectionCount() != 1 {
		t.Fatalf("selection should persist across filter, got %d", tab.sel.SelectionCount())
	}
	if !strings.Contains(tab.View(), "已选 1") {
		t.Fatal("selection hint missing from view")
	}
}

func TestEnvMultiSelectSingleEntityOpsBlocked(t *testing.T) {
	tab := newSelEnvTab(t)
	tab = envUpdate(tab, runeKey("space"))
	tab = envUpdate(tab, runeKey("j"))
	tab = envUpdate(tab, runeKey("space"))
	next := envUpdate(tab, runeKey("e"))
	if next.mode != envModeNormal {
		t.Fatalf("multi-selection edit must be blocked, mode = %v", next.mode)
	}
	if tab.sel.SelectionCount() != 2 {
		t.Fatalf("selection = %d, want 2", tab.sel.SelectionCount())
	}
}

func TestMCPMultiSelectBatchExportPlan(t *testing.T) {
	tab, _, _ := newMCPTestTab(t)
	addMCPProfile(t, tab, "github", "npx", nil)
	addMCPProfile(t, tab, "search", "uvx", nil)
	tab = flushMCP(t, tab, tab.load())
	tab.focusLeft = true

	// 勾选两个档案
	tab = mcpUpdate(t, tab, runeKey(" "))
	tab = mcpUpdate(t, tab, runeKey("j"))
	tab = mcpUpdate(t, tab, runeKey(" "))
	if tab.sel.SelectionCount() != 2 {
		t.Fatalf("selection = %d, want 2", tab.sel.SelectionCount())
	}

	// X：计划范围 = 选择集 × 全部 agent
	next, _ := tab.Update(runeKey("X"))
	nextTab := next.(*mcpTab)
	if nextTab.mode != mcpModePlan {
		t.Fatalf("mode = %v, want plan", nextTab.mode)
	}
	if nextTab.exportPlan == nil || len(nextTab.exportPlan.Items) == 0 {
		t.Fatal("plan should not be empty")
	}
	seen := map[string]bool{}
	for _, item := range nextTab.exportPlan.Items {
		seen[item.Alias] = true
	}
	if !seen["github"] || !seen["search"] {
		t.Fatalf("plan aliases = %v, want both selected", seen)
	}
}
