package storage

import (
	"fmt"
	"strings"

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

// ValidateHeaderName checks that an HTTP header name is a valid RFC 7230
// token. Header names become JSON object keys / TOML keys in agent configs,
// so anything outside the token alphabet could render invalid output.
func ValidateHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name is empty")
	}
	for _, r := range name {
		if !isTokenRune(r) {
			return fmt.Errorf("%q is not a valid HTTP header name", name)
		}
	}
	return nil
}

func isTokenRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	// RFC 7230 token delimiters minus the ones already covered above.
	return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}

// validateHeaderValue rejects characters that cannot appear in a rendered
// single-line header value (control characters; NUL included).
func validateHeaderValue(value string) error {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("value must not contain control characters")
		}
	}
	return nil
}
