package conflict

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

func keyMsg(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func update(m Model, key string) Model {
	next, _ := m.Update(keyMsg(key))
	return next.(Model)
}

func testConflict(t *testing.T) *provider.SyncConflictError {
	t.Helper()
	key := bytes.Repeat([]byte{7}, 32)
	local := storage.EnvVarEntry{Value: "local-secret", UpdatedAt: time.Now()}
	remote := storage.EnvVarEntry{Value: "remote-secret", UpdatedAt: time.Now()}
	localBlob, _ := storage.ToJSON(local)
	remoteBlob, _ := storage.ToJSON(remote)
	localCipher, _ := crypto.Encrypt(key, localBlob)
	remoteCipher, _ := crypto.Encrypt(key, remoteBlob)
	return &provider.SyncConflictError{
		Conflicts:        []provider.Conflict{{Kind: provider.KindEnv, Grp: "default", Key: "API", CurrentRevision: 2}},
		MetadataConflict: true,
		Details: []provider.ConflictDetail{{
			Kind: provider.KindEnv, Grp: "default", Key: "API",
			Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(localCipher)},
			Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(remoteCipher)},
		}},
	}
}

func TestListModelNavigationAndHelp(t *testing.T) {
	conflict := testConflict(t)
	conflict.Details = append(conflict.Details, provider.ConflictDetail{
		Kind: provider.KindText, Grp: "default", Key: "B",
	})
	m := New(conflict, renderconflict.Auth{})
	if m.Plan().Complete() {
		t.Fatal("new plan must be incomplete")
	}
	m = update(m, "j")
	if m.index != 1 {
		t.Fatalf("index after j = %d, want 1", m.index)
	}
	m = update(m, "k")
	if m.index != 0 {
		t.Fatalf("index after k = %d, want 0", m.index)
	}
	m = update(m, "?")
	if m.mode != ModeHelp || !strings.Contains(m.View(), "move") {
		t.Fatalf("help mode = %v view=%s", m.mode, m.View())
	}
	m = update(m, "x")
	if m.mode != ModeList {
		t.Fatalf("help enter returned to %v", m.mode)
	}
	m = update(m, "q")
	if !m.Quit() {
		t.Fatal("q must request quit")
	}
}

func TestDetailMaskRevealAndSafety(t *testing.T) {
	conflict := testConflict(t)
	key := bytes.Repeat([]byte{7}, 32)
	m := New(conflict, renderconflict.Auth{Key: key, RemoteKeyCompatible: true})
	m = update(m, "enter")
	hidden := m.View()
	if strings.Contains(hidden, "local-secret") || strings.Contains(hidden, "remote-secret") {
		t.Fatalf("detail leaked plaintext before reveal:\n%s", hidden)
	}
	m = update(m, "v")
	if !strings.Contains(m.View(), "local-secret") {
		t.Fatalf("reveal missing plaintext:\n%s", m.View())
	}
	m = update(m, "v")
	if strings.Contains(m.View(), "local-secret") {
		t.Fatalf("second v did not mask:\n%s", m.View())
	}
}

func TestPlanAndConfirmation(t *testing.T) {
	conflict := testConflict(t)
	m := New(conflict, renderconflict.Auth{})
	m = update(m, "y")
	if m.mode == ModeConfirm {
		t.Fatal("incomplete plan must not reach confirmation")
	}
	m = update(m, "l")
	m = update(m, "R")
	plan := m.Plan()
	if !plan.Complete() || plan.Items[0].Decision != DecisionLocal || plan.Metadata != DecisionRemote {
		t.Fatalf("plan = %+v, want local item and remote metadata", plan)
	}
	m = update(m, "y")
	if m.mode != ModeConfirm {
		t.Fatalf("mode = %v, want confirmation", m.mode)
	}
	view := m.View()
	if !strings.Contains(view, "env/default/API -> local") || !strings.Contains(view, "vault metadata -> remote") {
		t.Fatalf("confirmation view missing plan:\n%s", view)
	}
	m = update(m, "n")
	if m.Confirmed() || m.mode == ModeConfirm {
		t.Fatal("cancel must leave plan unconfirmed")
	}
	m = update(m, "y")
	m = update(m, "y")
	if !m.Confirmed() {
		t.Fatal("explicit y must confirm plan")
	}
}

