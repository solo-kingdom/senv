package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// FileProbes summarises decryptability of a category of encrypted files.
// Only counts and file names are exposed; no plaintext or key material.
type FileProbes struct {
	OK     int
	Total  int
	Failed []string // file names that could not be decrypted with the given key
}

// ConsistencyReport describes whether a key can decrypt the metadata verifier
// and each category of encrypted data files. It deliberately contains only
// booleans, counts and file names — never plaintext or derived-key bytes.
type ConsistencyReport struct {
	MetadataKeyOK bool
	EnvFiles      FileProbes
	TextFiles     FileProbes
	ConfigFiles   FileProbes
}

// AllOK reports whether the key decrypts the metadata and every data file.
func (r *ConsistencyReport) AllOK() bool {
	return r.MetadataKeyOK &&
		r.EnvFiles.OK == r.EnvFiles.Total &&
		r.TextFiles.OK == r.TextFiles.Total &&
		r.ConfigFiles.OK == r.ConfigFiles.Total
}

// CheckConsistency probes whether the given key can decrypt the metadata
// verifier (metadata.PasswordKey) and every encrypted data file (env, text,
// config). It returns counts and the names of files that fail to decrypt; it
// never returns plaintext or key material.
//
// A key whose length is not crypto.KeySize is treated as "decrypts nothing":
// all files are enumerated and reported as failed, without panicking.
func (m *Manager) CheckConsistency(key []byte) (*ConsistencyReport, error) {
	report := &ConsistencyReport{}

	// Metadata verifier.
	md, err := m.LoadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}
	report.MetadataKeyOK = canDecrypt(key, md.PasswordKey)

	// Env files: env_<group>.json.enc
	envGroups, err := m.ListEnvGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list env groups: %w", err)
	}
	for _, group := range envGroups {
		name := fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix)
		report.EnvFiles.Total++
		if m.probeFile(name, key) {
			report.EnvFiles.OK++
		} else {
			report.EnvFiles.Failed = append(report.EnvFiles.Failed, name)
		}
	}

	// Text files: texts/<group>/<key>.enc
	textGroups, err := m.ListTextGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list text groups: %w", err)
	}
	for _, group := range textGroups {
		keys, err := m.ListTextFiles(group)
		if err != nil {
			return nil, fmt.Errorf("failed to list text group %q: %w", group, err)
		}
		for _, k := range keys {
			rel := filepath.Join(TextDirName, group, k+TextFileSuffix)
			report.TextFiles.Total++
			if m.probeText(group, k, key) {
				report.TextFiles.OK++
			} else {
				report.TextFiles.Failed = append(report.TextFiles.Failed, rel)
			}
		}
	}

	// Config files: <name>.enc, enumerated via the config index.
	idx, err := m.LoadConfigIndex()
	if err == nil && idx != nil {
		for name, cf := range idx.Configs {
			fileName := cf.EncryptedFile
			if fileName == "" {
				fileName = name + ConfigFileSuffix
			}
			report.ConfigFiles.Total++
			if m.probeFile(fileName, key) {
				report.ConfigFiles.OK++
			} else {
				report.ConfigFiles.Failed = append(report.ConfigFiles.Failed, fileName)
			}
		}
	}

	return report, nil
}

// probeFile reports whether the key decrypts the given dataPath file.
func (m *Manager) probeFile(name string, key []byte) bool {
	path := filepath.Join(m.dataPath, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return canDecrypt(key, string(data))
}

// probeText reports whether the key decrypts the given text entry.
func (m *Manager) probeText(group, keyName string, key []byte) bool {
	path := m.textFilePath(group, keyName)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return canDecrypt(key, string(data))
}

// canDecrypt reports whether key decrypts the base64 ciphertext. A wrong-length
// key is treated as "cannot decrypt" rather than erroring, so callers can pass
// arbitrary key material safely.
func canDecrypt(key []byte, ciphertextBase64 string) bool {
	if len(key) != crypto.KeySize {
		return false
	}
	_, err := crypto.Decrypt(key, ciphertextBase64)
	return err == nil
}

// HasOrphanedData reports whether the data directory already contains encrypted
// files (env groups, text entries, or config files) while no metadata.json is
// present. Such a state means the data was encrypted with a key the caller no
// longer has metadata for, so (re-)initializing would silently make it
// undecryptable.
func (m *Manager) HasOrphanedData() bool {
	// If metadata exists, the project is initialized; no orphan condition.
	if m.IsInitialized() {
		return false
	}
	entries, err := os.ReadDir(m.dataPath)
	if err != nil {
		return false // data dir does not exist yet -> fresh project
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name == TextDirName {
				if hasTextFiles(m.dataPath) {
					return true
				}
			}
			continue
		}
		// env_*.json.enc, *.enc config files
		if strings.HasSuffix(name, EnvFileSuffix) || strings.HasSuffix(name, ConfigFileSuffix) {
			return true
		}
	}
	return false
}

// hasTextFiles reports whether the texts/ directory under dataPath contains any
// .enc entry, even nested one level (group dirs).
func hasTextFiles(dataPath string) bool {
	textsDir := filepath.Join(dataPath, TextDirName)
	entries, err := os.ReadDir(textsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			if strings.HasSuffix(e.Name(), TextFileSuffix) {
				return true
			}
			continue
		}
		sub, err := os.ReadDir(filepath.Join(textsDir, e.Name()))
		if err != nil {
			continue
		}
		for _, f := range sub {
			if strings.HasSuffix(f.Name(), TextFileSuffix) {
				return true
			}
		}
	}
	return false
}
