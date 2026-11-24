package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var (
	migrateRequireV2   bool
	migrateNoRequireV2 bool
	migrateForceV2     bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate data to the latest storage format",
	Long: `Migrate all environment variable groups from the legacy single-file
format to the new per-variable format. This reduces git conflicts when
syncing across multiple machines. Migration is idempotent and also happens
automatically on first access.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := getConfigPath()
		dataPath := getDataPath()

		auth, err := resolveAuth(configPath, dataPath, authPrompt)
		if err != nil {
			return err
		}

		key, err := resolveKeyForAuth(auth)
		if err != nil {
			return err
		}

		count, err := auth.storage.MigrateAllEnvGroups(key)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		if count == 0 {
			fmt.Println("✓ Already up to date. No migration needed.")
		} else {
			fmt.Printf("✓ Migrated %d group(s) to per-variable storage.\n", count)
		}
		return nil
	},
}

var migrateToV2Cmd = &cobra.Command{
	Use:   "to-v2",
	Short: "Upgrade all vault entries and indexes to v2 AAD-bound envelopes",
	Long: `Re-encrypt every vault entry, config_index.json, and skill_index.json into
the v2 AAD-bound envelope format with an incremented key_epoch. Optionally
enable strict anti-downgrade protection via --require-v2 so legacy v1
ciphertexts and plaintext indexes are rejected.`,
	Annotations: map[string]string{"senv/skip-auto-push": "true"},
	RunE:        runMigrateToV2,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(migrateToV2Cmd)
	migrateToV2Cmd.Flags().BoolVar(&migrateRequireV2, "require-v2", false, "Reject legacy v1 ciphertexts and plaintext indexes after migration")
	migrateToV2Cmd.Flags().BoolVar(&migrateNoRequireV2, "no-require-v2", false, "Disable strict v2-only policy (require_v2=false)")
	migrateToV2Cmd.Flags().BoolVar(&migrateForceV2, "force", false, "Re-encrypt all entries into a new key_epoch even if already on v2")
}

func runMigrateToV2(cmd *cobra.Command, args []string) error {
	if migrateRequireV2 && migrateNoRequireV2 {
		return fmt.Errorf("--require-v2 and --no-require-v2 cannot be used together")
	}

	configPath := getConfigPath()
	dataPath := getDataPath()
	store := storage.NewManager(configPath, dataPath)
	if !store.IsInitialized() {
		return errNotInitialized
	}

	if migrateNoRequireV2 && !migrateForceV2 {
		settings, err := store.LoadSettings()
		if err != nil {
			return fmt.Errorf("failed to load settings: %w", err)
		}
		settings.Vault.RequireV2 = false
		settings.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := store.SaveSettings(settings); err != nil {
			return fmt.Errorf("failed to update settings: %w", err)
		}
		fmt.Println("✓ Disabled vault.require_v2 policy.")
		return nil
	}

	md, err := store.LoadMetadata()
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		return fmt.Errorf("failed to decode salt: %w", err)
	}
	iterations, err := md.ValidatedKDFIterations()
	if err != nil {
		return err
	}

	password, err := authPrompt("Senv - Enter current password: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	oldKey := deriveKeyWithIterations(password, salt, iterations)
	defer session.ZeroKey(oldKey)

	storedHash, err := crypto.DecryptWithAAD(oldKey, md.PasswordKey, crypto.BuildEntryAAD("metadata_verifier", "", "", md.KeyEpoch))
	if err != nil || crypto.HashPassword(password) != string(storedHash) {
		return errInvalidPassword
	}

	report, err := store.CheckConsistency(oldKey)
	if err != nil {
		return fmt.Errorf("pre-migration consistency check failed: %w", err)
	}
	if !report.AllOK() {
		return fmt.Errorf("vault has undecryptable entries; run 'senv doctor' before migrating to v2")
	}

	rekeyedCount := 0
	currentEpoch := md.KeyEpoch
	if md.KeyEpoch == 0 || report.LegacyV1Count > 0 || migrateForceV2 {
		newSalt, err := crypto.GenerateSalt()
		if err != nil {
			return fmt.Errorf("failed to generate salt: %w", err)
		}
		targetMetadata := storage.NewMetadata("", "")
		newIterations, err := targetMetadata.ValidatedKDFIterations()
		if err != nil {
			return err
		}
		newKey := deriveKeyWithIterations(password, newSalt, newIterations)
		defer session.ZeroKey(newKey)

		newEpoch := md.KeyEpoch + 1
		passwordHash := crypto.HashPassword(password)
		newPasswordKey, err := crypto.EncryptWithAAD(newKey, []byte(passwordHash), crypto.BuildEntryAAD("metadata_verifier", "", "", newEpoch))
		if err != nil {
			return fmt.Errorf("failed to create password verifier: %w", err)
		}

		res, err := store.Rekey(oldKey, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, newIterations)
		if err != nil {
			return fmt.Errorf("v2 migration failed: %w", err)
		}
		rekeyedCount = res.Total()
		currentEpoch = newEpoch

		postReport, err := store.CheckConsistency(newKey)
		if err != nil {
			return fmt.Errorf("post-migration consistency check failed: %w", err)
		}
		if !postReport.AllOK() || postReport.LegacyV1Count > 0 {
			return fmt.Errorf("post-migration verification failed (legacy_v1=%d); run 'senv doctor'", postReport.LegacyV1Count)
		}

		if root, rerr := securefs.OpenRoot(dataPath); rerr == nil {
			if rmErr := root.Remove("tui-snapshot.enc"); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "warning: failed to remove stale tui-snapshot.enc: %v\n", rmErr)
			}
			_ = root.Close()
		}
		sm := session.NewManager(configPath, dataPath)
		sm.ClearSession()
		clearAuthMemo()
		pushBlockingAfterCriticalWrite(cmd)
	}

	settings, err := store.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if migrateRequireV2 {
		settings.Vault.RequireV2 = true
		settings.UpdatedAt = time.Now().Format(time.RFC3339)
		if err := store.SaveSettings(settings); err != nil {
			return fmt.Errorf("failed to save vault.require_v2 setting: %w", err)
		}
	}

	fmt.Printf("✓ Vault v2 envelope active (key_epoch=%d, re-encrypted=%d, require_v2=%v).\n",
		currentEpoch, rekeyedCount, settings.Vault.RequireV2)
	return nil
}
