package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
)

// RekeyResult reports how many files were re-encrypted.
type RekeyResult struct {
	EnvFiles    int
	TextFiles   int
	ConfigFiles int
}

func (r *RekeyResult) Total() int { return r.EnvFiles + r.TextFiles + r.ConfigFiles }

type rekeyEntryKind uint8

const (
	rekeyEntryEnv rekeyEntryKind = iota + 1
	rekeyEntryText
	rekeyEntryConfig
)

type rekeyEntry struct {
	identity   []string
	kind       rekeyEntryKind
	plaintext  []byte
	oldContent []byte
	newContent []byte
}

type rekeyCheckpoint string

const (
	rekeyCheckpointPrepare        rekeyCheckpoint = "PREPARE"
	rekeyCheckpointSwitchData     rekeyCheckpoint = "SWITCH_DATA"
	rekeyCheckpointDataEntry      rekeyCheckpoint = "SWITCH_DATA_ENTRY"
	rekeyCheckpointSwitchMetadata rekeyCheckpoint = "SWITCH_METADATA"
	rekeyCheckpointMetadata       rekeyCheckpoint = "METADATA_SWITCHED"
	rekeyCheckpointCommitted      rekeyCheckpoint = "COMMITTED"
	rekeyCheckpointCleanup        rekeyCheckpoint = "CLEANUP"
)

// rekeyHooks is a package-private test seam. Production managers leave it nil;
// crash and fault behavior lives only in _test.go callbacks.
type rekeyHooks struct {
	before     func(operation string, index int) error
	checkpoint func(rekeyCheckpoint)
	encrypt    func(key, plaintext []byte) (string, error)
}

func (m *Manager) rekeyBefore(operation string, index int) error {
	if m.rekeyHooks != nil && m.rekeyHooks.before != nil {
		return m.rekeyHooks.before(operation, index)
	}
	return nil
}

func (m *Manager) rekeyCheckpoint(point rekeyCheckpoint) {
	if m.rekeyHooks != nil && m.rekeyHooks.checkpoint != nil {
		m.rekeyHooks.checkpoint(point)
	}
}

