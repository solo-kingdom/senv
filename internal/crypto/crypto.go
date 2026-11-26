package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
)

const (
	// KeySize is the size of the encryption key in bytes (256 bits)
	KeySize = 32
	// NonceSize is the size of the GCM nonce in bytes
	NonceSize = 12
	// V2Prefix marks versioned AES-256-GCM envelopes bound with AAD.
	// ":" is outside the standard base64 alphabet, so v2 never collides with v1.
	V2Prefix = "senv:v2:"
)

var (
	ErrInvalidKey           = errors.New("invalid key size")
	ErrInvalidCiphertext    = errors.New("invalid ciphertext")
	ErrDecryptionFailed     = errors.New("decryption failed")
	ErrV1CiphertextRejected = errors.New("legacy v1 ciphertext rejected by require_v2 policy")
)

// Encrypt encrypts plaintext using AES-256-GCM (v1 format, nil AAD).
func Encrypt(key []byte, plaintext []byte) (string, error) {
	return sealGCM(key, plaintext, nil)
}

// EncryptWithAAD encrypts plaintext using the v2 envelope format and binds
// additional authenticated data (AAD) so ciphertext cannot be transplanted
// across entry identities or key epochs.
func EncryptWithAAD(key []byte, plaintext []byte, aad []byte) (string, error) {
	encoded, err := sealGCM(key, plaintext, aad)
	if err != nil {
		return "", err
	}
	return V2Prefix + encoded, nil
}

// IsV2Ciphertext reports whether encoded uses the v2 AAD-bound envelope.
func IsV2Ciphertext(encoded string) bool {
	return strings.HasPrefix(encoded, V2Prefix)
}

// BuildEntryAAD builds the deterministic AAD byte sequence for a vault entry.
func BuildEntryAAD(kind, grp, key string, keyEpoch int) []byte {
	return []byte("senv-v2\x00" + kind + "\x00" + grp + "\x00" + key + "\x00" + strconv.Itoa(keyEpoch))
}

// AEAD caches an AES-256-GCM instance for batch encryption/decryption with a
// single key, avoiding repeated AES key schedule allocations.
type AEAD struct {
	gcm cipher.AEAD
}

// NewAEAD constructs a reusable AES-256-GCM cipher for the given 32-byte key.
func NewAEAD(key []byte) (*AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AEAD{gcm: gcm}, nil
}

// EncryptWithAAD encrypts plaintext using the v2 envelope format with AAD.
func (a *AEAD) EncryptWithAAD(plaintext, aad []byte) (string, error) {
	encoded, err := sealWithGCM(a.gcm, plaintext, aad)
	if err != nil {
		return "", err
	}
	return V2Prefix + encoded, nil
}

// DecryptWithAADStrict decrypts a v2 AAD-bound envelope (verifying aad) or,
// when requireV2 is false, falls back to legacy v1 decryption.
func (a *AEAD) DecryptWithAADStrict(encoded string, aad []byte, requireV2 bool) ([]byte, error) {
	if strings.HasPrefix(encoded, V2Prefix) {
		return openWithGCM(a.gcm, strings.TrimPrefix(encoded, V2Prefix), aad)
	}
	if requireV2 {
		return nil, ErrV1CiphertextRejected
	}
	return openWithGCM(a.gcm, encoded, nil)
}

func sealGCM(key []byte, plaintext []byte, aad []byte) (string, error) {
	aead, err := NewAEAD(key)
	if err != nil {
		return "", err
	}
	return sealWithGCM(aead.gcm, plaintext, aad)
}

func sealWithGCM(gcm cipher.AEAD, plaintext []byte, aad []byte) (string, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal appends the ciphertext to the nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, aad)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts v1 ciphertext using AES-256-GCM.
func Decrypt(key []byte, ciphertextBase64 string) ([]byte, error) {
	return openGCM(key, ciphertextBase64, nil)
}

// DecryptWithAAD decrypts both v2 AAD-bound envelopes (verifying aad) and
// legacy v1 ciphertexts (backward read compatibility).
func DecryptWithAAD(key []byte, encoded string, aad []byte) ([]byte, error) {
	return DecryptWithAADStrict(key, encoded, aad, false)
}

// DecryptWithAADStrict decrypts a v2 AAD-bound envelope (verifying aad) and,
// when requireV2 is false, falls back to legacy v1 decryption. When requireV2
// is true, v1 ciphertexts are rejected with ErrV1CiphertextRejected.
func DecryptWithAADStrict(key []byte, encoded string, aad []byte, requireV2 bool) ([]byte, error) {
	if strings.HasPrefix(encoded, V2Prefix) {
		return openGCM(key, strings.TrimPrefix(encoded, V2Prefix), aad)
	}
	if requireV2 {
		return nil, ErrV1CiphertextRejected
	}
	return openGCM(key, encoded, nil)
}

func openGCM(key []byte, ciphertextBase64 string, aad []byte) ([]byte, error) {
	aead, err := NewAEAD(key)
	if err != nil {
		return nil, err
	}
	return openWithGCM(aead.gcm, ciphertextBase64, aad)
}

func openWithGCM(gcm cipher.AEAD, ciphertextBase64 string, aad []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}

	if len(ciphertext) < NonceSize {
		return nil, ErrInvalidCiphertext
	}

	nonce := ciphertext[:NonceSize]
	ciphertext = ciphertext[NonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// GenerateRandomBytes generates random bytes of the specified size
func GenerateRandomBytes(size int) ([]byte, error) {
	bytes := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return nil, err
	}
	return bytes, nil
}

// HashPassword creates a SHA-256 hash of the password for comparison
func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(hash[:])
}
