package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	password := "test-password-123"
	salt := GenerateSaltFixed()

	key := DeriveKey(password, salt)

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}

	// Same password and salt should produce same key
	key2 := DeriveKey(password, salt)
	if !bytes.Equal(key, key2) {
		t.Error("Same password and salt should produce same key")
	}

	// Different password should produce different key
	key3 := DeriveKey("different-password", salt)
	if bytes.Equal(key, key3) {
		t.Error("Different passwords should produce different keys")
	}

	// Different salt should produce different key
	salt2 := GenerateSaltFixed()
	key4 := DeriveKey(password, salt2)
	if bytes.Equal(key, key4) {
		t.Error("Different salts should produce different keys")
	}
}

func TestGenerateSalt(t *testing.T) {
	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}

	if len(salt1) != SaltSize {
		t.Errorf("Expected salt length %d, got %d", SaltSize, len(salt1))
	}

	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}

	if bytes.Equal(salt1, salt2) {
		t.Error("Two generated salts should be different")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte("")},
		{"short", []byte("hello")},
		{"medium", []byte("this is a medium length plaintext for testing")},
		{"long", make([]byte, 10000)},
		{"binary", []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}},
		{"unicode", []byte("你好世界 🌍")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := Encrypt(key, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			if ciphertext == "" {
				t.Error("Ciphertext should not be empty")
			}

			decrypted, err := Decrypt(key, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if !bytes.Equal(decrypted, tc.plaintext) {
				t.Errorf("Decrypted data doesn't match\nExpected: %v\nGot: %v", tc.plaintext, decrypted)
			}
		})
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	if _, err := rand.Read(key1); err != nil {
		t.Fatalf("Failed to generate key1: %v", err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatalf("Failed to generate key2: %v", err)
	}

	plaintext := []byte("secret message")
	ciphertext, err := Encrypt(key1, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(key2, ciphertext)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestDecryptWithInvalidCiphertext(t *testing.T) {
	key := make([]byte, 32)

	testCases := []struct {
		name       string
		ciphertext string
	}{
		{"empty", ""},
		{"invalid-base64", "not-valid-base64!!!"},
		{"too-short", "YWJj"}, // "abc" in base64
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decrypt(key, tc.ciphertext)
			if err == nil {
				t.Error("Decrypt with invalid ciphertext should fail")
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	password := "my-password"

	hash1 := HashPassword(password)
	hash2 := HashPassword(password)

	if hash1 != hash2 {
		t.Error("Same password should produce same hash")
	}

	if hash1 == password {
		t.Error("Hash should not equal the original password")
	}

	if len(hash1) != 64 { // SHA-256 produces 64 hex characters
		t.Errorf("Expected hash length 64, got %d", len(hash1))
	}
}

func TestHashPasswordDifferent(t *testing.T) {
	hash1 := HashPassword("password1")
	hash2 := HashPassword("password2")

	if hash1 == hash2 {
		t.Error("Different passwords should produce different hashes")
	}
}

// GenerateSaltFixed generates a fixed salt for testing
func GenerateSaltFixed() []byte {
	salt := make([]byte, SaltSize)
	for i := range salt {
		salt[i] = byte(i)
	}
	return salt
}

func BenchmarkEncrypt(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)
	plaintext := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encrypt(key, plaintext)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key := make([]byte, 32)
	rand.Read(key)
	plaintext := make([]byte, 1024)
	ciphertext, _ := Encrypt(key, plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decrypt(key, ciphertext)
	}
}

func BenchmarkDeriveKey(b *testing.B) {
	password := "test-password"
	salt, _ := GenerateSalt()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeriveKey(password, salt)
	}
}