// Rekey re-encrypts every managed ciphertext and changes metadata through a
// durable journal. Any returned error is followed by a best-effort deterministic
// recovery; if recovery itself fails the journal is retained and future access
// fails closed.
func (m *Manager) Rekey(oldKey, newKey []byte, newSaltB64, newPasswordKey string, newIterations int) (*RekeyResult, error) {
	var result *RekeyResult
	err := m.WithVaultMutation(func(locked *Manager) error {
		var err error
		result, err = locked.rekeyLocked(oldKey, newKey, newSaltB64, newPasswordKey, newIterations)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) rekeyLocked(oldKey, newKey []byte, newSaltB64, newPasswordKey string, newIterations int) (_ *RekeyResult, retErr error) {
	currentMetadata, err := m.LoadMetadata()
	if err != nil {
		return nil, fmt.Errorf("validate current metadata: %w", err)
	}
	targetMetadata := *currentMetadata
	targetMetadata.KDFIterations = newIterations
	if _, err := targetMetadata.ValidatedKDFIterations(); err != nil {
		return nil, fmt.Errorf("validate target metadata: %w", err)
	}

	entries, result, oldMetadata, err := m.rekeyPreflight(oldKey)
	if err != nil {
		return nil, fmt.Errorf("pre-flight decryption failed: %w", err)
	}

	encrypt := crypto.Encrypt
	if m.rekeyHooks != nil && m.rekeyHooks.encrypt != nil {
		encrypt = m.rekeyHooks.encrypt
	}
	for i := range entries {
		if err := m.rekeyBefore("encrypt", i); err != nil {
			return nil, fmt.Errorf("prepare encryption: %w", err)
		}
		ciphertext, err := encrypt(newKey, entries[i].plaintext)
		if err != nil {
			return nil, fmt.Errorf("encrypt entry %q: %w", strings.Join(entries[i].identity, "/"), err)
		}
		entries[i].newContent = []byte(ciphertext)
	}

	var metadata Metadata
	if err := FromJSON(oldMetadata, &metadata); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	metadata.Salt = newSaltB64
	metadata.PasswordKey = newPasswordKey
	metadata.KDFIterations = newIterations
	metadata.UpdatedAt = time.Now()
	newMetadata, err := ToJSON(&metadata)
	if err != nil {
		return nil, fmt.Errorf("serialize new metadata: %w", err)
	}

	tx, err := newRekeyTransactionID()
	if err != nil {
		return nil, err
	}
	manifestEntries := make([]rekeyManifestEntry, len(entries))
	for i, entry := range entries {
		manifestEntries[i] = rekeyManifestEntry{
			Kind:     rekeyManifestKind(entry.kind),
			Identity: append([]string(nil), entry.identity...),
			OldHash:  contentHash(entry.oldContent),
			NewHash:  contentHash(entry.newContent),
		}
	}
	manifest, err := m.newManifest(tx, oldMetadata, newMetadata, manifestEntries)
	if err != nil {
		return nil, err
	}

	journalStarted := false
	defer func() {
		if retErr == nil || !journalStarted {
			return
		}
		if recoverErr := m.recoverRekeyLocked(); recoverErr != nil {
			retErr = fmt.Errorf("%v; recovery failed: %w", retErr, recoverErr)
		}
	}()

	if err := m.rekeyBefore("manifest_PREPARE", 0); err != nil {
		return nil, err
	}
	if err := m.saveRekeyManifest(manifest); err != nil {
		return nil, err
	}
	journalStarted = true
	m.rekeyCheckpoint(rekeyCheckpointPrepare)

	configRoot, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return nil, err
	}
	defer configRoot.Close()
	dataRoot, err := securefs.OpenRoot(m.dataPath)
	if err != nil {
		return nil, err
	}
	defer dataRoot.Close()

	if err := m.rekeyBefore("write_metadata_new", 0); err != nil {
		return nil, err
	}
	if err := configRoot.AtomicWrite(rekeyMetadataSidecar(tx), newMetadata, 0o600); err != nil {
		return nil, fmt.Errorf("prepare metadata generation: %w", err)
	}
	for i, entry := range entries {
		if err := m.rekeyBefore("write_new", i); err != nil {
			return nil, err
		}
		if err := dataRoot.AtomicWrite(rekeySidecar(entry.identity, tx, ".new"), entry.newContent, 0o600); err != nil {
			return nil, fmt.Errorf("prepare entry %q: %w", strings.Join(entry.identity, "/"), err)
		}
		// These package-private checkpoints let tests model an error reported by
		// the durable file/parent sync boundary without any production control.
		if err := m.rekeyBefore("new_file_fsync", i); err != nil {
			return nil, err
		}
		if err := m.rekeyBefore("new_dir_fsync", i); err != nil {
			return nil, err
		}
	}

	manifest.Stage = rekeyStageSwitchData
	if err := m.rekeyBefore("manifest_SWITCH_DATA", 0); err != nil {
		return nil, err
	}
	if err := m.saveRekeyManifest(manifest); err != nil {
		return nil, err
	}
	m.rekeyCheckpoint(rekeyCheckpointSwitchData)

	for i, entry := range entries {
		oldSidecar := rekeySidecar(entry.identity, tx, ".old")
		newSidecar := rekeySidecar(entry.identity, tx, ".new")
		if err := m.rekeyBefore("rename_old", i); err != nil {
			return nil, err
		}
		if err := dataRoot.Rename(entry.identity, oldSidecar); err != nil {
			return nil, fmt.Errorf("preserve old entry %q: %w", strings.Join(entry.identity, "/"), err)
		}
		if err := m.rekeyBefore("rename_new", i); err != nil {
			return nil, err
		}
		if err := dataRoot.Rename(newSidecar, entry.identity); err != nil {
			return nil, fmt.Errorf("activate new entry %q: %w", strings.Join(entry.identity, "/"), err)
		}
		m.rekeyCheckpoint(rekeyCheckpointDataEntry)
	}

	manifest.Stage = rekeyStageSwitchMetadata
	if err := m.rekeyBefore("manifest_SWITCH_METADATA", 0); err != nil {
		return nil, err
	}
	if err := m.saveRekeyManifest(manifest); err != nil {
		return nil, err
	}
	m.rekeyCheckpoint(rekeyCheckpointSwitchMetadata)

	if err := m.rekeyBefore("metadata_write", 0); err != nil {
		return nil, err
	}
	if err := configRoot.AtomicWrite([]string{MetadataFile}, newMetadata, 0o600); err != nil {
		return nil, fmt.Errorf("switch metadata: %w", err)
	}
	if err := m.rekeyBefore("metadata_file_fsync", 0); err != nil {
		return nil, err
	}
	if err := m.rekeyBefore("metadata_dir_fsync", 0); err != nil {
		return nil, err
	}
	m.rekeyCheckpoint(rekeyCheckpointMetadata)

	manifest.Stage = rekeyStageCommitted
	if err := m.rekeyBefore("manifest_COMMITTED", 0); err != nil {
		return nil, err
	}
	if err := m.saveRekeyManifest(manifest); err != nil {
		return nil, err
	}
	m.rekeyCheckpoint(rekeyCheckpointCommitted)

	manifest.Stage = rekeyStageCleanup
	if err := m.rekeyBefore("manifest_CLEANUP", 0); err != nil {
		return nil, err
	}
	if err := m.saveRekeyManifest(manifest); err != nil {
		return nil, err
	}
	m.rekeyCheckpoint(rekeyCheckpointCleanup)
	if err := m.cleanupRekeyLocked(manifest, dataRoot, configRoot); err != nil {
		return nil, err
	}
	return result, nil
}

func newRekeyTransactionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate rekey transaction identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (m *Manager) rekeyPreflight(oldKey []byte) ([]rekeyEntry, *RekeyResult, []byte, error) {
	configRoot, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open config root: %w", err)
	}
	defer configRoot.Close()
	dataRoot, err := securefs.OpenRoot(m.dataPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open data root: %w", err)
	}
	defer dataRoot.Close()

	oldMetadata, err := configRoot.Read(MetadataFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read metadata: %w", err)
	}
	indexData, err := configRoot.Read(ConfigIndexFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read config index: %w", err)
	}
	var parsedIndex ConfigIndex
	if err := FromJSON(indexData, &parsedIndex); err != nil {
		return nil, nil, nil, fmt.Errorf("parse config index: %w", err)
	}
	index, err := normalizeConfigIndex(&parsedIndex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("validate config index: %w", err)
	}
	expectedConfigs := make(map[string]bool, len(index.Configs))
	for mapName, config := range index.Configs {
		expectedConfigs[config.EncryptedFile] = false
		if config.Name != mapName {
			return nil, nil, nil, fmt.Errorf("config index identity mismatch for %q", mapName)
		}
	}

	var entries []rekeyEntry
	result := &RekeyResult{}
	walkErr := filepath.WalkDir(m.dataPath, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == m.dataPath {
			return nil
		}
		rel, err := filepath.Rel(m.dataPath, path)
		if err != nil {
			return err
		}
		segments := strings.Split(rel, string(filepath.Separator))
		for _, segment := range segments {
			if err := securefs.ValidateSegment(segment); err != nil {
				return fmt.Errorf("invalid data identity %q: %w", filepath.ToSlash(rel), err)
			}
		}
		if dirEntry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in managed data at %q", filepath.ToSlash(rel))
		}
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".enc") {
			return nil
		}
		kind, err := classifyRekeyEntry(segments, expectedConfigs)
		if err != nil {
			return err
		}
		ciphertext, err := dataRoot.Read(segments...)
		if err != nil {
			return err
		}
		plaintext, err := crypto.Decrypt(oldKey, string(ciphertext))
		if err != nil {
			return fmt.Errorf("decrypt %q: %w", filepath.ToSlash(rel), err)
		}
		entries = append(entries, rekeyEntry{identity: append([]string(nil), segments...), kind: kind, plaintext: plaintext, oldContent: ciphertext})
		switch kind {
		case rekeyEntryEnv:
			result.EnvFiles++
		case rekeyEntryText:
			result.TextFiles++
		case rekeyEntryConfig:
			result.ConfigFiles++
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, nil, walkErr
	}
	for name, seen := range expectedConfigs {
		if !seen {
			return nil, nil, nil, fmt.Errorf("config index references missing encrypted file %q", name)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.Join(entries[i].identity, "\x00") < strings.Join(entries[j].identity, "\x00")
	})
	return entries, result, oldMetadata, nil
}

