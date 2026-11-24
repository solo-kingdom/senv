package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
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

	// 先校验初始化与 metadata/KDF 合法性（在任何口令输入和 PBKDF2 之前 fail-fast），
	// 且更改主口令始终要求重新输入并校验当前主口令，不得仅凭缓存会话改密。
	store := storage.NewManager(configPath, dataPath)
	if !store.IsInitialized() {
		return errNotInitialized
	}
	md, err := store.LoadMetadata()
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}
	oldSalt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		return fmt.Errorf("failed to decode salt: %w", err)
	}
	oldIterations, err := md.ValidatedKDFIterations()
	if err != nil {
		return err
	}

	currentPassword, err := authPrompt("Senv - Enter current password: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	oldKey := deriveKeyWithIterations(currentPassword, oldSalt, oldIterations)
	defer session.ZeroKey(oldKey)

	storedHash, err := crypto.DecryptWithAAD(oldKey, md.PasswordKey, crypto.BuildEntryAAD("metadata_verifier", "", "", md.KeyEpoch))
	if err != nil || crypto.HashPassword(currentPassword) != string(storedHash) {
		return errInvalidPassword
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
	defer session.ZeroKey(newKey)
	newEpoch := md.KeyEpoch + 1
	passwordHash := crypto.HashPassword(newPassword)
	newPasswordKey, err := crypto.EncryptWithAAD(newKey, []byte(passwordHash), crypto.BuildEntryAAD("metadata_verifier", "", "", newEpoch))
	if err != nil {
		return fmt.Errorf("failed to create password verifier: %w", err)
	}

	result, err := store.Rekey(oldKey, newKey,
		base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, newIterations)
	if err != nil {
		return fmt.Errorf("password change failed: %w", err)
	}

	// 删除旧主密钥加密的本地 TUI 快照缓存，避免旧口令泄露后仍可解出旧快照。
	if root, rerr := securefs.OpenRoot(dataPath); rerr == nil {
		if rmErr := root.Remove("tui-snapshot.enc"); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "warning: failed to remove stale tui-snapshot.enc: %v\n", rmErr)
		}
		_ = root.Close()
	}

	sm := session.NewManager(configPath, dataPath)
	sm.ClearSession()
	clearAuthMemo()

	fmt.Printf("✓ Password changed. Re-encrypted %d file(s). KDF iterations: %d, key_epoch: %d.\n",
		result.Total(), newIterations, newEpoch)
	fmt.Println("  Note: senv binaries older than this version cannot unlock this vault anymore.")
	fmt.Println("  Run 'senv session start' to cache the new key.")
	pushBlockingAfterCriticalWrite(cmd)
	return nil
}
