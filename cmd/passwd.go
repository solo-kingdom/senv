package cmd

import (
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change the encryption password",
	Long: `Change the password used to encrypt all senv data.
This re-encrypts every data file (env, text, config) with a new key
derived with the current 600,000-iteration PBKDF2 setting; legacy vaults are
upgraded even when the password is unchanged.

Rekey is a recoverable transaction. Later vault access automatically rolls
back or completes a safely identifiable interrupted transaction. If recovery
cannot be proven safe, access fails closed, preserves recovery files, and
reports guidance instead of exposing a mixed-key vault.`,
	RunE: runPasswd,
}

func init() {
	rootCmd.AddCommand(passwdCmd)
	passwdCmd.Annotations = map[string]string{"senv/skip-auto-push": "true"}
}

func runPasswd(cmd *cobra.Command, args []string) error {
	configPath := getConfigPath()
	dataPath := getDataPath()

	auth, err := resolveAuth(configPath, dataPath, authPrompt)
	if err != nil {
		return err
	}

	oldKey, err := resolveKeyForAuth(auth)
	if err != nil {
		return err
	}

	newPassword, err := authPrompt("Senv - Enter new password: ")
	if err != nil {
		return err
	}
	if newPassword == "" {
		return fmt.Errorf("password must not be empty")
	}

	confirm, err := authPrompt("Senv - Confirm new password: ")
	if err != nil {
		return err
	}
	if newPassword != confirm {
		return fmt.Errorf("passwords do not match")
	}

	newSalt, err := crypto.GenerateSalt()
	if err != nil {
		return fmt.Errorf("failed to generate salt: %w", err)
	}
	// Rekey upgrades the KDF to the current default so old vaults gain the
	// strengthened iteration count. Validate the target metadata before deriving.
	targetMetadata := storage.NewMetadata("", "")
	newIterations, err := targetMetadata.ValidatedKDFIterations()
	if err != nil {
		return err
	}
	newKey := deriveKeyWithIterations(newPassword, newSalt, newIterations)
	passwordHash := crypto.HashPassword(newPassword)
	newPasswordKey, err := crypto.Encrypt(newKey, []byte(passwordHash))
	if err != nil {
		return fmt.Errorf("failed to create password verifier: %w", err)
	}

	result, err := auth.storage.Rekey(oldKey, newKey,
		base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, newIterations)
	if err != nil {
		return fmt.Errorf("password change failed: %w", err)
	}

	sm := session.NewManager(configPath, dataPath)
	sm.ClearSession()
	clearAuthMemo()

	fmt.Printf("✓ Password changed. Re-encrypted %d file(s). KDF iterations: %d.\n",
		result.Total(), newIterations)
	fmt.Println("  Note: senv binaries older than this version cannot unlock this vault anymore.")
	fmt.Println("  Run 'senv session start' to cache the new key.")
	pushBlockingAfterCriticalWrite(cmd)
	return nil
}
