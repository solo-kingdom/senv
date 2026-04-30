package storage

import (
	"fmt"
	"strings"
)

// ValidateName checks that a group or key name does not contain ':'.
func ValidateName(name string) error {
	if strings.Contains(name, ":") {
		return fmt.Errorf("%q must not contain ':'", name)
	}
	return nil
}
