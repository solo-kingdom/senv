package env

import (
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/storage"
)

// Manager handles environment variable operations
type Manager struct {
	storage        *storage.Manager
	password       string
	key            []byte
	mutationLocked bool
}

// NewManager creates a new environment variable manager
func NewManager(storage *storage.Manager, password string) *Manager {
	return &Manager{
		storage:  storage,
		password: password,
	}
}

// NewManagerWithKey creates a new environment variable manager with a derived key
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
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	return nil
}

func validateIdentity(group, key string) error {
	if err := validateGroup(group); err != nil {
		return err
	}
	if err := storage.ValidateName(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	if err := storage.ValidateEnvKey(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	return nil
}

// loadEnvGroup loads an environment variable group using key or password
func (m *Manager) loadEnvGroup(group string) (*storage.EnvGroup, error) {
	if m.key != nil {
		return m.storage.LoadEnvGroupWithKey(group, m.key)
	}
	return m.storage.LoadEnvGroup(group, m.password)
}

// saveEnvGroup saves an environment variable group using key or password
func (m *Manager) saveEnvGroup(envGroup *storage.EnvGroup) error {
	if m.key != nil {
		return m.storage.SaveEnvGroupWithKey(envGroup, m.key)
	}
	return m.storage.SaveEnvGroup(envGroup, m.password)
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

// Get retrieves an environment variable from a group
func (m *Manager) Get(group string, key string) (string, error) {
	if err := validateIdentity(group, key); err != nil {
		return "", err
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return "", err
	}

	entry, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey)
	if err != nil {
		if os.IsNotExist(err) {
			// 包哨兵本身而不是底层 securefs 错误：消息干净、不泄露内部
			// 存储布局，errors.Is(err, os.ErrNotExist) 仍可判定。
			return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, os.ErrNotExist)
		}
		// Fall back to group load (handles old-format groups not yet migrated)
		envGroup, loadErr := m.loadEnvGroup(group)
		if loadErr != nil {
			return "", fmt.Errorf("failed to load group %s: %w", group, loadErr)
		}
		value, exists := envGroup.Variables[key]
		if !exists {
			return "", fmt.Errorf("variable %s not found in group %s: %w", key, group, os.ErrNotExist)
		}
		return value, nil
	}
	return entry.Value, nil
}

// Set sets an environment variable in a group
func (m *Manager) Set(group string, key string, value string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Set(group, key, value) })
	}

	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}

	// Ensure group exists (migrate old format if needed)
	exists, err := m.storage.EnvGroupExists(group)
	if err != nil {
		return err
	}
	if !exists {
		envGroup := storage.NewEnvGroup(group)
		if err := m.saveEnvGroup(envGroup); err != nil {
			return fmt.Errorf("failed to create group %s: %w", group, err)
		}
	} else if _, err := m.storage.LoadEnvGroupMetaWithKey(group, cryptoKey); err != nil {
		// Old format exists but not yet migrated — trigger migration
		if _, err := m.loadEnvGroup(group); err != nil {
			return fmt.Errorf("failed to load group %s: %w", group, err)
		}
	}

	now := time.Now()
	entry := &storage.EnvVarEntry{Value: value, CreatedAt: now, UpdatedAt: now}

	// Preserve CreatedAt if the variable already exists
	if existing, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey); err == nil {
		entry.CreatedAt = existing.CreatedAt
	}

	if err := m.storage.SaveEnvVarWithKey(group, key, entry, cryptoKey); err != nil {
		return fmt.Errorf("failed to save variable %s: %w", key, err)
	}
	return nil
}

// Delete deletes an environment variable from a group
func (m *Manager) Delete(group string, key string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Delete(group, key) })
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}

	// Check existence (triggers migration if old format)
	if _, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey); err != nil {
		if os.IsNotExist(err) {
			// Maybe old format not yet migrated
			envGroup, loadErr := m.loadEnvGroup(group)
			if loadErr != nil {
				return fmt.Errorf("failed to load group %s: %w", group, loadErr)
			}
			if _, exists := envGroup.Variables[key]; !exists {
				return fmt.Errorf("variable %s not found in group %s", key, group)
			}
			// Migration happened via loadEnvGroup, try delete again
			return m.storage.DeleteEnvVar(group, key)
		}
		return fmt.Errorf("failed to check variable %s: %w", key, err)
	}

	return m.storage.DeleteEnvVar(group, key)
}

// List lists all environment variables in a group (or all groups if group is
// empty)，附耗时日志（组名是既有审计也使用的非敏感标识）。
func (m *Manager) List(group string) (map[string]map[string]string, error) {
	st := perflog.Start("env.list").With("group", group)
	res, err := m.listEntries(group)
	if err == nil {
		st.With("groups", len(res))
	}
	st.EndErr(err)
	return res, err
}

