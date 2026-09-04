package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

type trustedRootOpener func(string) (securefs.TrustedRoot, error)

func defaultTrustedRootOpener(path string) (securefs.TrustedRoot, error) {
	return securefs.OpenRoot(path)
}

func (m *Manager) openConfigRoot() (securefs.TrustedRoot, error) {
	if err := EnsurePrivateDir(m.configPath, 0o700); err != nil {
		return nil, fmt.Errorf("prepare config root: %w", err)
	}
	root, err := m.openRoot(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("open config root: %w", err)
	}
	return root, nil
}

func (m *Manager) openDataRoot() (securefs.TrustedRoot, error) {
	if err := EnsurePrivateDir(m.dataPath, 0o700); err != nil {
		return nil, fmt.Errorf("prepare data root: %w", err)
	}
	root, err := m.openRoot(m.dataPath)
	if err != nil {
		return nil, fmt.Errorf("open data root: %w", err)
	}
	return root, nil
}

func removeManagedFile(root securefs.TrustedRoot, segments ...string) error {
	if err := root.Remove(segments...); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ValidateName applies the portable single-path-segment storage identity rule.
func ValidateName(name string) error {
	return securefs.ValidateSegment(name)
}

func validateEnvIdentity(group, key string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	if err := ValidateName(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	if err := ValidateEnvKey(key); err != nil {
		return err
	}
	return nil
}

func validateTextIdentity(group, key string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if err := ValidateName(key); err != nil {
		return fmt.Errorf("invalid text key %q: %w", key, err)
	}
	return nil
}

func validateConfigName(name string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("invalid config name %q: %w", name, err)
	}
	return nil
}

// hasStructuralIdentityHazard reports whether an identity contains path
// traversal, absolute-path, volume-label, NUL or empty/dot semantics. Such
// identities always fail closed, including read-only paths, because skipping
// them could hide an actual containment attack.
func hasStructuralIdentityHazard(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.ContainsAny(name, "\x00/\\") {
		return true
	}
	if filepath.IsAbs(name) {
		return true
	}
	// Reject Windows drive prefixes on every platform so a vault synchronized
	// from Windows cannot smuggle volume semantics past a Unix host check.
	if len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return true
	}
	return filepath.Clean(name) != name || filepath.Base(name) != name
}

// isNonPortableOnly reports whether name is structurally safe but rejected by
// the portable single-segment rule only because of a non-portable character
// (in practice ":" outside a Windows drive prefix). Legacy records with such
// names can be quarantined on read-only paths and repaired explicitly.
func isNonPortableOnly(name string) bool {
	return !hasStructuralIdentityHazard(name) &&
		strings.ContainsRune(name, ':') &&
		securefs.ValidateSegment(name) != nil
}

func validateConfigGroup(group string) error {
	if group == "" {
		group = ConfigDefaultGroup
	}
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid config group %q: %w", group, err)
	}
	return nil
}

func validateSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("settings are nil")
	}
	if err := ValidateName(settings.DefaultGroup); err != nil {
		return fmt.Errorf("invalid default env group %q: %w", settings.DefaultGroup, err)
	}
	for _, group := range settings.ActiveGroups {
		if err := ValidateName(group); err != nil {
			return fmt.Errorf("invalid active env group %q: %w", group, err)
		}
	}
	return nil
}

func validateEnvGroup(envGroup *EnvGroup) error {
	if envGroup == nil {
		return fmt.Errorf("env group is nil")
	}
	if err := ValidateName(envGroup.Name); err != nil {
		return fmt.Errorf("invalid env group %q: %w", envGroup.Name, err)
	}
	for key := range envGroup.Variables {
		if err := validateEnvIdentity(envGroup.Name, key); err != nil {
			return err
		}
	}
	return nil
}