func classifyRekeyEntry(segments []string, expectedConfigs map[string]bool) (rekeyEntryKind, error) {
	switch {
	case len(segments) == 1 && strings.HasPrefix(segments[0], EnvFilePrefix) && strings.HasSuffix(segments[0], EnvFileSuffix):
		group := strings.TrimSuffix(strings.TrimPrefix(segments[0], EnvFilePrefix), EnvFileSuffix)
		if err := securefs.ValidateSegment(group); err != nil {
			return 0, fmt.Errorf("invalid legacy env group: %w", err)
		}
		return rekeyEntryEnv, nil
	case len(segments) == 1:
		seen, expected := expectedConfigs[segments[0]]
		if !expected {
			return 0, fmt.Errorf("unindexed encrypted file %q", segments[0])
		}
		if seen {
			return 0, fmt.Errorf("duplicate config encrypted file %q", segments[0])
		}
		expectedConfigs[segments[0]] = true
		return rekeyEntryConfig, nil
	case len(segments) == 3 && segments[0] == EnvDirName:
		if err := securefs.ValidateSegment(segments[1]); err != nil {
			return 0, fmt.Errorf("invalid env group: %w", err)
		}
		if segments[2] != EnvMetaFileName {
			key := strings.TrimSuffix(segments[2], EnvVarSuffix)
			if key == segments[2] || ValidateEnvKey(key) != nil {
				return 0, fmt.Errorf("invalid env entry identity %q", strings.Join(segments, "/"))
			}
		}
		return rekeyEntryEnv, nil
	case len(segments) == 3 && segments[0] == TextDirName:
		if err := securefs.ValidateSegment(segments[1]); err != nil {
			return 0, fmt.Errorf("invalid text group: %w", err)
		}
		key := strings.TrimSuffix(segments[2], TextFileSuffix)
		if key == segments[2] {
			return 0, fmt.Errorf("invalid text entry identity %q", strings.Join(segments, "/"))
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return 0, fmt.Errorf("invalid text key: %w", err)
		}
		return rekeyEntryText, nil
	default:
		return 0, fmt.Errorf("encrypted file has unknown managed identity %q", strings.Join(segments, "/"))
	}
}

