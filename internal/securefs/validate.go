package securefs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateSegment accepts exactly one portable path segment. It rejects both
// Unix and Windows path syntax regardless of the host platform so identities
// remain safe when synchronized between systems.
func ValidateSegment(segment string) error {
	switch segment {
	case "", ".", "..":
		return invalidSegment(segment)
	}
	if strings.ContainsAny(segment, "\x00:/\\") {
		return invalidSegment(segment)
	}
	if filepath.IsAbs(segment) || filepath.VolumeName(segment) != "" {
		return invalidSegment(segment)
	}
	if filepath.Clean(segment) != segment || filepath.Base(segment) != segment {
		return invalidSegment(segment)
	}
	return nil
}

func invalidSegment(segment string) error {
	return &PathError{
		Op:   "validate segment",
		Path: segment,
		Err:  fmt.Errorf("%w", ErrInvalidSegment),
	}
}

func validateSegments(segments []string) error {
	if len(segments) == 0 {
		return &PathError{Op: "validate path", Err: ErrContainment}
	}
	for _, segment := range segments {
		if err := ValidateSegment(segment); err != nil {
			return err
		}
	}
	return nil
}

func displayPath(segments []string) string {
	return strings.Join(segments, "/")
}
