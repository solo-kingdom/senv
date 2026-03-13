package crypto

import (
	"crypto/rand"
	"io"
)

// SecureBytes holds sensitive data that can be securely cleared from memory
type SecureBytes struct {
	data []byte
}

// NewSecureBytes creates a new SecureBytes from a byte slice
func NewSecureBytes(data []byte) *SecureBytes {
	return &SecureBytes{data: data}
}

// Bytes returns the underlying byte slice
func (s *SecureBytes) Bytes() []byte {
	return s.data
}

// String returns the data as a string
func (s *SecureBytes) String() string {
	return string(s.data)
}

// Clear securely wipes the data from memory
func (s *SecureBytes) Clear() {
	if s.data == nil {
		return
	}
	// Overwrite with zeros
	for i := range s.data {
		s.data[i] = 0
	}
	// Overwrite with random data
	io.ReadFull(rand.Reader, s.data)
	// Overwrite with zeros again
	for i := range s.data {
		s.data[i] = 0
	}
	s.data = nil
}

// Len returns the length of the data
func (s *SecureBytes) Len() int {
	return len(s.data)
}

// SecurePassword provides a secure way to handle passwords in memory
type SecurePassword struct {
	*SecureBytes
}

// NewSecurePassword creates a new SecurePassword from a string
func NewSecurePassword(password string) *SecurePassword {
	return &SecurePassword{
		SecureBytes: NewSecureBytes([]byte(password)),
	}
}

// SecureKey provides a secure way to handle encryption keys in memory
type SecureKey struct {
	*SecureBytes
}

// NewSecureKey creates a new SecureKey from a byte slice
func NewSecureKey(key []byte) *SecureKey {
	return &SecureKey{
		SecureBytes: NewSecureBytes(key),
	}
}

// ClearPassword safely clears a password byte slice
func ClearPassword(password []byte) {
	if password == nil {
		return
	}
	for i := range password {
		password[i] = 0
	}
}

// ClearString safely clears a string by converting to bytes first
// Note: This is a best-effort approach as strings are immutable in Go
// The actual clearing depends on whether the string is backed by a mutable byte array
func ClearString(s *string) {
	if s == nil {
		return
	}
	// Convert to bytes and clear
	// This won't actually clear the original string due to Go's string immutability
	// But it's a signal that the string should no longer be used
	*s = ""
}
