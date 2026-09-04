package text

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/storage"
)

// Manager handles text block operations
type Manager struct {
	storage        *storage.Manager
	password       string
	key            []byte
	mutationLocked bool
}

// NewManager creates a new text manager with password
func NewManager(storage *storage.Manager, password string) *Manager {
	return &Manager{
		storage:  storage,
		password: password,
	}
}

// NewManagerWithKey creates a new text manager with a derived key
func NewManagerWithKey(storage *storage.Manager, key []byte) *Manager {
	return &Manager{
		storage: storage,
		key:     key,
	}
}

func (m *Manager) mutate(fn func(*Manager) error) error {
	if m.mutationLocked {
		return fn(m)
	}
	return m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		clone := *m
		clone.storage = locked
		clone.mutationLocked = true
		return fn(&clone)
	})
}

func validateGroup(group string) error {
	if err := storage.ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	return nil
}

func validateIdentity(group, key string) error {
	if err := validateGroup(group); err != nil {
		return err
	}
	if err := storage.ValidateName(key); err != nil {
		return fmt.Errorf("invalid text key %q: %w", key, err)
	}
	return nil
}

// saveTextFile saves a text entry using key or password
func (m *Manager) saveTextFile(group, key string, entry *storage.TextEntry) error {
	if m.key != nil {
		return m.storage.SaveTextFileWithKey(group, key, entry, m.key)
	}
	return m.storage.SaveTextFile(group, key, entry, m.password)
}

// loadTextFile loads a text entry using key or password
func (m *Manager) loadTextFile(group, key string) (*storage.TextEntry, error) {
	if m.key != nil {
		return m.storage.LoadTextFileWithKey(group, key, m.key)
	}
	return m.storage.LoadTextFile(group, key, m.password)
}

// Set sets a text entry in a group
func (m *Manager) Set(group, key, value string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Set(group, key, value) })
	}

	// Size check
	if len(value) > storage.MaxTextSize {
		return fmt.Errorf("text value exceeds %d bytes limit (%d bytes)", storage.MaxTextSize, len(value))
	}

	// Check if entry already exists (to preserve CreatedAt)
	entry, err := m.loadTextFile(group, key)
	if err != nil {
		// New entry
		entry = storage.NewTextEntry(value)
	} else {
		// Update existing entry, preserve CreatedAt
		entry.Value = value
		entry.Size = len(value)
		entry.UpdatedAt = time.Now()
	}

	return m.saveTextFile(group, key, entry)
}

// Get retrieves a text entry's value from a group
func (m *Manager) Get(group, key string) (string, error) {
	if err := validateIdentity(group, key); err != nil {
		return "", err
	}
	entry, err := m.loadTextFile(group, key)
	if err != nil {
		return "", err
	}
	return entry.Value, nil
}

// Delete deletes a text entry from a group
func (m *Manager) Delete(group, key string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Delete(group, key) })
	}
	// Verify it exists first
	_, err := m.loadTextFile(group, key)
	if err != nil {
		return err
	}

	return m.storage.DeleteTextFile(group, key)
}

// TextInfo contains metadata about a text entry for listing
type TextInfo struct {
	Key       string
	Size      int
	UpdatedAt time.Time
}

// List lists all text entries in a group with metadata
func (m *Manager) List(group string) ([]TextInfo, error) {
	if err := validateGroup(group); err != nil {
		return nil, err
	}
	keys, err := m.storage.ListTextFiles(group)
	if err != nil {
		return nil, err
	}

	var result []TextInfo
	for _, key := range keys {
		entry, err := m.loadTextFile(group, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load text %q in group %q: %w", key, group, err)
		}
		result = append(result, TextInfo{
			Key:       key,
			Size:      entry.Size,
			UpdatedAt: entry.UpdatedAt,
		})
	}

	return result, nil
}

// SetFromFile sets a text entry from a file
func (m *Manager) SetFromFile(group, key, filePath string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	// Expand home directory
	filePath = expandHome(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	return m.Set(group, key, string(data))
}

// SetFromReader sets a text entry from an io.Reader
func (m *Manager) SetFromReader(group, key string, reader io.Reader) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read from input: %w", err)
	}

	return m.Set(group, key, string(data))
}

// EditorSession holds the state of a pending editor invocation: the temp file
// path and the original content (to detect whether anything changed).
//
// The flow is split into PrepareEditor -> (run editor on TmpPath) -> FinishEditor
// so the TUI can run the editor through bubbletea's tea.ExecProcess (which
// suspends and restores the TUI) instead of blocking the program loop. The
// legacy CLI keeps using SetViaEditor which wraps both steps.
type EditorSession struct {
	Group    string
	Key      string
	TmpPath  string
	Original string
}

