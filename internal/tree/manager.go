// Package tree implements encrypted multi-level file tree storage and
// incremental directory sync (ADR-0036).
package tree

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/syncschema"
)

const (
	SchemaVersion = 1
	MaxPlainBytes = 384 << 10 // 384 KiB plaintext cap (fits well under 512 KiB ciphertext limit)
)

// Entry is one encrypted file stored under trees/<group>/<relPath>.enc.
type Entry struct {
	Schema      int       `json:"schema"`
	Mode        uint32    `json:"mode"`     // 0600, 0644, or 0755
	Encoding    string    `json:"encoding"` // "utf8" or "base64"
	Content     string    `json:"content"`
	SHA256      string    `json:"sha256"`
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Runtime-only fields
	Group string `json:"-"`
	Path  string `json:"-"`
}

// Meta is lightweight listing metadata without decrypting large diffs when unnecessary.
type Meta struct {
	Group       string
	Path        string
	Mode        uint32
	Size        int
	SHA256      string
	Description string
	UpdatedAt   time.Time
}

// DiffAction classifies a file difference between vault and local dir.
type DiffAction string

const (
	DiffAdd    DiffAction = "add"
	DiffModify DiffAction = "modify"
	DiffChmod  DiffAction = "chmod"
	DiffDelete DiffAction = "delete"
)

// DiffItem describes one change between vault and local directory.
type DiffItem struct {
	Path   string
	Action DiffAction
	Detail string
}

// Manager operates on tree entries using storage.Manager + unlocked crypto key.
type Manager struct {
	store     *storage.Manager
	cryptoKey []byte
}

// NewManager creates a tree Manager.
func NewManager(store *storage.Manager, cryptoKey []byte) *Manager {
	return &Manager{store: store, cryptoKey: cryptoKey}
}

func (m *Manager) requireGroup(group string) error {
	if err := storage.ValidateName(group); err != nil {
		return fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	meta, err := m.store.LoadTreeGroupMeta(group, m.cryptoKey)
	if err != nil {
		return err
	}
	if meta == nil {
		// Also accept existing group dir with entries (e.g., synced before meta)
		paths, err := m.store.ListTreePaths(group)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return storage.ErrGroupMissing("tree", group)
		}
	}
	return nil
}

// ListGroups lists tree groups and their descriptions.
func (m *Manager) ListGroups() ([]string, map[string]string, error) {
	groups, err := m.store.ListTreeGroups()
	if err != nil {
		return nil, nil, err
	}
	descs := make(map[string]string, len(groups))
	for _, g := range groups {
		if meta, err := m.store.LoadTreeGroupMeta(g, m.cryptoKey); err == nil && meta != nil {
			descs[g] = meta.Description
		}
	}
	return groups, descs, nil
}

// AddGroup creates a tree group with a required description.
func (m *Manager) AddGroup(group, description string) error {
	return m.store.SaveTreeGroupMeta(group, description, m.cryptoKey)
}

// DeleteGroup deletes a tree group (must be empty unless force=true).
func (m *Manager) DeleteGroup(group string, force bool) error {
	return m.store.DeleteTreeGroup(group, force)
}

// NormalizeMode maps arbitrary file permissions to 0600 / 0644 / 0755.
func NormalizeMode(mode fs.FileMode, forceExec bool) uint32 {
	if forceExec || mode&0o111 != 0 {
		return 0o755
	}
	if mode&0o077 == 0 {
		return 0o600
	}
	return 0o644
}

// DecodeBytes extracts the raw file bytes from an Entry.
func (e *Entry) DecodeBytes() ([]byte, error) {
	switch e.Encoding {
	case "", "utf8":
		return []byte(e.Content), nil
	case "base64":
		return base64.StdEncoding.DecodeString(e.Content)
	default:
		return nil, fmt.Errorf("unsupported tree encoding %q", e.Encoding)
	}
}

func encodeBytes(raw []byte) (encoding, content, sum string, err error) {
	if len(raw) > MaxPlainBytes {
		return "", "", "", fmt.Errorf("file size %d exceeds %d bytes limit", len(raw), MaxPlainBytes)
	}
	h := sha256.Sum256(raw)
	sum = hex.EncodeToString(h[:])
	if utf8.Valid(raw) {
		return "utf8", string(raw), sum, nil
	}
	return "base64", base64.StdEncoding.EncodeToString(raw), sum, nil
}

