package cmd

import (
	"strings"
	"testing"

	"github.com/wii/senv/internal/provider"
)

func TestSyncSuccessReportHealed(t *testing.T) {
	var b strings.Builder
	writeSyncSuccessReport(&b, &provider.SyncResult{
		Pull:   &provider.PullResult{},
		Push:   &provider.PushResult{},
		Dirty:  1,
		Healed: 2,
	})
	out := b.String()
	if !strings.Contains(out, "已自动修复 2 条同步状态") {
		t.Errorf("output missing heal report: %q", out)
	}
	if !strings.Contains(out, "同步完成") || strings.Contains(out, "已是最新") {
		t.Errorf("healing sync must complete, not report no-op: %q", out)
	}
}

func TestSyncSuccessReportNoOpUnchanged(t *testing.T) {
	var b strings.Builder
	writeSyncSuccessReport(&b, &provider.SyncResult{Pull: &provider.PullResult{}, Push: &provider.PushResult{}})
	if out := b.String(); !strings.Contains(out, "已是最新，无需同步") || strings.Contains(out, "已自动修复") {
		t.Errorf("no-op output changed: %q", out)
	}
}
