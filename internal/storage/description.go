package storage

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxDescriptionBytes is the UTF-8 byte cap for vault descriptions.
const MaxDescriptionBytes = 2048

// ValidateDescription trims space and enforces the byte cap.
// emptyOK true allows the empty string (entries and grandfathered groups);
// emptyOK false is for creating a new env/text group.
func ValidateDescription(raw string, emptyOK bool) (string, error) {
	desc := strings.TrimSpace(raw)
	if desc == "" {
		if emptyOK {
			return "", nil
		}
		return "", fmt.Errorf("description is required")
	}
	if !utf8.ValidString(desc) {
		return "", fmt.Errorf("description must be valid UTF-8")
	}
	if len(desc) > MaxDescriptionBytes {
		return "", fmt.Errorf("description exceeds %d bytes (%d bytes)", MaxDescriptionBytes, len(desc))
	}
	return desc, nil
}

// ErrGroupMissing is returned when writing into an env/text group that was
// never explicitly created.
func ErrGroupMissing(kind, name string) error {
	return fmt.Errorf("%s group %q does not exist; create it with `senv %s group add %s --description ...`", kind, name, kind, name)
}
