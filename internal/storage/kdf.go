package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/wii/senv/internal/crypto"
)

const (
	currentMetadataVersion  = "1.0"
	maxCurrentKDFIterations = 1_000_000
)

// ErrInvalidKDFParameters identifies unsupported or corrupt metadata KDF
// parameters. Callers must report this as a metadata error, never as a bad
// password.
var ErrInvalidKDFParameters = errors.New("invalid or unsupported metadata KDF parameters")

// ValidatedKDFIterations returns the only PBKDF2 iteration count that may be
// used for this metadata version. Unversioned metadata is supported solely as
// the legacy format without an explicit KDF cost. Version 1.0 retains the same
// missing/zero legacy behavior while bounding explicit costs before PBKDF2.
func (m *Metadata) ValidatedKDFIterations() (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: metadata is nil", ErrInvalidKDFParameters)
	}

	switch m.Version {
	case "":
		if m.KDFIterations != 0 {
			return 0, fmt.Errorf("%w: unversioned metadata cannot specify kdf_iterations", ErrInvalidKDFParameters)
		}
		return crypto.LegacyIterations, nil
	case currentMetadataVersion:
		if m.KDFIterations == 0 {
			return crypto.LegacyIterations, nil
		}
		if m.KDFIterations < crypto.LegacyIterations || m.KDFIterations > maxCurrentKDFIterations {
			return 0, fmt.Errorf("%w: version %s kdf_iterations must be between %d and %d", ErrInvalidKDFParameters, m.Version, crypto.LegacyIterations, maxCurrentKDFIterations)
		}
		return m.KDFIterations, nil
	default:
		return 0, fmt.Errorf("%w: unsupported metadata version %q", ErrInvalidKDFParameters, m.Version)
	}
}

// UnmarshalJSON decodes kdf_iterations without allowing JSON numbers to wrap
// or truncate to the platform int type. The versioned validator runs as part of
// decoding so every metadata JSON consumer gets identical boundary behavior.
func (m *Metadata) UnmarshalJSON(data []byte) error {
	var wire struct {
		Version       string          `json:"version"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
		Salt          string          `json:"salt"`
		PasswordKey   string          `json:"password_key"`
		KDFIterations json.RawMessage `json:"kdf_iterations"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	iterations := 0
	if len(wire.KDFIterations) != 0 && string(wire.KDFIterations) != "null" {
		var number json.Number
		if err := json.Unmarshal(wire.KDFIterations, &number); err != nil {
			return fmt.Errorf("%w: kdf_iterations is not an integer: %v", ErrInvalidKDFParameters, err)
		}
		value, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return fmt.Errorf("%w: kdf_iterations is outside the supported integer representation", ErrInvalidKDFParameters)
		}
		maxInt := int64(^uint(0) >> 1)
		minInt := -maxInt - 1
		if value < minInt || value > maxInt {
			return fmt.Errorf("%w: kdf_iterations is outside the platform integer representation", ErrInvalidKDFParameters)
		}
		iterations = int(value)
	}

	*m = Metadata{
		Version:       wire.Version,
		CreatedAt:     wire.CreatedAt,
		UpdatedAt:     wire.UpdatedAt,
		Salt:          wire.Salt,
		PasswordKey:   wire.PasswordKey,
		KDFIterations: iterations,
	}
	_, err := m.ValidatedKDFIterations()
	return err
}

// ParseMetadata decodes and validates metadata obtained from local or synced
// storage. It is the shared boundary for raw metadata blobs.
func ParseMetadata(data []byte) (*Metadata, error) {
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}
