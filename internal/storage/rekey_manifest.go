package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

const (
	rekeyManifestVersion = 2
	rekeyManifestFile    = ".senv-rekey-manifest.json"
)

type rekeyManifestEntryKind string

const (
	rekeyManifestEntryEnv    rekeyManifestEntryKind = "env"
	rekeyManifestEntryText   rekeyManifestEntryKind = "text"
	rekeyManifestEntryConfig rekeyManifestEntryKind = "config"
)

type rekeyStage string

const (
	rekeyStagePrepare        rekeyStage = "PREPARE"
	rekeyStageSwitchData     rekeyStage = "SWITCH_DATA"
	rekeyStageSwitchMetadata rekeyStage = "SWITCH_METADATA"
	rekeyStageCommitted      rekeyStage = "COMMITTED"
	rekeyStageCleanup        rekeyStage = "CLEANUP"
)

type rekeyManifestEntry struct {
	Kind     rekeyManifestEntryKind `json:"kind"`
	Identity []string               `json:"identity"`
	OldHash  string                 `json:"old_hash"`
	NewHash  string                 `json:"new_hash"`
}

type rekeyManifest struct {
	Version         int                  `json:"version"`
	TransactionID   string               `json:"transaction_id"`
	Stage           rekeyStage           `json:"stage"`
	ConfigRootHash  string               `json:"config_root_hash"`
	DataRootHash    string               `json:"data_root_hash"`
	OldMetadataHash string               `json:"old_metadata_hash"`
	NewMetadataHash string               `json:"new_metadata_hash"`
	Entries         []rekeyManifestEntry `json:"entries"`
}

