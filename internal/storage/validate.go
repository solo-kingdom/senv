package storage

import (
	"fmt"

	"github.com/wii/senv/internal/syncschema"
)

// ValidateEnvKey checks that an env variable key is a valid POSIX shell
// variable name. Env keys are exported to the shell via `env export`, so they
// must match `^[A-Za-z_][A-Za-z0-9_]*$` to be safely consumed by `eval`.
func ValidateEnvKey(name string) error {
	if err := syncschema.ValidateEnvKey(name); err != nil {
		return fmt.Errorf("%q is not a valid shell variable name: must match [A-Za-z_][A-Za-z0-9_]*", name)
	}
	return nil
}