func TestMergeGateBlocksIncompatibleRemoteMetadata(t *testing.T) {
	conflict := testConflict(t)
	key := bytes.Repeat([]byte{7}, 32)
	m := New(conflict, renderconflict.Auth{Key: key, RemoteKeyCompatible: false})
	m = update(m, "m")
	if idx, requested := m.EditorRequested(); requested || idx != 0 {
		t.Fatalf("editor requested=%v index=%d for incompatible metadata", requested, idx)
	}
	view := m.View()
	if !strings.Contains(view, "different vault key") {
		t.Fatalf("merge gate missing reason:\n%s", view)
	}
	for _, secret := range []string{"local-secret", "remote-secret", "passwordKey"} {
		if strings.Contains(view, secret) {
			t.Fatalf("merge gate leaked %q:\n%s", secret, view)
		}
	}
}

func TestEditorFinishProducesMergedDecision(t *testing.T) {
	conflict := testConflict(t)
	key := bytes.Repeat([]byte{7}, 32)
	m := New(conflict, renderconflict.Auth{Key: key, RemoteKeyCompatible: true})
	session, err := renderconflict.PrepareMergeEditor(conflict.Details[0], key)
	if err != nil {
		t.Fatalf("prepare editor: %v", err)
	}
	if err := os.WriteFile(session.Path, []byte("merged-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.editorSession, m.editorIndex = session, 0
	next, _ := m.Update(editorFinishedMsg{})
	m = next.(Model)
	if m.items[0].Decision != DecisionMerged || len(m.items[0].MergedData) == 0 {
		t.Fatalf("item after editor = %+v", m.items[0])
	}
	if _, err := os.Stat(session.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("editor directory survived: %v", err)
	}
	plaintext, err := crypto.Decrypt(key, string(m.items[0].MergedData))
	if err != nil {
		t.Fatal(err)
	}
	var entry storage.EnvVarEntry
	if err := storage.FromJSON(plaintext, &entry); err != nil || entry.Value != "merged-secret" {
		t.Fatalf("merged ciphertext decoded to %+v err=%v", entry, err)
	}
}

func TestEditorFailureCleansUpAndLeavesPlanUnchanged(t *testing.T) {
	conflict := testConflict(t)
	key := bytes.Repeat([]byte{7}, 32)
	m := New(conflict, renderconflict.Auth{Key: key, RemoteKeyCompatible: true})
	session, err := renderconflict.PrepareMergeEditor(conflict.Details[0], key)
	if err != nil {
		t.Fatal(err)
	}
	m.editorSession, m.editorIndex = session, 0
	next, _ := m.Update(editorFinishedMsg{err: errors.New("editor exited 1")})
	m = next.(Model)
	if m.items[0].Decision != DecisionUnresolved || m.editorSession != nil {
		t.Fatalf("failed editor changed state: %+v", m.items[0])
	}
	if !strings.Contains(m.message, "editor exited 1") {
		t.Fatalf("editor failure not reported: %q", m.message)
	}
	if _, err := os.Stat(session.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed editor directory survived: %v", err)
	}
}

func TestEditorCommandRespectsVisualAndEditor(t *testing.T) {
	conflict := testConflict(t)
	key := bytes.Repeat([]byte{7}, 32)
	t.Setenv("VISUAL", "visual-editor")
	t.Setenv("EDITOR", "editor-editor")
	session, err := renderconflict.PrepareMergeEditor(conflict.Details[0], key)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if got := session.EditorCommand().Path; got != "visual-editor" {
		t.Fatalf("editor path = %q, want visual-editor", got)
	}
}