var (
	rekeyTransactionIDRE = regexp.MustCompile(`^[a-f0-9]{32}$`)
	rekeyHashRE          = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func rootFingerprint(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return contentHash([]byte(filepath.Clean(abs))), nil
}

func (m *Manager) newManifest(tx string, oldMetadata, newMetadata []byte, entries []rekeyManifestEntry) (*rekeyManifest, error) {
	configHash, err := rootFingerprint(m.configPath)
	if err != nil {
		return nil, err
	}
	dataHash, err := rootFingerprint(m.dataPath)
	if err != nil {
		return nil, err
	}
	manifest := &rekeyManifest{
		Version:         rekeyManifestVersion,
		TransactionID:   tx,
		Stage:           rekeyStagePrepare,
		ConfigRootHash:  configHash,
		DataRootHash:    dataHash,
		OldMetadataHash: contentHash(oldMetadata),
		NewMetadataHash: contentHash(newMetadata),
		Entries:         entries,
	}
	if err := m.validateManifest(manifest); err != nil {
		return nil, err
	}
	if err := m.validateManifestIdentities(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

// validateManifestIdentities binds recovery entries to the vault data model,
// not merely to syntactically safe path segments. This keeps journals from
// targeting state, locks, sidecars, or arbitrary files under the data root.
func (m *Manager) validateManifestIdentities(manifest *rekeyManifest) error {
	configRoot, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return fmt.Errorf("open config root for rekey identities: %w", err)
	}
	defer configRoot.Close()
	indexData, err := configRoot.Read(ConfigIndexFile)
	if err != nil {
		return fmt.Errorf("read config index for rekey identities: %w", err)
	}
	var parsedIndex ConfigIndex
	if err := FromJSON(indexData, &parsedIndex); err != nil {
		return fmt.Errorf("parse config index for rekey identities: %w", err)
	}
	index, err := normalizeConfigIndex(&parsedIndex)
	if err != nil {
		return fmt.Errorf("validate config index for rekey identities: %w", err)
	}
	expectedConfigs := make(map[string]bool, len(index.Configs))
	for mapName, config := range index.Configs {
		if config.Name != mapName {
			return fmt.Errorf("config index identity mismatch for %q", mapName)
		}
		expectedConfigs[config.EncryptedFile] = false
	}
	for _, entry := range manifest.Entries {
		kind, err := classifyRekeyEntry(entry.Identity, expectedConfigs)
		if err != nil {
			return fmt.Errorf("invalid rekey entry identity %q: %w", strings.Join(entry.Identity, "/"), err)
		}
		if rekeyManifestKind(kind) != entry.Kind {
			return fmt.Errorf("rekey entry kind mismatch for %q", strings.Join(entry.Identity, "/"))
		}
	}
	return nil
}

func rekeyManifestKind(kind rekeyEntryKind) rekeyManifestEntryKind {
	switch kind {
	case rekeyEntryEnv:
		return rekeyManifestEntryEnv
	case rekeyEntryText:
		return rekeyManifestEntryText
	case rekeyEntryConfig:
		return rekeyManifestEntryConfig
	default:
		panic("unknown rekey entry kind")
	}
}

func (m *Manager) validateManifest(manifest *rekeyManifest) error {
	if manifest == nil {
		return fmt.Errorf("rekey manifest is nil")
	}
	if manifest.Version != rekeyManifestVersion {
		return fmt.Errorf("unsupported rekey manifest version %d", manifest.Version)
	}
	if !rekeyTransactionIDRE.MatchString(manifest.TransactionID) {
		return fmt.Errorf("invalid rekey transaction identity")
	}
	switch manifest.Stage {
	case rekeyStagePrepare, rekeyStageSwitchData, rekeyStageSwitchMetadata, rekeyStageCommitted, rekeyStageCleanup:
	default:
		return fmt.Errorf("invalid rekey stage %q", manifest.Stage)
	}
	configHash, err := rootFingerprint(m.configPath)
	if err != nil {
		return err
	}
	dataHash, err := rootFingerprint(m.dataPath)
	if err != nil {
		return err
	}
	if manifest.ConfigRootHash != configHash || manifest.DataRootHash != dataHash {
		return fmt.Errorf("rekey manifest belongs to different vault roots")
	}
	if !rekeyHashRE.MatchString(manifest.OldMetadataHash) || !rekeyHashRE.MatchString(manifest.NewMetadataHash) || manifest.OldMetadataHash == manifest.NewMetadataHash {
		return fmt.Errorf("invalid rekey metadata hashes")
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		switch entry.Kind {
		case rekeyManifestEntryEnv, rekeyManifestEntryText, rekeyManifestEntryConfig:
		default:
			return fmt.Errorf("invalid rekey entry kind %q", entry.Kind)
		}
		if len(entry.Identity) == 0 {
			return fmt.Errorf("empty rekey entry identity")
		}
		for _, segment := range entry.Identity {
			if err := securefs.ValidateSegment(segment); err != nil {
				return fmt.Errorf("invalid rekey entry identity: %w", err)
			}
		}
		identity := strings.Join(entry.Identity, "/")
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("duplicate rekey entry identity %q", identity)
		}
		seen[identity] = struct{}{}
		if !rekeyHashRE.MatchString(entry.OldHash) || !rekeyHashRE.MatchString(entry.NewHash) || entry.OldHash == entry.NewHash {
			return fmt.Errorf("invalid hashes for rekey entry %q", identity)
		}
	}
	return nil
}

func (m *Manager) saveRekeyManifest(manifest *rekeyManifest) error {
	if err := m.validateManifest(manifest); err != nil {
		return err
	}
	if err := m.validateManifestIdentities(manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	root, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return fmt.Errorf("open config root for rekey manifest: %w", err)
	}
	defer root.Close()
	if err := root.AtomicWrite([]string{rekeyManifestFile}, data, 0o600); err != nil {
		return fmt.Errorf("write rekey manifest: %w", err)
	}
	return nil
}

func (m *Manager) loadRekeyManifest() (*rekeyManifest, error) {
	root, err := securefs.OpenRoot(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("open config root for rekey manifest: %w", err)
	}
	defer root.Close()
	data, err := root.Read(rekeyManifestFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rekey manifest: %w", err)
	}
	var manifest rekeyManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse rekey manifest: %w", err)
	}
	if err := m.validateManifest(&manifest); err != nil {
		return nil, err
	}
	if err := m.validateManifestIdentities(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func rekeySidecar(identity []string, tx string, suffix string) []string {
	result := append([]string(nil), identity...)
	last := len(result) - 1
	result[last] = result[last] + ".rekey-" + tx + suffix
	return result
}

func rekeyMetadataSidecar(tx string) []string {
	return []string{MetadataFile + ".rekey-" + tx + ".new"}
}
