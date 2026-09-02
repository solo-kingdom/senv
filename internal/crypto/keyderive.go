package crypto

import (
	"crypto/sha256"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// DefaultIterations is the PBKDF2 iteration count used for newly created
	// vaults and rekeys (OWASP 2026 recommendation for PBKDF2-SHA256).
	DefaultIterations = 600000
	// LegacyIterations is the iteration count used before KDF parameters were
	// versioned. Metadata without a kdf_iterations field MUST be interpreted
	// as this value.
	LegacyIterations = 100000
	// SaltSize is the size of the salt in bytes
	SaltSize = 32
)

// DeriveKey derives a 256-bit key from a password using PBKDF2 with the
// legacy iteration count. Only meaningful for metadata that predates KDF
// parameter versioning; prefer DeriveKeyWithIterations with the iteration
// count recorded in metadata.
func DeriveKey(password string, salt []byte) []byte {
	return DeriveKeyWithIterations(password, salt, LegacyIterations)
}

// DeriveKeyWithIterations derives a 256-bit key from a password using PBKDF2
// with an explicit iteration count.
func DeriveKeyWithIterations(password string, salt []byte, iterations int) []byte {
	return pbkdf2.Key([]byte(password), salt, iterations, KeySize, sha256.New)
}

// GenerateSalt generates a random salt for key derivation
func GenerateSalt() ([]byte, error) {
	return GenerateRandomBytes(SaltSize)
}