func (m *Manager) listEntries(group string) (map[string]map[string]string, error) {
	if group != "" {
		if err := validateGroup(group); err != nil {
			return nil, err
		}
	}
	result := make(map[string]map[string]string)

	if group != "" {
		envGroup, err := m.loadEnvGroup(group)
		if err != nil {
			return nil, fmt.Errorf("failed to load group %s: %w", group, err)
		}
		result[group] = envGroup.Variables
	} else {
		all, err := m.loadEnvVault()
		if err != nil {
			return nil, err
		}
		for g, eg := range all {
			result[g] = eg.Variables
		}
	}

	return result, nil
}

// loadEnvVault 一次锁内批量加载全部分组（key 或 password 认证，tui-perf-load）。
func (m *Manager) loadEnvVault() (map[string]*storage.EnvGroup, error) {
	if m.key != nil {
		return m.storage.LoadEnvVaultWithKey(m.key)
	}
	return m.storage.LoadEnvVault(m.password)
}

// Snapshot 一次批量加载全部分组与变量（单锁单 root），同时返回变量视图与
// 分组信息，供 TUI 等「分组列表 + 全部变量」消费方单趟取数。
func (m *Manager) Snapshot() (map[string]map[string]string, []GroupInfo, error) {
	st := perflog.Start("env.snapshot")
	all, err := m.loadEnvVault()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		st.End(false)
		return nil, nil, err
	}

	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)

	vars := make(map[string]map[string]string, len(all))
	gis := make([]GroupInfo, 0, len(all))
	for _, name := range names {
		eg := all[name]
		vars[name] = eg.Variables
		isActive := name == settings.DefaultGroup
		if !isActive {
			for _, g := range settings.ActiveGroups {
				if g == name {
					isActive = true
					break
				}
			}
		}
		gis = append(gis, GroupInfo{
			Name:      name,
			IsActive:  isActive,
			VarCount:  len(eg.Variables),
			IsDefault: name == settings.DefaultGroup,
		})
	}
	st.With("groups", len(gis), "items", len(vars)).End(true)
	return vars, gis, nil
}

// ExportVariables returns merged variables from active groups (default ∪
// activated). Later groups overwrite same key names, matching Export semantics.
func (m *Manager) ExportVariables() (map[string]string, error) {
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	activeGroups := []string{settings.DefaultGroup}
	for _, g := range settings.ActiveGroups {
		if g != settings.DefaultGroup {
			activeGroups = append(activeGroups, g)
		}
	}

	allVars := make(map[string]string)
	for _, group := range activeGroups {
		if err := validateGroup(group); err != nil {
			return nil, err
		}
		envGroup, err := m.loadEnvGroup(group)
		if err != nil {
			return nil, fmt.Errorf("failed to load active group %s: %w", group, err)
		}

		for k, v := range envGroup.Variables {
			if err := validateIdentity(group, k); err != nil {
				return nil, fmt.Errorf("invalid historical env identity: %w", err)
			}
			allVars[k] = v
		}
	}
	return allVars, nil
}

// FormatExportShell renders export statements for the given variables.
func FormatExportShell(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := vars[key]
		escapedValue := strings.ReplaceAll(value, "'", "'\\''")
		lines = append(lines, fmt.Sprintf("export %s='%s'", key, escapedValue))
	}
	return strings.Join(lines, "\n")
}

// Export exports environment variables from active groups
func (m *Manager) Export() (string, error) {
	vars, err := m.ExportVariables()
	if err != nil {
		return "", err
	}
	return FormatExportShell(vars), nil
}

// AddGroup creates a new environment variable group
func (m *Manager) AddGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddGroup(name) })
	}
	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	for _, g := range groups {
		if g == name {
			return fmt.Errorf("group %s already exists", name)
		}
	}

	envGroup := storage.NewEnvGroup(name)
	if err := m.saveEnvGroup(envGroup); err != nil {
		return fmt.Errorf("failed to create group %s: %w", name, err)
	}

	return nil
}

// ActivateGroup activates a group by adding it to the active groups list
func (m *Manager) ActivateGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.ActivateGroup(name) })
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	groupExists := false
	for _, g := range groups {
		if g == name {
			groupExists = true
			break
		}
	}

	if !groupExists {
		return fmt.Errorf("group %s does not exist", name)
	}

	if name == settings.DefaultGroup {
		return nil
	}

	for _, g := range settings.ActiveGroups {
		if g == name {
			return nil
		}
	}

	settings.ActiveGroups = append(settings.ActiveGroups, name)
	settings.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := m.storage.SaveSettings(settings); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// DeactivateGroup deactivates a group by removing it from the active groups list
func (m *Manager) DeactivateGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeactivateGroup(name) })
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	if name == settings.DefaultGroup {
		return fmt.Errorf("cannot deactivate default group")
	}

	newActiveGroups := []string{}
	for _, g := range settings.ActiveGroups {
		if g != name {
			newActiveGroups = append(newActiveGroups, g)
		}
	}

	settings.ActiveGroups = newActiveGroups
	settings.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := m.storage.SaveSettings(settings); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// ListGroups lists all groups and their status
