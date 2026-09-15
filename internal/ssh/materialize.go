package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/storage"
)

const publicKeyFilePerm os.FileMode = 0o644

// publicCompanionPath is the OpenSSH-style sibling of a materialized private
// key: <private-path>.pub.
func publicCompanionPath(privatePath string) string {
	return privatePath + ".pub"
}

func isPublicCompanionName(name string) bool {
	return strings.HasSuffix(name, ".pub")
}

func vaultNameForMaterializedFile(name string) string {
	if isPublicCompanionName(name) {
		return strings.TrimSuffix(name, ".pub")
	}
	return name
}

func markMaterializedPaths(referenced map[string]bool, privatePath string) {
	referenced[privatePath] = true
	referenced[publicCompanionPath(privatePath)] = true
}

// resolvedPublicKey prefers a live derivation from the private key so older
// vaults still emit a complete authorized-key line; encrypted keys stay empty.
func resolvedPublicKey(entry *storage.KeyPairEntry) string {
	if entry == nil {
		return ""
	}
	if entry.PrivateKey != "" {
		if pub, _, _, err := derivePublicKey([]byte(entry.PrivateKey)); err == nil && pub != "" {
			return pub
		}
	}
	return strings.TrimSpace(entry.PublicKey)
}

func publicKeyFileBytes(publicKey string) []byte {
	return []byte(strings.TrimSpace(publicKey) + "\n")
}

func writePublicKeyFile(privatePath, publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return nil
	}
	pubPath := publicCompanionPath(privatePath)
	if err := storage.WriteSensitiveFile(pubPath, publicKeyFileBytes(publicKey), 0o700, publicKeyFilePerm); err != nil {
		return fmt.Errorf("write public key %q: %w", pubPath, err)
	}
	return nil
}

// writeMaterializedPair writes the private key (0600) and, when known, the
// sibling .pub (0644) next to it.
func writeMaterializedPair(target, name, privateKey, publicKey string) error {
	if err := storage.EnsurePrivateDir(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := storage.WriteSensitiveFile(target, []byte(privateKey), 0o700, 0o600); err != nil {
		return fmt.Errorf("materialize keypair %q: %w", name, err)
	}
	return writePublicKeyFile(target, publicKey)
}

// ensurePublicKeyFile writes a missing sibling .pub without touching an
// existing private key. Existing .pub files are left as-is (host apply skip).
func ensurePublicKeyFile(privatePath, publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return nil
	}
	pubPath := publicCompanionPath(privatePath)
	if _, err := os.Lstat(pubPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect public key %q: %w", pubPath, err)
	}
	return writePublicKeyFile(privatePath, publicKey)
}
