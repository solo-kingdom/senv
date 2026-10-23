package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeLineKeepsPlainAndStyledText(t *testing.T) {
	for _, s := range []string{"", "hello world", "中文 ok", "é", "e\u0301", "🚀 rocket", "\x1b[1;38;2;1;2;3mbold\x1b[0m"} {
		if got := sanitizeLine(s); got != s {
			t.Errorf("sanitizeLine(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestSanitizeLineMakesWidthModelsAgree(t *testing.T) {
	for _, s := range nastyValues {
		got := sanitizeLine(s)
		if gw, ww := ansi.StringWidth(got), ansi.StringWidthWc(got); gw != ww {
			t.Errorf("sanitizeLine(%q) = %q: grapheme width %d != wcwidth %d", s, got, gw, ww)
		}
		rest := sgrRe.ReplaceAllString(got, "")
		for _, r := range rest {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("sanitizeLine(%q) kept control %U", s, r)
			}
		}
		if strings.Contains(rest, "\x1b") {
			t.Errorf("sanitizeLine(%q) kept non-SGR escape: %q", s, got)
		}
	}
}

func TestSanitizeLineDropsCursorMovingSequences(t *testing.T) {
	got := sanitizeLine("a\x1b[2Jb\x1b[10;1Hc\x1b]8;;http://x\x07d\x1b7e\u009b2Jf\u0085g")
	if got != "abcde2Jfg" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateWidthIsGraphemeAndANSIAware(t *testing.T) {
	styled := "\x1b[36m2026-10-23 11:19:02\x1b[0m session_validate OK"
	got := truncateWidth(styled, 25)
	if w := ansi.StringWidth(got); w != 25 {
		t.Fatalf("width = %d, want 25 (escape bytes must not count): %q", w, got)
	}
	// 修复前逐 rune 量宽：VS16 等多码点字素被量窄，截断结果比预算更宽。
	for _, v := range nastyValues {
		for _, n := range []int{1, 2, 5, 17, 40} {
			got := truncateWidth(v, n)
			if w := ansi.StringWidth(got); w > n {
				t.Fatalf("truncateWidth(%q, %d) width %d", v, n, w)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("truncateWidth(%q, %d) kept a newline", v, n)
			}
		}
	}
}

func TestFitBlockExactRectangle(t *testing.T) {
	in := "short\n" + strings.Repeat("x", 50) + "\n\x1b[31m" + strings.Repeat("中", 30) + "\n4\n5\n6"
	out := fitBlock(in, 20, 4)
	if len(out) != 4 {
		t.Fatalf("rows = %d", len(out))
	}
	for i, l := range out {
		if ansi.StringWidth(l) != 20 || ansi.StringWidthWc(l) != 20 {
			t.Fatalf("row %d width %d/%d: %q", i, ansi.StringWidth(l), ansi.StringWidthWc(l), l)
		}
	}
}
