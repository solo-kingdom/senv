package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/securefs"
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

func publicKeyFileBytes(publicKey string) ([]byte, error) {
	trimmed := strings.TrimSpace(publicKey)
	for _, r := range trimmed {
		if r == '\r' || r == '\n' || r == 0 || r < 0x20 || r == 0x7f {
			return nil, fmt.Errorf("public key must be a single line without control characters")
		}
	}
	return []byte(trimmed + "\n"), nil
}

func writePublicKeyFile(privatePath, publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return nil
	}
	data, err := publicKeyFileBytes(publicKey)
	if err != nil {
		return fmt.Errorf("validate public key for %q: %w", privatePath, err)
	}
	pubPath := publicCompanionPath(privatePath)
	if err := storage.WriteSensitiveFile(pubPath, data, 0o700, publicKeyFilePerm); err != nil {
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

// verifyAndTightenMaterializedKey ensures an existing materialized private key
// is a regular file without symlinks along the path, tightening loose modes to 0600.
func verifyAndTightenMaterializedKey(group, name string) error {
	senvDir, err := SenvDir()
	if err != nil {
		return err
	}
	senvRoot, err := securefs.OpenRoot(senvDir)
	if err != nil {
		return fmt.Errorf("open senv ssh root: %w", err)
	}
	defer senvRoot.Close()
	segs := []string{keysDirName, groupDir(group), name}
	data, mode, err := senvRoot.ReadWithMode(segs...)
	if err != nil {
		return fmt.Errorf("inspect materialized key %q: %w", name, err)
	}
	if mode.Perm()&0o077 != 0 {
		if err := senvRoot.AtomicWrite(segs, data, 0o600); err != nil {
			return fmt.Errorf("tighten materialized key %q permissions: %w", name, err)
		}
	}
	return nil
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
