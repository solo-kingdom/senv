package crypto

import (
	"crypto/sha256"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// Iterations is the number of PBKDF2 iterations
	Iterations = 100000
	// SaltSize is the size of the salt in bytes
	SaltSize = 32
)

// DeriveKey derives a 256-bit key from a password using PBKDF2
func DeriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, Iterations, KeySize, sha256.New)
}

// GenerateSalt generates a random salt for key derivation
func GenerateSalt() ([]byte, error) {
	return GenerateRandomBytes(SaltSize)
}