// PrepareEditor decrypts the entry (or starts empty) into a 0600 temp file and
// returns the session. The caller runs the editor on TmpPath, then calls
// FinishEditor to persist.
func (m *Manager) PrepareEditor(group, key string) (*EditorSession, error) {
	if err := validateIdentity(group, key); err != nil {
		return nil, err
	}
	var original string
	if entry, err := m.loadTextFile(group, key); err == nil {
		original = entry.Value
	}

	tmpFile, err := os.CreateTemp("", "senv-text-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if original != "" {
		if _, err := tmpFile.WriteString(original); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to set temp file permissions: %w", err)
	}

	return &EditorSession{Group: group, Key: key, TmpPath: tmpPath, Original: original}, nil
}

// FinishEditor reads the edited temp file, re-encrypts when the content changed,
// and removes the temp file. It returns changed=true when a new value was
// persisted.
func (m *Manager) FinishEditor(s *EditorSession) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("editor session is nil")
	}
	if err := validateIdentity(s.Group, s.Key); err != nil {
		return false, err
	}
	defer os.Remove(s.TmpPath)

	editedContent, err := os.ReadFile(s.TmpPath)
	if err != nil {
		return false, fmt.Errorf("failed to read edited file: %w", err)
	}

	if string(editedContent) == s.Original {
		return false, nil
	}

	if err := m.Set(s.Group, s.Key, string(editedContent)); err != nil {
		return false, err
	}
	return true, nil
}

// EditorCommand builds the exec.Cmd for the configured editor on the session's
// temp file, wired to the real stdio. The TUI passes this to tea.ExecProcess.
func (s *EditorSession) EditorCommand() *exec.Cmd {
	cmd := exec.Command(getEditor(), s.TmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// SetViaEditor opens an editor for creating or editing a text entry. It is the
// one-shot wrapper around PrepareEditor/FinishEditor used by the CLI.
func (m *Manager) SetViaEditor(group, key string) error {
	s, err := m.PrepareEditor(group, key)
	if err != nil {
		return err
	}

	if err := s.EditorCommand().Run(); err != nil {
		os.Remove(s.TmpPath)
		return fmt.Errorf("failed to run editor: %w", err)
	}

	changed, err := m.FinishEditor(s)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("No changes detected")
	}
	return nil
}

// GetToFile writes a text entry using the private default export mode.
func (m *Manager) GetToFile(group, key, outputPath string) error {
	return m.GetToFileWithMode(group, key, outputPath, exportfile.DefaultMode)
}

// GetToFileWithMode writes a text entry using a mode selected for this
// operation only.
func (m *Manager) GetToFileWithMode(group, key, outputPath string, mode fs.FileMode) error {
	value, err := m.Get(group, key)
	if err != nil {
		return err
	}
	return m.ExportValue(value, outputPath, mode)
}

// ExportValue securely writes an already-resolved text value. This allows the
// CLI decode path to export exactly the value it displays.
func (m *Manager) ExportValue(value, outputPath string, mode fs.FileMode) error {
	if err := exportfile.WriteFile(outputPath, []byte(value), mode); err != nil {
		return fmt.Errorf("failed to export text: %w", err)
	}
	return nil
}

// GetToClipboard copies a text entry's value to the system clipboard
func (m *Manager) GetToClipboard(group, key string) error {
	value, err := m.Get(group, key)
	if err != nil {
		return err
	}

	// Try to find a clipboard command
	var cmd *exec.Cmd
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd = exec.Command("pbcopy")
	} else if _, err := exec.LookPath("xclip"); err == nil {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd = exec.Command("wl-copy")
	} else {
		return fmt.Errorf("no clipboard command found (install pbcopy, xclip, xsel, or wl-copy)")
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open clipboard stdin: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start clipboard command: %w", err)
	}

	if _, err := stdin.Write([]byte(value)); err != nil {
		return fmt.Errorf("failed to write to clipboard: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clipboard command failed: %w", err)
	}

	return nil
}

// AddGroup creates a new text group directory
func (m *Manager) AddGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddGroup(name) })
	}
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}
	for _, group := range groups {
		if group == name {
			return fmt.Errorf("group %s already exists", name)
		}
	}
	if err := m.storage.AddTextGroup(name); err != nil {
		return fmt.Errorf("failed to create group directory: %w", err)
	}
	return nil
}

// DeleteGroup deletes a text group and all its contents
func (m *Manager) DeleteGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteGroup(name) })
	}
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}
	found := false
	for _, group := range groups {
		if group == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("group %s does not exist", name)
	}
	if err := m.storage.DeleteTextGroup(name); err != nil {
		return fmt.Errorf("failed to delete group %s: %w", name, err)
	}
	return nil
}

// ListGroups lists all text groups with their key counts
type GroupInfo struct {
	Name     string
	KeyCount int
}

func (m *Manager) ListGroups() ([]GroupInfo, error) {
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}

	var result []GroupInfo
	for _, name := range groups {
		keys, err := m.storage.ListTextFiles(name)
		if err != nil {
			return nil, fmt.Errorf("failed to list text group %q: %w", name, err)
		}
		result = append(result, GroupInfo{
			Name:     name,
			KeyCount: len(keys),
		})
	}

	return result, nil
}

// getEditor returns the editor to use, checking $VISUAL, $EDITOR, then falling back
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	return "vim"
}

// expandHome expands ~ to the home directory
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}
