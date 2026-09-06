// 访问日志字段截断：超长 path（认证前即可注入）入库前被截断，日志表不因
// 恶意超长 URL 膨胀；多字节字符不在截断边界被劈开。
package store

import (
	"context"
	"strings"
	"testing"
)

func TestTruncateUTF8(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"短字符串原样", "abc", 10, "abc"},
		{"恰好上限", "abcdef", 6, "abcdef"},
		{"ASCII 截断", "abcdef", 3, "abc"},
		// "中"占 3 字节：max=10 时前 10 字节为 3 个完整字符 + 1 个残缺
		// 首字节，回退后保留 3 个完整字符
		{"多字节边界回退", strings.Repeat("中", 10), 10, strings.Repeat("中", 3)},
		{"空串", "", 8, ""},
		{"max 为 0", "abc", 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateUTF8(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

// 端到端：RecordAccess 落库后 path/reason/ip 均不超过上限。
func TestRecordAccessTruncatesFields(t *testing.T) {
	st := newStore(t)

	err := st.RecordAccess(context.Background(), AccessEvent{
		IP:      strings.Repeat("1", 500),
		Method:  "GET",
		Path:    "/v1/vaults/" + strings.Repeat("粗", 600) + "/entries", // 1800+ 字节
		Outcome: AccessOutcomeAuthFailed,
		Reason:  strings.Repeat("r", 300),
	})
	if err != nil {
		t.Fatalf("RecordAccess: %v", err)
	}

	events, err := st.ListAccessLogs(context.Background(), AccessLogFilter{
		Outcome: AccessOutcomeAuthFailed, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListAccessLogs: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if len(e.IP) > MaxAccessLogIPBytes {
		t.Errorf("ip stored with %d bytes, want <= %d", len(e.IP), MaxAccessLogIPBytes)
	}
	if len(e.Path) > MaxAccessLogPathBytes {
		t.Errorf("path stored with %d bytes, want <= %d", len(e.Path), MaxAccessLogPathBytes)
	}
	if len(e.Reason) > MaxAccessLogReasonBytes {
		t.Errorf("reason stored with %d bytes, want <= %d", len(e.Reason), MaxAccessLogReasonBytes)
	}
	if !strings.HasPrefix(e.Path, "/v1/vaults/") || strings.ContainsRune(e.Path, '\ufffd') {
		t.Errorf("path = %q, want 前缀保留且无残缺 rune", e.Path)
	}
}