func (m *Manager) recoverRekeyLocked() error {
	manifest, err := m.loadRekeyManifest()
	if err != nil {
		return fmt.Errorf("%w: %v; run senv doctor with a newer senv version", ErrRekeyRecoveryRequired, err)
	}
	if manifest == nil {
		return nil
	}
	configRoot, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRekeyRecoveryRequired, err)
	}
	defer configRoot.Close()
	dataRoot, err := securefs.OpenRoot(m.dataPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRekeyRecoveryRequired, err)
	}
	defer dataRoot.Close()
	metadata, err := configRoot.Read(MetadataFile)
	if err != nil {
		return fmt.Errorf("%w: read metadata: %v", ErrRekeyRecoveryRequired, err)
	}
	switch contentHash(metadata) {
	case manifest.OldMetadataHash:
		if err := recoverEntries(dataRoot, manifest, false); err != nil {
			return fmt.Errorf("%w: rollback: %v", ErrRekeyRecoveryRequired, err)
		}
	case manifest.NewMetadataHash:
		if err := recoverEntries(dataRoot, manifest, true); err != nil {
			return fmt.Errorf("%w: roll forward: %v", ErrRekeyRecoveryRequired, err)
		}
	default:
		return fmt.Errorf("%w: metadata hash matches neither transaction generation", ErrRekeyRecoveryRequired)
	}
	if err := m.cleanupRekeyLocked(manifest, dataRoot, configRoot); err != nil {
		return fmt.Errorf("%w: cleanup: %v", ErrRekeyRecoveryRequired, err)
	}
	return nil
}

type recoveryAction struct {
	identity []string
	source   []string
}

func recoverEntries(root *securefs.Root, manifest *rekeyManifest, rollForward bool) error {
	actions := make([]recoveryAction, 0)
	for _, entry := range manifest.Entries {
		want := entry.OldHash
		source := rekeySidecar(entry.Identity, manifest.TransactionID, ".old")
		if rollForward {
			want = entry.NewHash
			source = rekeySidecar(entry.Identity, manifest.TransactionID, ".new")
		}
		original, originalExists, err := readOptional(root, entry.Identity)
		if err != nil {
			return err
		}
		if originalExists && contentHash(original) == want {
			continue
		}
		generation, exists, err := readOptional(root, source)
		if err != nil {
			return err
		}
		if !exists || contentHash(generation) != want {
			return fmt.Errorf("entry %q has no verified %s generation", strings.Join(entry.Identity, "/"), map[bool]string{false: "old", true: "new"}[rollForward])
		}
		actions = append(actions, recoveryAction{identity: entry.Identity, source: source})
	}
	for _, action := range actions {
		if _, exists, err := readOptional(root, action.identity); err != nil {
			return err
		} else if exists {
			if err := root.Remove(action.identity...); err != nil {
				return err
			}
		}
		if err := root.Rename(action.source, action.identity); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) cleanupRekeyLocked(manifest *rekeyManifest, dataRoot, configRoot *securefs.Root) error {
	for i, entry := range manifest.Entries {
		for _, suffix := range []string{".old", ".new"} {
			if err := m.rekeyBefore("cleanup_remove", i); err != nil {
				return err
			}
			if err := removeOptional(dataRoot, rekeySidecar(entry.Identity, manifest.TransactionID, suffix)); err != nil {
				return err
			}
		}
	}
	if err := removeOptional(configRoot, rekeyMetadataSidecar(manifest.TransactionID)); err != nil {
		return err
	}
	if err := removeOptional(configRoot, []string{rekeyManifestFile}); err != nil {
		return err
	}
	return nil
}

func readOptional(root *securefs.Root, segments []string) ([]byte, bool, error) {
	data, err := root.Read(segments...)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func removeOptional(root *securefs.Root, segments []string) error {
	err := root.Remove(segments...)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func isEncFile(name string) bool { return strings.HasSuffix(name, ".enc") }
