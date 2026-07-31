package cmd

import (
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/session"
)

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change the encryption password",
	Long: `Change the password used to encrypt all senv data.
This re-encrypts every data file (env, text, config) with a new key
derived from the new password. On failure the original encryption is preserved.`,
	RunE: runPasswd,
}

func init() {
	rootCmd.AddCommand(passwdCmd)
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
	newKey := crypto.DeriveKey(newPassword, newSalt)
	passwordHash := crypto.HashPassword(newPassword)
	newPasswordKey, err := crypto.Encrypt(newKey, []byte(passwordHash))
	if err != nil {
		return fmt.Errorf("failed to create password verifier: %w", err)
	}

	result, err := auth.storage.Rekey(oldKey, newKey,
		base64.StdEncoding.EncodeToString(newSalt), newPasswordKey)
	if err != nil {
		return fmt.Errorf("password change failed: %w", err)
	}

	sm := session.NewManager(configPath, dataPath)
	sm.ClearSession()
	clearAuthMemo()

	fmt.Printf("✓ Password changed. Re-encrypted %d file(s).\n", result.Total())
	fmt.Println("  Run 'senv session start' to cache the new key.")
	return nil
}
