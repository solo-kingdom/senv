package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	dataPath          string
	rootShorthandFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "senv",
	Short: "Secure environment variable and configuration manager",
	Long: `Senv is a secure tool for managing environment variables and configuration files.
It provides encrypted storage for sensitive data with group-based organization.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		group, key, ok := parseAddress(args[0])
		if !ok {
			return cmd.Help()
		}
		return runTextShorthand(group, key, rootShorthandFile, args[1:])
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Get default data path
	defaultPath := getDefaultDataPath()

	rootCmd.PersistentFlags().StringVar(&dataPath, "path", defaultPath, "data storage path")
	rootCmd.Flags().StringVar(&rootShorthandFile, "file", "", "read value from file (shorthand)")
}

func getDefaultConfigPath() string {
	usr, err := user.Current()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "senv")
	}
	return filepath.Join(usr.HomeDir, ".config", "senv")
}

func getDefaultDataPath() string {
	usr, err := user.Current()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "senv", "data")
	}
	return filepath.Join(usr.HomeDir, ".config", "senv", "data")
}

func getConfigPath() string {
	return getDefaultConfigPath()
}

func getDataPath() string {
	// Expand home directory if needed
	if len(dataPath) >= 2 && dataPath[:2] == "~/" {
		usr, err := user.Current()
		if err != nil {
			return dataPath
		}
		return filepath.Join(usr.HomeDir, dataPath[2:])
	}
	return dataPath
}