// ListGroups 列出全部分组（完整加载每个分组），附耗时日志。
func (m *Manager) ListGroups() ([]GroupInfo, error) {
	st := perflog.Start("env.list-groups")
	gis, err := m.listGroupsInfo()
	if err == nil {
		st.With("groups", len(gis))
	}
	st.EndErr(err)
	return gis, err
}

func (m *Manager) listGroupsInfo() ([]GroupInfo, error) {
	_, gis, err := m.Snapshot()
	return gis, err
}

// GroupInfo represents information about a group
type GroupInfo struct {
	Name      string
	IsActive  bool
	VarCount  int
	IsDefault bool
}

// RenameKey atomically renames an environment variable inside its group. The
// value, creation time and file mode are preserved; the group must already
// exist and the destination key must be free.
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
	envGroup, err := m.loadEnvGroup(group)
	if err != nil {
		return fmt.Errorf("failed to load group %s: %w", group, err)
	}
	if _, ok := envGroup.Variables[oldKey]; !ok {
		return fmt.Errorf("variable %s not found in group %s", oldKey, group)
	}
	if _, ok := envGroup.Variables[newKey]; ok {
		return fmt.Errorf("variable %s already exists in group %s", newKey, group)
	}
	return m.storage.RenameEnvVar(group, oldKey, newKey)
}

// RenameGroup renames a non-default group. Its variables and activation state
// are preserved; the group metadata inside the moved directory is rewritten so
// the group stays loadable under its new name.
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
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if oldName == settings.DefaultGroup {
		return fmt.Errorf("cannot rename default group")
	}
	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}
	found := false
	for _, g := range groups {
		if g == newName {
			return fmt.Errorf("group %s already exists", newName)
		}
		if g == oldName {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("group %s does not exist", oldName)
	}

	// Loading first migrates a legacy single-blob group into the per-variable
	// format, so the directory rename below moves every variable.
	envGroup, err := m.loadEnvGroup(oldName)
	if err != nil {
		return fmt.Errorf("failed to load group %s: %w", oldName, err)
	}
	if err := m.storage.RenameEnvGroupDir(oldName, newName); err != nil {
		return err
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		_ = m.storage.RenameEnvGroupDir(newName, oldName)
		return err
	}
	meta := &storage.EnvGroupMeta{Name: newName, CreatedAt: envGroup.CreatedAt}
	if err := m.storage.SaveEnvGroupMetaWithKey(newName, meta, cryptoKey); err != nil {
		_ = m.storage.RenameEnvGroupDir(newName, oldName)
		return fmt.Errorf("failed to rewrite group metadata: %w", err)
	}

	// Preserve the activation state across the rename.
	updated := make([]string, 0, len(settings.ActiveGroups))
	changed := false
	for _, g := range settings.ActiveGroups {
		if g == oldName {
			updated = append(updated, newName)
			changed = true
			continue
		}
		updated = append(updated, g)
	}
	if changed {
		settings.ActiveGroups = updated
		settings.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := m.storage.SaveSettings(settings); err != nil {
			// Roll the metadata and directory back so activation names stay valid.
			_ = m.storage.SaveEnvGroupMetaWithKey(newName, &storage.EnvGroupMeta{Name: oldName, CreatedAt: envGroup.CreatedAt}, cryptoKey)
			_ = m.storage.RenameEnvGroupDir(newName, oldName)
			return fmt.Errorf("failed to update active groups: %w", err)
		}
	}
	return nil
}

// DeleteGroup deletes a group together with all of its variables. The default
// group can never be deleted. Deleting a group that is currently active
// changes the exported environment, so callers must pass allowActive=true after
// an explicit confirmation; the active set is updated in the same locked
// mutation.
func (m *Manager) DeleteGroup(name string, allowActive bool) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteGroup(name, allowActive) })
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if name == settings.DefaultGroup {
		return fmt.Errorf("cannot delete default group")
	}
	groups, err := m.storage.ListEnvGroups()
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

	isActive := false
	remaining := make([]string, 0, len(settings.ActiveGroups))
	for _, g := range settings.ActiveGroups {
		if g == name {
			isActive = true
			continue
		}
		remaining = append(remaining, g)
	}
	if isActive && !allowActive {
		return fmt.Errorf("group %s is active; deleting it removes its variables from the exported environment", name)
	}

	// Deactivate first: a stale active group name would make later exports fail,
	// while a group that survived a failed delete is merely inactive.
	if isActive {
		settings.ActiveGroups = remaining
		settings.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := m.storage.SaveSettings(settings); err != nil {
			return fmt.Errorf("failed to update active groups: %w", err)
		}
	}
	if err := m.storage.DeleteEnvGroup(name); err != nil {
		if isActive {
			settings.ActiveGroups = append(remaining, name)
			_ = m.storage.SaveSettings(settings)
		}
		return err
	}
	return nil
}
