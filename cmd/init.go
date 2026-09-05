package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
	"golang.org/x/term"
)

var (
	initServerAddress string
	initServerToken   string
	initServerVault   string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new senv project",
	Long: `Initialize a new senv project with encrypted storage.
This creates the necessary directory structure and configuration files. New
vaults use PBKDF2-SHA256 with 600,000 iterations; legacy metadata without an
explicit cost remains readable at 100,000 iterations.

With --server, join an existing vault hosted on senv-server: pull the hosted
metadata blob and all entries into a local cache, then unlock with the vault
password. The vault password is never sent to the server.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initServerAddress, "server", "", "senv-server 地址（接入已有 server vault）")
	initCmd.Flags().StringVar(&initServerToken, "token", "", "server token（默认取环境变量 SENV_SERVER_TOKEN）")
	initCmd.Flags().StringVar(&initServerVault, "vault", "main", "server 端 vault 名")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	configPath := getConfigPath()
	dataPath := getDataPath()

	// Check if already initialized
	manager := getStorage()
	if manager.IsInitialized() {
		return fmt.Errorf("project already initialized at %s", configPath)
	}

	// server 模式：接入 server 上已有的 vault
	if initServerAddress != "" {
		return runInitServer(manager, configPath, dataPath)
	}

	// Guard: if encrypted data files already exist without metadata, refuse to
	// avoid minting a new key that would render them undecryptable.
	if manager.HasOrphanedData() {
		return fmt.Errorf("%w\n\nData directory %q already contains encrypted files but no metadata.json.\n"+
			"Re-running init will generate a new key and make them undecryptable.\n"+
			"Restore metadata.json from version control, or back up and remove the\n"+
			"existing data before initializing.",
			storage.ErrOrphanedData, dataPath)
	}

	// Prompt for password
	password, err := promptPassword("Senv - Enter password for encryption: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	// Confirm password
	confirmPassword, err := promptPassword("Senv - Confirm password: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	// Verify passwords match
	if password != confirmPassword {
		return fmt.Errorf("passwords do not match")
	}

	// Initialize
	fmt.Printf("Initializing senv project...\n")
	fmt.Printf("  Config path: %s\n", configPath)
	fmt.Printf("  Data path: %s\n", dataPath)
	if err := manager.Initialize(password); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	pushBlockingAfterCriticalWrite(cmd)

	fmt.Println("✓ Project initialized successfully!")
	fmt.Println()
	fmt.Println("Quick start:")
	fmt.Println("  senv env set DATABASE_URL \"postgres://localhost/db\"")
	fmt.Println("  senv env set --group prod API_KEY \"sk-xxx\"")
	fmt.Println("  senv env list")
	fmt.Println("  senv session start -t restart")
	fmt.Println("  eval \"$(senv env export --if-session)\"")

	return nil
}

// runInitServer 以 server 地址 + token 初始化：拉取 metadata 与全部条目建本地缓存，
// 然后用 vault 口令解锁。口令只在本地派生 key 校验，绝不发往 server。
func runInitServer(manager *storage.Manager, configPath, dataPath string) error {
	token := initServerToken
	if token == "" {
		token = os.Getenv("SENV_SERVER_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("缺少 server token：请提供 --token 或设置 SENV_SERVER_TOKEN")
	}

	if err := provider.ValidateServerAddress(initServerAddress); err != nil {
		return err
	}
	sp := provider.NewServerProvider(initServerAddress, token, configPath, dataPath, initServerVault)

	fmt.Printf("正在从 server 拉取 vault %q ...\n", initServerVault)
	if err := sp.Bootstrap(context.Background()); err != nil {
		if errors.Is(err, provider.ErrVaultNotFound) {
			return fmt.Errorf("server 上不存在 vault %q（或 token 无权限）\n请先在已有机器上执行 senv migrate to-server，或检查 vault 名与 token", initServerVault)
		}
		return err
	}
	fmt.Println("✓ 本地缓存已建立")

	// vault 口令解锁：与本地模式一致（PBKDF2 派生 + passwordKey 校验）
	password, err := promptPassword("Senv - Enter vault password: ")
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	ok, err := manager.VerifyPassword(password)
	if err != nil {
		return fmt.Errorf("校验口令失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("vault 口令错误（本地缓存已落盘，修正口令可通过 senv session start 或直接重试命令解锁）")
	}

	// 写入 provider 配置（机器本地，不同步）
	settings, err := manager.LoadSettings()
	if err != nil {
		settings = storage.NewSettings()
	}
	settings.Provider = storage.ProviderConfig{
		Type:    provider.TypeServer,
		Address: initServerAddress,
		Token:   token,
		Vault:   initServerVault,
	}
	if err := manager.SaveSettings(settings); err != nil {
		return fmt.Errorf("保存 provider 配置失败: %w", err)
	}

	fmt.Println("✓ 已接入 server vault，口令验证通过")
	fmt.Println("  同步: senv sync")
	return nil
}

func promptPassword(prompt string) (string, error) {
	// Check if stdin is a terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // New line after password
		return string(password), err
	}

	// Fallback for non-terminal input
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, prompt)
	password, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(password), nil
}
