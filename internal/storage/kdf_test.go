package storage

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

func TestKDFValidatedIterations(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		iterations int
		want       int
		wantErr    bool
	}{
		{name: "unversioned missing", want: crypto.LegacyIterations},
		{name: "missing", version: currentMetadataVersion, want: crypto.LegacyIterations},
		{name: "zero", version: currentMetadataVersion, iterations: 0, want: crypto.LegacyIterations},
		{name: "negative", version: currentMetadataVersion, iterations: -1, wantErr: true},
		{name: "below minimum", version: currentMetadataVersion, iterations: 99_999, wantErr: true},
		{name: "minimum", version: currentMetadataVersion, iterations: 100_000, want: 100_000},
		{name: "current default", version: currentMetadataVersion, iterations: 600_000, want: 600_000},
		{name: "maximum", version: currentMetadataVersion, iterations: 1_000_000, want: 1_000_000},
		{name: "above maximum", version: currentMetadataVersion, iterations: 1_000_001, wantErr: true},
		{name: "max int", version: currentMetadataVersion, iterations: math.MaxInt, wantErr: true},
		{name: "unversioned explicit", iterations: 600_000, wantErr: true},
		{name: "unsupported version", version: "2.0", iterations: 600_000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Metadata{Version: tt.version, KDFIterations: tt.iterations}).ValidatedKDFIterations()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidKDFParameters) {
					t.Fatalf("error = %v, want ErrInvalidKDFParameters", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatedKDFIterations: %v", err)
			}
			if got != tt.want {
				t.Fatalf("iterations = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestKDFJSONOverflowIterations(t *testing.T) {
	for _, raw := range []string{
		`9223372036854775808`,
		`18446744073709551615`,
	} {
		data := []byte(`{"version":"1.0","salt":"c2FsdA==","password_key":"key","kdf_iterations":` + raw + `}`)
		if _, err := ParseMetadata(data); !errors.Is(err, ErrInvalidKDFParameters) {
			t.Fatalf("ParseMetadata(%s) error = %v, want ErrInvalidKDFParameters", raw, err)
		}
	}
}

func writeInvalidKDFMetadata(t *testing.T, manager *Manager, iterations int) {
	t.Helper()
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	metadata.KDFIterations = iterations
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manager.configPath, MetadataFile), data, 0o600); err != nil {
		t.Fatalf("write invalid metadata: %v", err)
	}
}

func TestInvalidKDFRejectedBeforeDeriveStorage(t *testing.T) {
	manager, _ := setupTestManager(t)
	writeInvalidKDFMetadata(t, manager, 1_000_001)

	originalDerive := deriveKeyWithIterations
	deriveCalls := 0
	deriveKeyWithIterations = func(password string, salt []byte, iterations int) []byte {
		deriveCalls++
		return originalDerive(password, salt, iterations)
	}
	t.Cleanup(func() { deriveKeyWithIterations = originalDerive })

	valid, err := manager.VerifyPassword("test-password")
	if valid {
		t.Fatal("invalid KDF metadata must not verify as a valid password")
	}
	if !errors.Is(err, ErrInvalidKDFParameters) {
		t.Fatalf("VerifyPassword error = %v, want ErrInvalidKDFParameters", err)
	}
	if deriveCalls != 0 {
		t.Fatalf("PBKDF2 invoked %d times for invalid metadata", deriveCalls)
	}

	if _, err := manager.deriveKeyFromPassword("test-password"); !errors.Is(err, ErrInvalidKDFParameters) {
		t.Fatalf("manager derivation error = %v, want ErrInvalidKDFParameters", err)
	}
	if deriveCalls != 0 {
		t.Fatalf("PBKDF2 invoked %d times through manager path", deriveCalls)
	}

	if _, err := manager.Rekey(nil, nil, "", "", crypto.DefaultIterations); !errors.Is(err, ErrInvalidKDFParameters) {
		t.Fatalf("Rekey error = %v, want ErrInvalidKDFParameters", err)
	}
	if deriveCalls != 0 {
		t.Fatalf("PBKDF2 invoked %d times through rekey path", deriveCalls)
	}
}
