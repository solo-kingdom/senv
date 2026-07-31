package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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

func init() {
	rootCmd.AddCommand(migrateCmd)
}