// ConfigQuarantine describes one legacy index entry that is structurally
// consistent but uses a non-portable identity (typically a name containing
// ":"). Read-only callers may skip these entries and surface the warning;
// mutating callers must fail closed until `senv config repair` resolves them.
type ConfigQuarantine struct {
	// Name is the original index map key.
	Name string
	// Record is the in-memory entry with EncryptedFile normalized to the
	// canonical name-derived file, preserving all metadata for repair.
	Record ConfigFile
	// Reason is a human-readable, plaintext-free classification.
	Reason string
}

// normalizeConfigIndexWithQuarantine validates every identity before any
// indexed file can be read or deleted. Structural hazards fail the whole load;
// structurally consistent but non-portable legacy entries are returned in the
// quarantine slice instead of poisoning read-only callers. Legacy empty
// EncryptedFile values are normalized only in the returned in-memory objects.
func normalizeConfigIndexWithQuarantine(index *ConfigIndex) (*ConfigIndex, []ConfigQuarantine, error) {
	if index == nil || index.Configs == nil {
		return nil, nil, fmt.Errorf("config index has nil configs map")
	}
	normalized := &ConfigIndex{Configs: make(map[string]ConfigFile, len(index.Configs))}
	var quarantined []ConfigQuarantine
	for mapName, config := range index.Configs {
		if hasStructuralIdentityHazard(mapName) {
			return nil, nil, fmt.Errorf("invalid config index map key %q: structural identity hazard", mapName)
		}
		if config.Name != mapName {
			return nil, nil, fmt.Errorf("config index identity mismatch: map key %q does not match Name %q", mapName, config.Name)
		}
		effectiveGroup := config.Group
		if effectiveGroup == "" {
			effectiveGroup = ConfigDefaultGroup
		}
		if hasStructuralIdentityHazard(effectiveGroup) {
			return nil, nil, fmt.Errorf("invalid config index Group for %q: structural identity hazard", mapName)
		}
		if !isNonPortableOnly(effectiveGroup) {
			if err := ValidateName(effectiveGroup); err != nil {
				return nil, nil, fmt.Errorf("invalid config index Group for %q: %w", mapName, err)
			}
		}
		canonical := mapName + ConfigFileSuffix
		if config.EncryptedFile == "" {
			config.EncryptedFile = canonical
		} else {
			if config.EncryptedFile != canonical {
				return nil, nil, fmt.Errorf("config index encrypted file mismatch for %q: got %q, want %q", mapName, config.EncryptedFile, canonical)
			}
			if hasStructuralIdentityHazard(config.EncryptedFile) {
				return nil, nil, fmt.Errorf("invalid config index EncryptedFile for %q: structural identity hazard", mapName)
			}
		}
		// Classify portability last: after the structural checks above, the
		// only remaining rejection cause is a non-portable ":" character.
		if isNonPortableOnly(mapName) || isNonPortableOnly(config.Group) {
			reason := "non-portable legacy name"
			if !isNonPortableOnly(mapName) {
				reason = "non-portable legacy group"
			}
			quarantined = append(quarantined, ConfigQuarantine{Name: mapName, Record: config, Reason: reason})
			continue
		}
		normalized.Configs[mapName] = config
	}
	sort.Slice(quarantined, func(i, j int) bool { return quarantined[i].Name < quarantined[j].Name })
	return normalized, quarantined, nil
}

// normalizeConfigIndex is the fail-closed view used by every mutating and
// rekey path: quarantined legacy entries are treated as validation errors so
// no write can silently drop them from the index.
func normalizeConfigIndex(index *ConfigIndex) (*ConfigIndex, error) {
	normalized, quarantined, err := normalizeConfigIndexWithQuarantine(index)
	if err != nil {
		return nil, err
	}
	if len(quarantined) > 0 {
		return nil, quarantinedIndexError(quarantined)
	}
	return normalized, nil
}

func quarantinedIndexError(quarantined []ConfigQuarantine) error {
	first := quarantined[0].Name
	if len(quarantined) == 1 {
		return fmt.Errorf("config index contains legacy non-portable entry %q: run `senv config repair`", first)
	}
	return fmt.Errorf("config index contains %d legacy non-portable entries (first %q): run `senv config repair`", len(quarantined), first)
}
