package tui

import (
	"strings"
	"unicode"
)

// 内置图标集 id。Nerd Font v3 为默认（现代开发终端主流配置）；若终端未装
// Nerd Font，可在 Settings Tab 或 SENV_TUI_ICONS 环境变量中一键切回
// unicode 或 ascii。
const (
	IconSetNerd    = "nerd"
	IconSetUnicode = "unicode"
	IconSetASCII   = "ascii"
)

// IconSetInfo 是设置页列出的一项图标集。
type IconSetInfo struct {
	ID, Name string
}

var builtinIconSets = []IconSetInfo{
	{ID: IconSetNerd, Name: "Nerd Font"},
	{ID: IconSetUnicode, Name: "Unicode"},
	{ID: IconSetASCII, Name: "ASCII"},
}

// currentIconSetID 是当前已应用到包级渲染的图标集 id；空表示尚未 apply。
var currentIconSetID string

// allowedIconRunes 缓存当前图标集允许通过 sanitizeLine 的私用区（PUA）rune。
// 仅 UI Chrome 图标注册在此白名单内；用户记录数据经 plainText 仍严格剔除
// 全部 PUA，守住防残影与列宽不变量。
var allowedIconRunes map[rune]bool

// NormalizeIconSet 把用户/配置里的图标集名收敛到内置 id；无法识别时回落到
// 默认的 Nerd Font。
func NormalizeIconSet(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	switch s {
	case IconSetUnicode, "bmp", "default-unicode":
		return IconSetUnicode
	case IconSetASCII, "plain", "none":
		return IconSetASCII
	case IconSetNerd, "nerd-font", "nerdfont", "nf", "":
		return IconSetNerd
	default:
		return IconSetNerd
	}
}

// IconSetName 返回图标集的展示名。
func IconSetName(id string) string {
	id = NormalizeIconSet(id)
	for _, s := range builtinIconSets {
		if s.ID == id {
			return s.Name
		}
	}
	return builtinIconSets[0].Name
}

func allIconSets() []IconSetInfo { return builtinIconSets }

type iconPack struct {
	tabs      map[string]string
	fallback  string
	cursor    string
	sync      string
	search    string
	more      string
	overflowL string
	overflowR string
}

var iconPacks = map[string]iconPack{
	IconSetNerd: {
		tabs: map[string]string{
			"Env":       "\U000f030b", // 󰌋 nf-md-key_variant
			"Text":      "\U000f0219", // 󰈙 nf-md-file_document_outline
			"Providers": "\U000f06a9", // 󰚩 nf-md-robot_outline
			"Skill":     "\uf0e7",     //  nf-fa-bolt
			"Config":    "\U000f107b", // 󱁻 nf-md-file_cog_outline
			"SSH":       "\uf489",     //  nf-oct-terminal
			"KeyPair":   "\uf023",     //  nf-fa-lock
			"MCP":       "\U000f061a", // 󰘚 nf-md-chip
			"Ledger":    "\uf02d",     //  nf-fa-book
			"Tree":      "\U000f0645", // 󰙅 nf-md-file_tree
			"History":   "\U000f02da", // 󰋚 nf-md-history
			"Audit":     "\U000f0565", // 󰕥 nf-md-shield_check_outline
			"Backup":    "\U000f01bc", // 󰆼 nf-md-database
			"Settings":  "\U000f0493", // 󰒓 nf-md-cog_outline
		},
		fallback:  "\uf111",     //  nf-fa-circle
		cursor:    "\uf054",     //  nf-fa-chevron_right
		sync:      "\U000f04e6", // 󰓦 nf-md-sync
		search:    "\uf002",     //  nf-fa-search
		more:      "\U000f01d8", // 󰇘 nf-md-dots_horizontal
		overflowL: "‹",
		overflowR: "›",
	},
	IconSetUnicode: {
		tabs: map[string]string{
			"Env":       "◆",
			"Text":      "¶",
			"Providers": "◈",
			"Skill":     "✦",
			"Config":    "✎",
			"SSH":       "⌘",
			"KeyPair":   "⋆",
			"MCP":       "⬡",
			"Ledger":    "▤",
			"Tree":      "⊞",
			"History":   "↺",
			"Audit":     "⚑",
			"Backup":    "▣",
			"Settings":  "⚙",
		},
		fallback:  "•",
		cursor:    "▸",
		sync:      "↻",
		search:    "⌕",
		more:      "⋯",
		overflowL: "‹",
		overflowR: "›",
	},
	IconSetASCII: {
		tabs: map[string]string{
			"Env":       "$",
			"Text":      "#",
			"Providers": "@",
			"Skill":     "!",
			"Config":    "~",
			"SSH":       ">",
			"KeyPair":   "+",
			"MCP":       "&",
			"Ledger":    "=",
			"Tree":      "/",
			"History":   "<",
			"Audit":     "?",
			"Backup":    "%",
			"Settings":  "*",
		},
		fallback:  "-",
		cursor:    ">",
		sync:      "~",
		search:    "/",
		more:      "+",
		overflowL: "<",
		overflowR: ">",
	},
}

func activeIconPack() iconPack {
	id := currentIconSetID
	if id == "" {
		id = IconSetNerd
	}
	if p, ok := iconPacks[id]; ok {
		return p
	}
	return iconPacks[IconSetNerd]
}

// applyIconSet 切换当前图标集并刷新 PUA 白名单，返回收敛后的 id。
func applyIconSet(id string) string {
	id = NormalizeIconSet(id)
	currentIconSetID = id
	pack := iconPacks[id]
	allowed := make(map[rune]bool)
	addRunes := func(s string) {
		for _, r := range s {
			if unicode.Is(unicode.Co, r) {
				allowed[r] = true
			}
		}
	}
	for _, v := range pack.tabs {
		addRunes(v)
	}
	addRunes(pack.fallback)
	addRunes(pack.cursor)
	addRunes(pack.sync)
	addRunes(pack.search)
	addRunes(pack.more)
	allowedIconRunes = allowed
	return id
}

func isAllowedIconRune(r rune) bool {
	return allowedIconRunes[r]
}

// iconPreviewSample 返回一组代表性图标，用于在 Settings 页实时检查终端字体
// 是否能正确渲染当前图标集。
func iconPreviewSample() string {
	titles := []string{"Env", "Text", "Providers", "SSH", "Tree", "Settings"}
	parts := make([]string, 0, len(titles))
	for _, t := range titles {
		parts = append(parts, tabIcon(t))
	}
	return strings.Join(parts, "  ")
}

func iconCursor() string    { return activeIconPack().cursor }
func iconSync() string      { return activeIconPack().sync }
func iconSearch() string    { return activeIconPack().search }
func iconMore() string      { return activeIconPack().more }
func iconOverflowL() string { return activeIconPack().overflowL }
func iconOverflowR() string { return activeIconPack().overflowR }

func init() { applyIconSet(IconSetNerd) }
