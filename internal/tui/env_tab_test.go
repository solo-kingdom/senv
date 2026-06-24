package tui

import "testing"

func TestMaskValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "***"},
		{"a", "a***"},
		{"ab", "a***"},
		{"abc", "a***"},
		{"abcd", "abc***"},
		{"sk-supersecret", "sk-***"},
		{"postgres://user:pass@host/db", "pos***"},
	}
	for _, c := range cases {
		if got := maskValue(c.in); got != c.want {
			t.Errorf("maskValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// envTabWith builds an env tab whose data is already loaded (no manager needed),
// so view/logic tests run without touching storage.
func envTabWith(items ...envItemRow) *envTab {
	t := newEnvTab(Managers{})
	t.loaded = true
	t.groups = []envGroupRow{{name: "default", isDefault: true, isActive: true, varCount: len(items)}}
	t.itemsByGroup = map[string][]envItemRow{"default": items}
	t.focusLeft = false
	t.SetSize(80, 20)
	t.clampCursors()
	return t
}

// envUpdate applies a message and returns the typed tab.
func envUpdate(t *envTab, msg interface{}) *envTab {
	next, _ := t.Update(msg)
	return next.(*envTab)
}

func TestMaskingDefaultAllMasked(t *testing.T) {
	tab := envTabWith(envItemRow{key: "API_KEY", value: "sk-supersecret"})
	view := tab.View()
	if contains(view, "sk-supersecret") {
		t.Errorf("default view leaks plaintext: %q", view)
	}
	if !contains(view, "sk-***") {
		t.Errorf("default view does not show masked value: %q", view)
	}
}

func TestMaskingToggleSingleItem(t *testing.T) {
	tab := envTabWith(envItemRow{key: "API_KEY", value: "sk-supersecret"})

	// Press v: only the current item is revealed.
	tab = envUpdate(tab, runeKey("v"))
	if !tab.maskRevealed {
		t.Fatal("expected maskRevealed=true after pressing v")
	}
	if !contains(tab.View(), "sk-supersecret") {
		t.Errorf("revealed view does not show plaintext: %q", tab.View())
	}

	// Press v again: re-masked.
	tab = envUpdate(tab, runeKey("v"))
	if tab.maskRevealed {
		t.Fatal("expected maskRevealed=false after pressing v again")
	}
	if contains(tab.View(), "sk-supersecret") {
		t.Errorf("re-masked view leaks plaintext: %q", tab.View())
	}
}

func TestMaskingRemaskOnCursorMove(t *testing.T) {
	tab := envTabWith(
		envItemRow{key: "A", value: "secret-one"},
		envItemRow{key: "B", value: "secret-two"},
	)
	tab.itemIndex = 0

	// Reveal item A.
	tab = envUpdate(tab, runeKey("v"))
	if !contains(tab.View(), "secret-one") {
		t.Fatalf("expected A revealed: %q", tab.View())
	}

	// Move cursor down to B: A must auto re-mask.
	tab = envUpdate(tab, runeKey("j"))
	if tab.maskRevealed {
		t.Fatal("maskRevealed must reset when the cursor moves")
	}
	if contains(tab.View(), "secret-one") {
		t.Errorf("A leaked after cursor moved away: %q", tab.View())
	}
	// B is masked too until explicitly revealed.
	if contains(tab.View(), "secret-two") {
		t.Errorf("B leaked without explicit reveal: %q", tab.View())
	}
}

func TestMaskingVOnlyAffectsRightPane(t *testing.T) {
	tab := envTabWith(envItemRow{key: "API_KEY", value: "sk-supersecret"})
	tab.focusLeft = true // cursor in the groups pane

	tab = envUpdate(tab, runeKey("v"))
	if tab.maskRevealed {
		t.Fatal("v must not unmask while the groups pane is focused")
	}
	if contains(tab.View(), "sk-supersecret") {
		t.Errorf("plaintext leaked via v from groups pane: %q", tab.View())
	}
}

func TestFilterMatchesKeysOnly(t *testing.T) {
	tab := envTabWith(
		envItemRow{key: "DATABASE_URL", value: "postgres://secret-db/host"},
		envItemRow{key: "API_KEY", value: "sk-1234"},
		envItemRow{key: "DB_HOST", value: "localhost"},
	)

	// "secret-db" appears only in a value -> no keys match (security: never match values).
	tab.filter = "secret-db"
	if got := len(tab.filteredItems()); got != 0 {
		t.Errorf("value-only filter leaked %d items", got)
	}

	// "database" matches the DATABASE_URL key only.
	tab.filter = "database"
	got := tab.filteredItems()
	if len(got) != 1 || got[0].key != "DATABASE_URL" {
		t.Errorf("database filter = %#v, want DATABASE_URL only", got)
	}

	// Case-insensitive.
	tab.filter = "DATABASE"
	if len(tab.filteredItems()) != 1 {
		t.Errorf("uppercase filter should match case-insensitively, got %d", len(tab.filteredItems()))
	}

	// "api" matches API_KEY only (disambiguates from DATABASE_URL).
	tab.filter = "api"
	if got := len(tab.filteredItems()); got != 1 {
		t.Errorf("api filter = %d items, want 1", got)
	}

	// Empty filter restores all items.
	tab.filter = ""
	if got := len(tab.filteredItems()); got != 3 {
		t.Errorf("empty filter = %d items, want 3", got)
	}
}

// TestFilterModeFlow drives the "/" filter input with the keyboard.
func TestFilterModeFlow(t *testing.T) {
	tab := envTabWith(
		envItemRow{key: "DATABASE_URL", value: "x"},
		envItemRow{key: "API_KEY", value: "y"},
	)

	// Enter filter mode.
	tab = envUpdate(tab, runeKey("/"))
	if tab.mode != envModeFilter {
		t.Fatalf("expected envModeFilter, got %v", tab.mode)
	}

	// Type "data".
	for _, r := range "data" {
		tab = envUpdate(tab, runeKey(string(r)))
	}
	if got := len(tab.filteredItems()); got != 1 {
		t.Errorf("after typing 'data': %d items, want 1 (DATABASE_URL)", got)
	}

	// Esc exits filter mode and clears the filter.
	tab = envUpdate(tab, runeKey("esc"))
	if tab.mode != envModeNormal {
		t.Fatalf("expected normal mode after esc, got %v", tab.mode)
	}
	if tab.filter != "" {
		t.Errorf("filter should be cleared on esc, got %q", tab.filter)
	}
	if got := len(tab.filteredItems()); got != 2 {
		t.Errorf("after clearing filter: %d items, want 2", got)
	}
}
