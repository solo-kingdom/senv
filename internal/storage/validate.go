package storage

import (
	"fmt"
	"regexp"
	"strings"
)

// validEnvKeyRe defines a POSIX shell variable name: an uppercase/lowercase
// letter or underscore followed by zero or more letters, digits or underscores.
// Env keys are emitted as `export <key>=...` statements and MUST therefore be
// valid shell variable names; otherwise `eval $(senv env export)` fails with
// errors like `not valid in this context`.
var validEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateName checks that a group or key name does not contain ':'.
func ValidateName(name string) error {
	if strings.Contains(name, ":") {
		return fmt.Errorf("%q must not contain ':'", name)
	}
	return nil
}

// ValidateEnvKey checks that an env variable key is a valid POSIX shell
// variable name. Env keys are exported to the shell via `env export`, so they
// must match `^[A-Za-z_][A-Za-z0-9_]*$` to be safely consumed by `eval`.
func ValidateEnvKey(name string) error {
	if !validEnvKeyRe.MatchString(name) {
		return fmt.Errorf("%q is not a valid shell variable name: must match [A-Za-z_][A-Za-z0-9_]*", name)
	}
	return nil
}
