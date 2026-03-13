package crypto

import (
	"testing"
)

func TestSecureBytes(t *testing.T) {
	data := []byte("sensitive data")
	sb := NewSecureBytes(data)

	if string(sb.Bytes()) != "sensitive data" {
		t.Errorf("Unexpected bytes: %s", string(sb.Bytes()))
	}

	if sb.String() != "sensitive data" {
		t.Errorf("Unexpected string: %s", sb.String())
	}

	if sb.Len() != 14 {
		t.Errorf("Expected length 14, got %d", sb.Len())
	}
}

func TestSecureBytesClear(t *testing.T) {
	data := []byte("sensitive data")
	sb := NewSecureBytes(data)

	sb.Clear()

	if sb.Bytes() != nil {
		t.Error("Bytes should be nil after clear")
	}

	if sb.Len() != 0 {
		t.Errorf("Length should be 0 after clear, got %d", sb.Len())
	}
}

func TestSecurePassword(t *testing.T) {
	password := "my-secret-password"
	sp := NewSecurePassword(password)

	if sp.String() != password {
		t.Errorf("Unexpected password: %s", sp.String())
	}

	sp.Clear()

	if sp.Bytes() != nil {
		t.Error("Password should be nil after clear")
	}
}

func TestSecureKey(t *testing.T) {
	key := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	sk := NewSecureKey(key)

	if sk.Len() != 5 {
		t.Errorf("Expected length 5, got %d", sk.Len())
	}

	sk.Clear()

	if sk.Bytes() != nil {
		t.Error("Key should be nil after clear")
	}
}

func TestClearPassword(t *testing.T) {
	password := []byte("password123")
	ClearPassword(password)

	for _, b := range password {
		if b != 0 {
			t.Error("Password bytes should be zeroed")
		}
	}
}

func TestClearPasswordNil(t *testing.T) {
	// Should not panic
	ClearPassword(nil)
}

func TestClearString(t *testing.T) {
	s := "test"
	ClearString(&s)

	if s != "" {
		t.Errorf("String should be empty, got: %s", s)
	}
}

func TestClearStringNil(t *testing.T) {
	// Should not panic
	var s *string
	ClearString(s)
}

func TestSecureBytesClearTwice(t *testing.T) {
	sb := NewSecureBytes([]byte("test"))

	sb.Clear()
	sb.Clear() // Should not panic

	if sb.Bytes() != nil {
		t.Error("Bytes should still be nil after second clear")
	}
}

func BenchmarkSecureBytesClear(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sb := NewSecureBytes(data)
		sb.Clear()
	}
}