// Set writes or updates one file in the tree.
func (m *Manager) Set(group, relPath string, raw []byte, mode uint32, description string, keepDesc bool) (*Entry, error) {
	if err := m.requireGroup(group); err != nil {
		return nil, err
	}
	if err := syncschema.ValidateRelativePath(relPath); err != nil {
		return nil, err
	}
	enc, content, sum, err := encodeBytes(raw)
	if err != nil {
		return nil, err
	}
	desc := strings.TrimSpace(description)
	if keepDesc {
		var existing Entry
		if err := m.store.LoadTreeJSON(group, relPath, &existing, m.cryptoKey); err == nil {
			desc = existing.Description
		}
	} else {
		desc, err = storage.ValidateDescription(desc, true)
		if err != nil {
			return nil, err
		}
	}
	if mode != 0o600 && mode != 0o644 && mode != 0o755 {
		mode = 0o644
	}
	entry := &Entry{
		Schema:      SchemaVersion,
		Mode:        mode,
		Encoding:    enc,
		Content:     content,
		SHA256:      sum,
		Description: desc,
		UpdatedAt:   time.Now().UTC(),
		Group:       group,
		Path:        relPath,
	}
	if err := m.store.SaveTreeJSON(group, relPath, entry, m.cryptoKey); err != nil {
		return nil, err
	}
	return entry, nil
}

// Get loads and decrypts one file from the tree.
func (m *Manager) Get(group, relPath string) (*Entry, error) {
	if err := syncschema.ValidateRelativePath(relPath); err != nil {
		return nil, err
	}
	var entry Entry
	if err := m.store.LoadTreeJSON(group, relPath, &entry, m.cryptoKey); err != nil {
		return nil, err
	}
	entry.Group = group
	entry.Path = relPath
	return &entry, nil
}

// Remove deletes one file from the tree.
func (m *Manager) Remove(group, relPath string) error {
	if err := syncschema.ValidateRelativePath(relPath); err != nil {
		return err
	}
	// Verify existence first so deleting non-existent file returns clear error
	var entry Entry
	if err := m.store.LoadTreeJSON(group, relPath, &entry, m.cryptoKey); err != nil {
		return err
	}
	return m.store.DeleteTreeFile(group, relPath)
}

// List lists entries under group (optionally filtered by path prefix).
func (m *Manager) List(group, prefix string) ([]Meta, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		if err := syncschema.ValidateRelativePath(prefix); err != nil {
			return nil, err
		}
	}
	paths, err := m.store.ListTreePaths(group)
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, p := range paths {
		if prefix != "" && p != prefix && !strings.HasPrefix(p, prefix+"/") {
			continue
		}
		var e Entry
		if err := m.store.LoadTreeJSON(group, p, &e, m.cryptoKey); err != nil {
			return nil, err
		}
		raw, err := e.DecodeBytes()
		if err != nil {
			return nil, err
		}
		out = append(out, Meta{
			Group:       group,
			Path:        p,
			Mode:        e.Mode,
			Size:        len(raw),
			SHA256:      e.SHA256,
			Description: e.Description,
			UpdatedAt:   e.UpdatedAt,
		})
	}
	return out, nil
}

