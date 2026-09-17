package text

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/perflog"
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

func (m *Manager) resolveCryptoKey() ([]byte, error) {
	if m.key != nil {
		return m.key, nil
	}
	md, err := m.storage.LoadMetadata()
	if err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		return nil, err
	}
	iterations, err := md.ValidatedKDFIterations()
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKeyWithIterations(m.password, salt, iterations), nil
}

func (m *Manager) requireGroup(group string) error {
	exists, err := m.storage.TextGroupExists(group)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrGroupMissing("text", group)
	}
	return nil
}

// Set sets a text entry in a group. The group must already exist.
func (m *Manager) Set(group, key, value string) error {
	return m.SetWithDescription(group, key, value, nil)
}

// SetWithDescription sets a text value. description nil keeps the existing
// (or empty) description; non-nil replaces it.
func (m *Manager) SetWithDescription(group, key, value string, description *string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if description != nil {
		desc, err := storage.ValidateDescription(*description, true)
		if err != nil {
			return err
		}
		description = &desc
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SetWithDescription(group, key, value, description)
		})
	}

	if len(value) > storage.MaxTextSize {
		return fmt.Errorf("text value exceeds %d bytes limit (%d bytes)", storage.MaxTextSize, len(value))
	}
	if err := m.requireGroup(group); err != nil {
		return err
	}

	entry, err := m.loadTextFile(group, key)
	if err != nil {
		entry = storage.NewTextEntry(value)
	} else {
		entry.Value = value
		entry.Size = len(value)
		entry.UpdatedAt = time.Now()
	}
	if description != nil {
		entry.Description = *description
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

// GetWithMeta returns the stored value and description. Missing description is empty.
func (m *Manager) GetWithMeta(group, key string) (value, description string, err error) {
	if err := validateIdentity(group, key); err != nil {
		return "", "", err
	}
	entry, err := m.loadTextFile(group, key)
	if err != nil {
		return "", "", err
	}
	return entry.Value, entry.Description, nil
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
	Key         string
	Description string
	Size        int
	UpdatedAt   time.Time
}

// List lists all text entries in a group with metadata
// List 列出分组内 text 条目，附耗时日志。
func (m *Manager) List(group string) ([]TextInfo, error) {
	st := perflog.Start("text.list").With("group", group)
	res, err := m.listEntries(group)
	if err == nil {
		st.With("items", len(res))
	}
	st.EndErr(err)
	return res, err
}

func (m *Manager) listEntries(group string) ([]TextInfo, error) {
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
			Key:         key,
			Description: entry.Description,
			Size:        entry.Size,
			UpdatedAt:   entry.UpdatedAt,
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

	return m.SetWithDescription(group, key, string(data), nil)
}

// SetFromFileWithDescription is SetFromFile and optionally replaces the description.
func (m *Manager) SetFromFileWithDescription(group, key, filePath string, description *string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	filePath = expandHome(filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	return m.SetWithDescription(group, key, string(data), description)
}

// SetFromReader sets a text entry from an io.Reader
func (m *Manager) SetFromReader(group, key string, reader io.Reader) error {
	return m.SetFromReaderWithDescription(group, key, reader, nil)
}

// SetFromReaderWithDescription is SetFromReader and optionally replaces the description.
func (m *Manager) SetFromReaderWithDescription(group, key string, reader io.Reader, description *string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read from input: %w", err)
	}

	return m.SetWithDescription(group, key, string(data), description)
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

// AddGroup creates a new text group. Description is required.
func (m *Manager) AddGroup(name string, description string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	desc, err := storage.ValidateDescription(description, false)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddGroup(name, description) })
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
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}
	meta := &storage.EnvGroupMeta{Name: name, Description: desc, CreatedAt: time.Now()}
	if err := m.storage.SaveTextGroupMetaWithKey(name, meta, cryptoKey); err != nil {
		return fmt.Errorf("failed to save group metadata: %w", err)
	}
	return nil
}

// EnsureGroup creates the group when missing. Existing groups are left unchanged,
// including reserved buckets such as llm-keys on vaults initialized before the
// reserved group was created at init time.
func (m *Manager) EnsureGroup(name, description string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	exists, err := m.storage.TextGroupExists(name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.AddGroup(name, description)
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
	Name        string
	Description string
	KeyCount    int
}

// ListGroups 列出全部分组，附耗时日志。
func (m *Manager) ListGroups() ([]GroupInfo, error) {
	st := perflog.Start("text.list-groups")
	res, err := m.listGroupsInfo()
	if err == nil {
		st.With("groups", len(res))
	}
	st.EndErr(err)
	return res, err
}

func (m *Manager) listGroupsInfo() ([]GroupInfo, error) {
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
		desc := ""
		if cryptoKey, err := m.resolveCryptoKey(); err == nil {
			if meta, err := m.storage.LoadTextGroupMetaWithKey(name, cryptoKey); err == nil && meta != nil {
				desc = meta.Description
			}
		}
		result = append(result, GroupInfo{
			Name:        name,
			Description: desc,
			KeyCount:    len(keys),
		})
	}

	return result, nil
}

