package storage

import (
	"errors"
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
	// QuarantinedConfigNames lists legacy config entries whose identities are
	// structurally consistent but non-portable. They are skipped (not probed,
	// not counted) and surfaced separately as repair guidance.
	QuarantinedConfigNames []string
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
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*ConsistencyReport, error) {
			return locked.CheckConsistency(key)
		})
	}
	report := &ConsistencyReport{}

	md, err := m.LoadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}
	report.MetadataKeyOK = canDecrypt(key, md.PasswordKey)

	dataRoot, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer dataRoot.Close()

	envGroups, err := m.ListEnvGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list env groups: %w", err)
	}
	for _, group := range envGroups {
		files, readErr := dataRoot.ReadDir(EnvDirName, group)
		if readErr == nil {
			for _, file := range files {
				if file.IsDir || !isEncFile(file.Name) {
					return nil, fmt.Errorf("invalid managed env entry %q", filepath.Join(EnvDirName, group, file.Name))
				}
				rel := filepath.Join(EnvDirName, group, file.Name)
				ciphertext, err := dataRoot.Read(EnvDirName, group, file.Name)
				if err != nil {
					return nil, err
				}
				report.EnvFiles.Total++
				if canDecrypt(key, string(ciphertext)) {
					report.EnvFiles.OK++
				} else {
					report.EnvFiles.Failed = append(report.EnvFiles.Failed, rel)
				}
			}
			continue
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		name := fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix)
		ciphertext, err := dataRoot.Read(name)
		if err != nil {
			return nil, err
		}
		report.EnvFiles.Total++
		if canDecrypt(key, string(ciphertext)) {
			report.EnvFiles.OK++
		} else {
			report.EnvFiles.Failed = append(report.EnvFiles.Failed, name)
		}
	}

	textGroups, err := m.ListTextGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list text groups: %w", err)
	}
	for _, group := range textGroups {
		keys, err := m.ListTextFiles(group)
		if err != nil {
			return nil, fmt.Errorf("failed to list text group %q: %w", group, err)
		}
		for _, name := range keys {
			rel := filepath.Join(TextDirName, group, name+TextFileSuffix)
			ciphertext, err := dataRoot.Read(TextDirName, group, name+TextFileSuffix)
			if err != nil {
				return nil, err
			}
			report.TextFiles.Total++
			if canDecrypt(key, string(ciphertext)) {
				report.TextFiles.OK++
			} else {
				report.TextFiles.Failed = append(report.TextFiles.Failed, rel)
			}
		}
	}

	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}
	for _, q := range quarantined {
		report.QuarantinedConfigNames = append(report.QuarantinedConfigNames, q.Name)
	}
	for name, config := range index.Configs {
		fileName := config.EncryptedFile
		if fileName == "" {
			fileName = name + ConfigFileSuffix
		}
		ciphertext, err := dataRoot.Read(fileName)
		if err != nil {
			return nil, err
		}
		report.ConfigFiles.Total++
		if canDecrypt(key, string(ciphertext)) {
			report.ConfigFiles.OK++
		} else {
			report.ConfigFiles.Failed = append(report.ConfigFiles.Failed, fileName)
		}
	}

	return report, nil
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
			if name == EnvDirName {
				if hasEnvVarFiles(m.dataPath) {
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

// hasEnvVarFiles reports whether the envs/ directory under dataPath contains any
// .enc entry in group subdirectories.
func hasEnvVarFiles(dataPath string) bool {
	envsDir := filepath.Join(dataPath, EnvDirName)
	groups, err := os.ReadDir(envsDir)
	if err != nil {
		return false
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(envsDir, g.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && isEncFile(f.Name()) {
				return true
			}
		}
	}
	return false
}