// ShouldIgnoreDefault reports whether relPath is skipped by default during `tree push`.
func ShouldIgnoreDefault(relPath string) bool {
	parts := strings.Split(relPath, "/")
	for _, seg := range parts {
		if seg == ".git" || seg == ".DS_Store" {
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") && base != ".env.example" {
		return true
	}
	for _, suf := range []string{".dump", ".sql.gz", ".key", ".pem", ".p12", ".bak"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.Contains(base, ".bak-") {
		return true
	}
	return false
}

// IsSensitiveMCPPath rejects writing secret-like paths from MCP senv_tree_set.
func IsSensitiveMCPPath(relPath string) bool {
	base := filepath.Base(relPath)
	if base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example") {
		return true
	}
	for _, suf := range []string{".key", ".pem", ".p12"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

func scanLocalDir(localDir string, includeIgnored bool) (map[string]struct {
	raw  []byte
	mode uint32
	sum  string
}, error) {
	info, err := os.Stat(localDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", localDir)
	}
	files := make(map[string]struct {
		raw  []byte
		mode uint32
		sum  string
	})
	err = filepath.WalkDir(localDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p == localDir {
			return nil
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !includeIgnored && ShouldIgnoreDefault(rel) {
			return nil
		}
		if err := syncschema.ValidateRelativePath(rel); err != nil {
			return fmt.Errorf("unsafe local path %q: %w", rel, err)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(raw) > MaxPlainBytes {
			return fmt.Errorf("local file %q (%d bytes) exceeds %d bytes limit", rel, len(raw), MaxPlainBytes)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		h := sha256.Sum256(raw)
		files[rel] = struct {
			raw  []byte
			mode uint32
			sum  string
		}{
			raw:  raw,
			mode: NormalizeMode(fi.Mode(), false),
			sum:  hex.EncodeToString(h[:]),
		}
		return nil
	})
	return files, err
}

func joinTreePrefix(prefix, rel string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

func trimTreePrefix(prefix, full string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return full
	}
	return strings.TrimPrefix(full, prefix+"/")
}

// Push incrementally uploads localDir into group[:prefix].
func (m *Manager) Push(localDir, group, prefix string, includeIgnored, deleteRemote, dryRun bool) ([]DiffItem, error) {
	if err := m.requireGroup(group); err != nil {
		return nil, err
	}
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		if err := syncschema.ValidateRelativePath(prefix); err != nil {
			return nil, err
		}
	}
	localFiles, err := scanLocalDir(localDir, includeIgnored)
	if err != nil {
		return nil, err
	}
	remoteMetas, err := m.List(group, prefix)
	if err != nil {
		return nil, err
	}
	remoteMap := make(map[string]Meta, len(remoteMetas))
	for _, rm := range remoteMetas {
		remoteMap[rm.Path] = rm
	}

	var plan []DiffItem
	localPaths := make([]string, 0, len(localFiles))
	for rel := range localFiles {
		localPaths = append(localPaths, rel)
	}
	sort.Strings(localPaths)

	for _, rel := range localPaths {
		lf := localFiles[rel]
		fullPath := joinTreePrefix(prefix, rel)
		rm, exists := remoteMap[fullPath]
		if !exists {
			plan = append(plan, DiffItem{Path: fullPath, Action: DiffAdd, Detail: fmt.Sprintf("mode %04o (%d B)", lf.mode, len(lf.raw))})
			if !dryRun {
				if _, err := m.Set(group, fullPath, lf.raw, lf.mode, "", true); err != nil {
					return nil, err
				}
			}
			continue
		}
		if rm.SHA256 != lf.sum || rm.Mode != lf.mode {
			act := DiffModify
			if rm.SHA256 == lf.sum {
				act = DiffChmod
			}
			plan = append(plan, DiffItem{Path: fullPath, Action: act, Detail: fmt.Sprintf("mode %04o (%d B)", lf.mode, len(lf.raw))})
			if !dryRun {
				if _, err := m.Set(group, fullPath, lf.raw, lf.mode, "", true); err != nil {
					return nil, err
				}
			}
		}
	}

	if deleteRemote {
		for _, rm := range remoteMetas {
			rel := trimTreePrefix(prefix, rm.Path)
			if _, ok := localFiles[rel]; !ok {
				plan = append(plan, DiffItem{Path: rm.Path, Action: DiffDelete})
				if !dryRun {
					if err := m.Remove(group, rm.Path); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return plan, nil
}

// Diff compares vault group[:prefix] against localDir (vault -> local perspective).
func (m *Manager) Diff(group, prefix, localDir string) ([]DiffItem, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		if err := syncschema.ValidateRelativePath(prefix); err != nil {
			return nil, err
		}
	}
	remoteMetas, err := m.List(group, prefix)
	if err != nil {
		return nil, err
	}
	localFiles, err := scanLocalDir(localDir, false)
	if err != nil {
		return nil, err
	}
	remoteMap := make(map[string]Meta, len(remoteMetas))
	for _, rm := range remoteMetas {
		remoteMap[trimTreePrefix(prefix, rm.Path)] = rm
	}

	seen := make(map[string]bool)
	var all []string
	for rel := range remoteMap {
		seen[rel] = true
		all = append(all, rel)
	}
	for rel := range localFiles {
		if !seen[rel] {
			all = append(all, rel)
		}
	}
	sort.Strings(all)

	var diffs []DiffItem
	for _, rel := range all {
		rm, inRemote := remoteMap[rel]
		lf, inLocal := localFiles[rel]
		switch {
		case inRemote && !inLocal:
			diffs = append(diffs, DiffItem{Path: rm.Path, Action: DiffAdd, Detail: "present in vault, missing locally"})
		case !inRemote && inLocal:
			diffs = append(diffs, DiffItem{Path: joinTreePrefix(prefix, rel), Action: DiffDelete, Detail: "only in local dir"})
		case rm.SHA256 != lf.sum:
			diffs = append(diffs, DiffItem{Path: rm.Path, Action: DiffModify, Detail: fmt.Sprintf("vault %s != local %s", rm.SHA256[:8], lf.sum[:8])})
		case rm.Mode != lf.mode:
			diffs = append(diffs, DiffItem{Path: rm.Path, Action: DiffChmod, Detail: fmt.Sprintf("vault %04o != local %04o", rm.Mode, lf.mode)})
		}
	}
	return diffs, nil
}

// Pull incrementally materializes group[:prefix] into destDir.
func (m *Manager) Pull(group, prefix, destDir string, deleteLocal, dryRun bool) ([]DiffItem, error) {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		if err := syncschema.ValidateRelativePath(prefix); err != nil {
			return nil, err
		}
	}
	remoteMetas, err := m.List(group, prefix)
	if err != nil {
		return nil, err
	}
	remoteRel := make(map[string]Meta, len(remoteMetas))
	for _, rm := range remoteMetas {
		remoteRel[trimTreePrefix(prefix, rm.Path)] = rm
	}

	var plan []DiffItem
	for _, rm := range remoteMetas {
		rel := trimTreePrefix(prefix, rm.Path)
		target := filepath.Join(destDir, filepath.FromSlash(rel))
		needWrite := false
		act := DiffAdd
		if st, err := os.Lstat(target); err == nil {
			if st.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("refusing to overwrite symlink %q", target)
			}
			if st.IsDir() {
				return nil, fmt.Errorf("target %q is a directory", target)
			}
			rawLocal, err := os.ReadFile(target)
			if err != nil {
				return nil, err
			}
			h := sha256.Sum256(rawLocal)
			localSum := hex.EncodeToString(h[:])
			localMode := NormalizeMode(st.Mode(), false)
			if localSum != rm.SHA256 {
				needWrite = true
				act = DiffModify
			} else if localMode != rm.Mode {
				needWrite = true
				act = DiffChmod
			}
		} else if os.IsNotExist(err) {
			needWrite = true
		} else {
			return nil, err
		}

		if needWrite {
			plan = append(plan, DiffItem{Path: rm.Path, Action: act, Detail: fmt.Sprintf("-> %s (%04o)", target, rm.Mode)})
			if !dryRun {
				entry, err := m.Get(group, rm.Path)
				if err != nil {
					return nil, err
				}
				raw, err := entry.DecodeBytes()
				if err != nil {
					return nil, err
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return nil, err
				}
				tmp, err := os.CreateTemp(filepath.Dir(target), ".senv-tree-*")
				if err != nil {
					return nil, err
				}
				tmpName := tmp.Name()
				if _, err := tmp.Write(raw); err != nil {
					_ = tmp.Close()
					_ = os.Remove(tmpName)
					return nil, err
				}
				if err := tmp.Chmod(fs.FileMode(entry.Mode)); err != nil {
					_ = tmp.Close()
					_ = os.Remove(tmpName)
					return nil, err
				}
				if err := tmp.Close(); err != nil {
					_ = os.Remove(tmpName)
					return nil, err
				}
				if err := os.Rename(tmpName, target); err != nil {
					_ = os.Remove(tmpName)
					return nil, err
				}
			}
		}
	}

	if deleteLocal {
		if _, err := os.Stat(destDir); err == nil {
			localFiles, err := scanLocalDir(destDir, true)
			if err != nil {
				return nil, err
			}
			for rel := range localFiles {
				if _, ok := remoteRel[rel]; !ok {
					plan = append(plan, DiffItem{Path: joinTreePrefix(prefix, rel), Action: DiffDelete, Detail: "remove local"})
					if !dryRun {
						_ = os.Remove(filepath.Join(destDir, filepath.FromSlash(rel)))
					}
				}
			}
		}
	}
	return plan, nil
}
