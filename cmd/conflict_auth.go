package cmd

import (
	"errors"

	"github.com/wii/senv/internal/conflict"
)

// resolveConflictAuth obtains a vault key only after the user asks to reveal or
// merge conflict contents. It reuses the normal session/auth memo and never
// includes the key or password in returned error text.
func resolveConflictAuth(prompt passwordPrompter, remoteMetadata []byte) (conflict.Auth, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), prompt)
	if err != nil {
		return conflict.Auth{}, err
	}
	key, err := resolveKeyForAuth(auth)
	if err != nil {
		if errors.Is(err, errInvalidPassword) {
			return conflict.Auth{}, errInvalidPassword
		}
		return conflict.Auth{}, err
	}
	return conflict.NewAuth(key, remoteMetadata), nil
}