// Snapshot 聚合 text 全部分组与条目的元数据视图（单趟读取的产物）。
// KeyCount 与分组列出沿用逐组语义：某组条目读取失败时该组仍列出（计数
// 为列出的文件数）、Items 中缺失对应条目。
type Snapshot struct {
	Groups []GroupInfo           // 全部分组（含空分组）及 key 计数
	Items  map[string][]TextInfo // group → 条目元数据（与 List 返回逐字段一致）
}

// Snapshot 单趟读取并解密全部分组与条目（一次 vault 读锁、共享单个 data
// root，替代逐组 ListGroups+List 的 N 次排它锁），返回聚合视图供 TUI
// 「分组列表 + 全部条目」一次取数。条目明文不留在返回结构里（tab 只渲染
// 元数据），内容正确性与逐组 List 路径一致；条目级失败的组按组降级
// （与逐组消费方 List 失败后置空该组等价）。
func (m *Manager) Snapshot() (*Snapshot, error) {
	st := perflog.Start("text.snapshot")
	vault, err := m.loadTextVault()
	if err != nil {
		st.End(false)
		return nil, err
	}
	out := &Snapshot{
		Groups: make([]GroupInfo, 0, len(vault.Groups)),
		Items:  make(map[string][]TextInfo, len(vault.Groups)),
	}
	total := 0
	for _, name := range vault.Groups {
		desc := ""
		if cryptoKey, err := m.resolveCryptoKey(); err == nil {
			if meta, err := m.storage.LoadTextGroupMetaWithKey(name, cryptoKey); err == nil && meta != nil {
				desc = meta.Description
			}
		}
		out.Groups = append(out.Groups, GroupInfo{Name: name, Description: desc, KeyCount: vault.KeyCount[name]})
		entries, ok := vault.Entries[name]
		if !ok {
			continue // 条目级失败组：列出但无条目（vault.Errors[name] 有原因）
		}
		infos := make([]TextInfo, 0, len(entries))
		for _, f := range entries {
			infos = append(infos, TextInfo{Key: f.Key, Description: f.Entry.Description, Size: f.Entry.Size, UpdatedAt: f.Entry.UpdatedAt})
		}
		out.Items[name] = infos
		total += len(infos)
	}
	st.With("groups", len(out.Groups), "items", total, "failed_groups", len(vault.Errors)).End(true)
	return out, nil
}

// loadTextVault 是 Snapshot 的装载缝：key 与 password 两种认证同等待遇。
func (m *Manager) loadTextVault() (*storage.TextVaultSnapshot, error) {
	if m.key != nil {
		return m.storage.LoadTextVaultWithKey(m.key)
	}
	return m.storage.LoadTextVault(m.password)
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

// RenameKey atomically renames a text block inside its group. Contents, size
// and timestamps are preserved; the destination key must be free.
func (m *Manager) RenameKey(group, oldKey, newKey string) error {
	if err := validateIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateIdentity(group, newKey); err != nil {
		return err
	}
	if oldKey == newKey {
		return nil
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameKey(group, oldKey, newKey) })
	}
	if _, err := m.loadTextFile(group, oldKey); err != nil {
		return fmt.Errorf("text block %s/%s not found: %w", group, oldKey, err)
	}
	if _, err := m.loadTextFile(group, newKey); err == nil {
		return fmt.Errorf("text block %s already exists in group %s", newKey, group)
	}
	return m.storage.RenameTextFile(group, oldKey, newKey)
}

// RenameGroup renames a text group directory; every block inside keeps its
// content.
func (m *Manager) RenameGroup(oldName, newName string) error {
	if err := validateGroup(oldName); err != nil {
		return err
	}
	if err := validateGroup(newName); err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameGroup(oldName, newName) })
	}
	groups, err := m.storage.ListTextGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}
	found := false
	for _, group := range groups {
		if group == newName {
			return fmt.Errorf("group %s already exists", newName)
		}
		if group == oldName {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("group %s does not exist", oldName)
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}
	meta, err := m.storage.LoadTextGroupMetaWithKey(oldName, cryptoKey)
	if err != nil {
		return err
	}
	if err := m.storage.RenameTextGroup(oldName, newName); err != nil {
		return err
	}
	if meta != nil {
		meta.Name = newName
		if err := m.storage.SaveTextGroupMetaWithKey(newName, meta, cryptoKey); err != nil {
			_ = m.storage.RenameTextGroup(newName, oldName)
			return fmt.Errorf("failed to rewrite group metadata: %w", err)
		}
	}
	return nil
}
