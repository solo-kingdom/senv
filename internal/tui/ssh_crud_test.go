package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	golangssh "golang.org/x/crypto/ssh"

	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

// writeSSHTUIKey writes a real ed25519 private key so ImportKeyPair's validation
// has something to parse.
func writeSSHTUIKey(t *testing.T, name string) string {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := golangssh.MarshalPrivateKey(private, "senv-tui-test")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func newSSHCrudTab(t *testing.T) (*sshTab, *fakeAuditWriter) {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	w := &fakeAuditWriter{}
	tab := newSSHTab(Managers{SSH: ssh.NewManager(sm, "pw"), AuditWriter: w})
	tab.SetSize(100, 24)
	return flushTab(tab, tab.load()).(*sshTab), w
}

// newKeyPairCrudTab 构造已装载的 KeyPair Tab，与 newSSHCrudTab 共用同一
// 类 store 初始化（keypair 动作已迁至 KeyPair Tab）。
func newKeyPairCrudTab(t *testing.T) (*keyPairTab, *fakeAuditWriter) {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("pw"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	w := &fakeAuditWriter{}
	tab := newKeyPairTab(Managers{SSH: ssh.NewManager(sm, "pw"), AuditWriter: w})
	tab.SetSize(100, 24)
	return flushTab(tab, tab.load()).(*keyPairTab), w
}

// submitKPForm fills a keypair form's fields by key and presses enter, returning
// the settled tab (the form stays open when validation fails).
func submitKPForm(t *testing.T, tab *keyPairTab, values map[string]string) *keyPairTab {
	t.Helper()
	if tab.form == nil {
		t.Fatal("expected an open form")
	}
	for key, value := range values {
		tab.form.SetValue(key, value)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return flushTab(out, cmd).(*keyPairTab)
}

// submitSSHForm fills a form's fields by key and presses enter, returning the
// settled tab (the form stays open when validation fails).
func submitSSHForm(t *testing.T, tab *sshTab, values map[string]string) *sshTab {
	t.Helper()
	if tab.form == nil {
		t.Fatal("expected an open form")
	}
	for key, value := range values {
		tab.form.SetValue(key, value)
	}
	out, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return flushTab(out, cmd).(*sshTab)
}

func TestSSHHostCreateViaForm(t *testing.T) {
	tab, w := newSSHCrudTab(t)

	out, _ := tab.Update(runeKey("n"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("n should open the host create form")
	}
	if !tab.InputMode() {
		t.Error("open form must report InputMode")
	}
	tab = submitSSHForm(t, tab, map[string]string{
		"alias":    "web",
		"hostname": "web.example",
		"user":     "deploy",
		"port":     "2222",
		"tags":     "prod, web",
		"extra":    "ForwardAgent=yes\nStrictHostKeyChecking=no\n",
	})
	if tab.form != nil {
		t.Fatalf("form should close after a successful submit: %#v", tab.form.errs)
	}
	host, ok := tab.hostByAlias("web")
	if !ok {
		t.Fatalf("host not created; hosts=%#v", tab.hosts)
	}
	if host.Hostname != "web.example" || host.User != "deploy" || host.Port != 2222 {
		t.Fatalf("unexpected host: %+v", host)
	}
	if len(host.Tags) != 2 || host.Extra["ForwardAgent"] != "yes" {
		t.Fatalf("tags/extra not stored: %+v", host)
	}
	if len(w.calls) != 1 || w.calls[0].target != "host:web" || !w.calls[0].success {
		t.Fatalf("audit calls = %#v", w.calls)
	}
}

func TestSSHHostEditFormKeepsAliasAndRejectsUnknownKey(t *testing.T) {
	tab, _ := newSSHCrudTab(t)
	if err := tab.mgr.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "old.example"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*sshTab)
	tab.focus = paneHost
	tab.hostIndex = 0

	out, _ := tab.Update(runeKey("e"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("e should open the host edit form")
	}
	if tab.form.fieldIndex("alias") != -1 {
		t.Fatal("edit form must not expose the alias field")
	}
	// A reference to a keypair that does not exist must fail inline and write nothing.
	tab = submitSSHForm(t, tab, map[string]string{"identityKey": "ghost"})
	if tab.form == nil {
		t.Fatal("unknown identityKey must keep the form open")
	}
	if !strings.Contains(tab.form.errs[tab.form.fieldIndex("identityKey")], "does not exist") {
		t.Fatalf("inline error missing: %#v", tab.form.errs)
	}
	host, err := tab.mgr.SSH.GetHost("web")
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if host.IdentityKey != "" || host.Hostname != "old.example" {
		t.Fatalf("invalid submit wrote data: %+v", host)
	}

	// A valid edit updates fields but never the alias.
	tab = submitSSHForm(t, tab, map[string]string{"identityKey": "", "hostname": "new.example", "user": "ops"})
	if tab.form != nil {
		t.Fatalf("valid submit kept the form open: %#v", tab.form.errs)
	}
	host, err = tab.mgr.SSH.GetHost("web")
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if host.Hostname != "new.example" || host.User != "ops" || host.Alias != "web" {
		t.Fatalf("edit not applied: %+v", host)
	}
}

func TestSSHHostDeleteConfirm(t *testing.T) {
	tab, _ := newSSHCrudTab(t)
	if err := tab.mgr.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*sshTab)

	out, _ := tab.Update(runeKey("d"))
	tab = out.(*sshTab)
	if tab.mode != sshModeDeleteHost {
		t.Fatalf("d should stage a delete, mode=%v", tab.mode)
	}
	if view := tab.View(); !strings.Contains(view, "delete host web") {
		t.Fatalf("confirm modal missing:\n%s", view)
	}
	// esc cancels and leaves the host in place.
	out, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tab = out.(*sshTab)
	if _, ok := tab.hostByAlias("web"); !ok {
		t.Fatal("esc should not delete the host")
	}
	out, _ = tab.Update(runeKey("d"))
	tab = out.(*sshTab)
	out, cmd := tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*sshTab)
	if _, ok := tab.hostByAlias("web"); ok {
		t.Fatalf("host still present after confirm: %#v", tab.hosts)
	}
}

func TestSSHExportPreviewThenWrite(t *testing.T) {
	tab, _ := newSSHCrudTab(t)
	if err := tab.mgr.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example", User: "deploy"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*sshTab)

	out, cmd := tab.Update(runeKey("x"))
	tab = flushTab(out, cmd).(*sshTab)
	if tab.mode != sshModeExportPreview {
		t.Fatalf("x should open the export preview, mode=%v", tab.mode)
	}
	view := tab.View()
	for _, want := range []string{"export OpenSSH snippet", "Host web", "HostName web.example"} {
		if !strings.Contains(view, want) {
			t.Fatalf("preview missing %q:\n%s", want, view)
		}
	}

	out, _ = tab.Update(runeKey("w"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("w should open the export path form")
	}
	target := filepath.Join(t.TempDir(), "senv.conf")
	tab = submitSSHForm(t, tab, map[string]string{"path": target})
	if tab.form != nil {
		t.Fatalf("export form still open: %#v", tab.form.errs)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if !strings.Contains(string(data), "Host web") {
		t.Fatalf("exported file missing fragment: %q", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("exported file mode = %o, want 600", info.Mode().Perm())
	}
}

// TestSSHExportPreviewScroll：预览超高时窗口化显示并可滚动，不被 clip 裁掉；
// 滚动后仍可按 w 进入写文件表单。
func TestSSHExportPreviewScroll(t *testing.T) {
	tab, _ := newSSHCrudTab(t)
	var b strings.Builder
	b.WriteString("Host head\n")
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("x", 8) + "\n")
	}
	b.WriteString("Host tail\n")
	tab.exportLabel = "host head"
	tab.exportContent = b.String()
	tab.exportScroll = 0
	tab.mode = sshModeExportPreview
	tab.SetSize(80, 10) // pageSize = height-4 = 6

	view := tab.View()
	if !strings.Contains(view, "Host head") {
		t.Fatalf("top of preview should show Host head:\n%s", view)
	}
	if strings.Contains(view, "Host tail") {
		t.Fatalf("short viewport must not show Host tail yet:\n%s", view)
	}
	if !strings.Contains(view, "scroll") {
		t.Fatalf("scrollable preview should mention scroll keys:\n%s", view)
	}
	if !strings.Contains(view, "1–") {
		t.Fatalf("scrollable title should show line range:\n%s", view)
	}

	out, _ := tab.Update(runeKey("G"))
	tab = out.(*sshTab)
	if tab.exportScroll != tab.exportPreviewMaxScroll() {
		t.Fatalf("end should jump to max scroll, got %d want %d", tab.exportScroll, tab.exportPreviewMaxScroll())
	}
	view = tab.View()
	if !strings.Contains(view, "Host tail") {
		t.Fatalf("scrolled preview should show Host tail:\n%s", view)
	}
	if strings.Contains(view, "Host head") {
		t.Fatalf("scrolled preview should hide Host head:\n%s", view)
	}

	out, _ = tab.Update(runeKey("w"))
	tab = out.(*sshTab)
	if tab.form == nil {
		t.Fatal("w should still open the export path form after scrolling")
	}
}

func TestSSHKeyPairImportRenameAndProtectedDelete(t *testing.T) {
	tab, w := newKeyPairCrudTab(t)
	keyPath := writeSSHTUIKey(t, "id_ed25519")

	// Import via the form (KeyPair Tab list pane).
	out, _ := tab.Update(runeKey("i"))
	tab = out.(*keyPairTab)
	if tab.form == nil {
		t.Fatal("i should open the keypair import form")
	}
	tab = submitKPForm(t, tab, map[string]string{"name": "web-key", "path": keyPath})
	if tab.form != nil {
		t.Fatalf("import form still open: %#v", tab.form.errs)
	}
	if len(tab.keyPairs) != 1 || tab.keyPairs[0].Name != "web-key" {
		t.Fatalf("keypair not imported: %#v", tab.keyPairs)
	}

	// Link a host to it, then rename the keypair and check the reference moves.
	if err := tab.mgr.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example", IdentityKey: "web-key"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*keyPairTab)
	assertMgrHostKey(t, tab.mgr.SSH, "web", "web-key")

	out, _ = tab.Update(runeKey("r"))
	tab = out.(*keyPairTab)
	if tab.form == nil {
		t.Fatal("r should open the keypair rename form")
	}
	tab = submitKPForm(t, tab, map[string]string{"name": "prod-key"})
	if tab.form != nil {
		t.Fatalf("rename form still open: %#v", tab.form.errs)
	}
	if len(tab.keyPairs) != 1 || tab.keyPairs[0].Name != "prod-key" {
		t.Fatalf("keypair not renamed: %#v", tab.keyPairs)
	}
	assertMgrHostKey(t, tab.mgr.SSH, "web", "prod-key")

	// Delete while referenced: refuse and list the referencer.
	out, _ = tab.Update(runeKey("d"))
	tab = out.(*keyPairTab)
	if tab.mode != kpModeDeleteKey {
		t.Fatalf("d should stage the keypair delete, mode=%v", tab.mode)
	}
	view := tab.View()
	if !strings.Contains(view, "still referenced") || !strings.Contains(view, "web") {
		t.Fatalf("referencer list missing:\n%s", view)
	}
	out, cmd := tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*keyPairTab)
	if len(tab.keyPairs) != 1 {
		t.Fatal("plain confirm must not delete a referenced keypair")
	}
	assertMgrHostKey(t, tab.mgr.SSH, "web", "prod-key")

	// Force delete clears the host reference.
	out, _ = tab.Update(runeKey("d"))
	tab = out.(*keyPairTab)
	out, cmd = tab.Update(runeKey("F"))
	tab = flushTab(out, cmd).(*keyPairTab)
	if len(tab.keyPairs) != 0 {
		t.Fatalf("force delete left keypairs: %#v", tab.keyPairs)
	}
	assertMgrHostKey(t, tab.mgr.SSH, "web", "")

	var sawForce bool
	for _, c := range w.calls {
		if c.target == "keypair:prod-key" && c.detail == "delete" && c.success {
			sawForce = true
		}
	}
	if !sawForce {
		t.Fatalf("force delete not audited: %#v", w.calls)
	}
}

func TestSSHMaterializeConfirmAndPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tab, _ := newKeyPairCrudTab(t)
	keyPath := writeSSHTUIKey(t, "id_ed25519")
	if _, err := tab.mgr.SSH.ImportKeyPair("web-key", keyPath, false); err != nil {
		t.Fatalf("import keypair: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*keyPairTab)

	out, _ := tab.Update(runeKey("m"))
	tab = out.(*keyPairTab)
	if tab.mode != kpModeMaterialize {
		t.Fatalf("m should stage materialize, mode=%v", tab.mode)
	}
	view := tab.View()
	for _, want := range []string{filepath.Join(home, ".ssh", "senv", "keys", "_ungrouped", "web-key"), "0600"} {
		if !strings.Contains(view, want) {
			t.Fatalf("materialize confirm missing %q:\n%s", want, view)
		}
	}
	out, cmd := tab.Update(runeKey("y"))
	tab = flushTab(out, cmd).(*keyPairTab)
	target := filepath.Join(home, ".ssh", "senv", "keys", "_ungrouped", "web-key")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("materialized file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized mode = %o, want 600", info.Mode().Perm())
	}
	// The private key body must never be rendered by the tab.
	if view := tab.View(); strings.Contains(view, "PRIVATE KEY") {
		t.Fatalf("view leaked private key material:\n%s", view)
	}
}

func TestSSHHostFormPrefillsFromSelection(t *testing.T) {
	tab, _ := newSSHCrudTab(t)
	if _, err := tab.mgr.SSH.ImportKeyPair("web-key", writeSSHTUIKey(t, "id_ed25519"), false); err != nil {
		t.Fatalf("import keypair: %v", err)
	}
	if err := tab.mgr.SSH.AddHost(&storage.HostEntry{Alias: "web", Hostname: "web.example", IdentityKey: "web-key"}); err != nil {
		t.Fatalf("add host: %v", err)
	}
	tab = flushTab(tab, tab.load()).(*sshTab)
	if view := tab.View(); !strings.Contains(view, "key:web-key") {
		t.Fatalf("host list missing keypair summary:\n%s", view)
	}
	out, _ := tab.Update(runeKey("e"))
	tab = out.(*sshTab)
	if got := tab.form.Values()["identityKey"]; got != "web-key" {
		t.Fatalf("form identityKey = %q, want web-key", got)
	}
	if got := tab.form.Values()["hostname"]; got != "web.example" {
		t.Fatalf("form hostname = %q", got)
	}
}

// assertMgrHostKey asserts a host's identityKey straight from the manager
// (keypair flows live on keyPairTab, which does not index hosts by alias).
func assertMgrHostKey(t *testing.T, mgr *ssh.Manager, alias, want string) {
	t.Helper()
	host, err := mgr.GetHost(alias)
	if err != nil {
		t.Fatalf("get host %s: %v", alias, err)
	}
	if host.IdentityKey != want {
		t.Fatalf("host %s identityKey = %q, want %q", alias, host.IdentityKey, want)
	}
}
