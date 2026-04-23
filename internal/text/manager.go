package text

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Manager handles text block operations
type Manager struct {
	storage  *storage.Manager
	password string
	key      []byte
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
	entry, err := m.loadTextFile(group, key)
	if err != nil {
		return "", err
	}
	return entry.Value, nil
}

// Delete deletes a text entry from a group
func (m *Manager) Delete(group, key string) error {
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
	keys, err := m.storage.ListTextFiles(group)
	if err != nil {
		return nil, err
	}

	var result []TextInfo
	for _, key := range keys {
		entry, err := m.loadTextFile(group, key)
		if err != nil {
			continue // Skip entries that fail to load
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
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read from input: %w", err)
	}

	return m.Set(group, key, string(data))
}

// SetViaEditor opens an editor for creating or editing a text entry
func (m *Manager) SetViaEditor(group, key string) error {
	// Check if entry exists (to pre-fill content)
	var existingContent string
	entry, err := m.loadTextFile(group, key)
	if err == nil {
		existingContent = entry.Value
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "senv-text-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write existing content
	if existingContent != "" {
		if _, err := tmpFile.WriteString(existingContent); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write temp file: %w", err)
		}
	}
	tmpFile.Close()

	// Set restrictive permissions
	os.Chmod(tmpPath, 0o600)

	// Get editor
	editor := getEditor()

	// Open editor
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run editor: %w", err)
	}

	// Read edited content
	editedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	// Check if content changed
	if string(editedContent) == existingContent {
		fmt.Println("No changes detected")
		return nil
	}

	// Save
	return m.Set(group, key, string(editedContent))
}

// GetToFile writes a text entry's value to a file
func (m *Manager) GetToFile(group, key, outputPath string) error {
	value, err := m.Get(group, key)
	if err != nil {
		return err
	}

	// Expand home directory
	outputPath = expandHome(outputPath)

	// Create parent directory if needed
	dir := outputPath[:strings.LastIndex(outputPath, "/")]
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(outputPath, []byte(value), 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputPath, err)
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
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	for _, g := range groups {
		if g == name {
			return fmt.Errorf("group %s already exists", name)
		}
	}

	// Create the directory by saving a placeholder and removing it
	groupDir := m.storage.GetDataPath() + "/" + storage.TextDirName + "/" + name
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		return fmt.Errorf("failed to create group directory: %w", err)
	}

	return nil
}

// DeleteGroup deletes a text group and all its contents
func (m *Manager) DeleteGroup(name string) error {
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	found := false
	for _, g := range groups {
		if g == name {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("group %s does not exist", name)
	}

	groupDir := m.storage.GetDataPath() + "/" + storage.TextDirName + "/" + name
	if err := os.RemoveAll(groupDir); err != nil {
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
		count := 0
		if err == nil {
			count = len(keys)
		}
		result = append(result, GroupInfo{
			Name:     name,
			KeyCount: count,
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
