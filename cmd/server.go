package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

var (
	serverRegisterAddress string
	serverRegisterCode    string
	serverRegisterName    string
	serverRegisterVault   string
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "senv-server 相关操作（注册等）",
}

var serverRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "凭一次性注册码把本机注册为 server client",
	Long: `向 senv-server 提交一次性注册码完成本机注册，取得 client 专属 token 并写入
机器本地 server-token.json（0600，git 同步永远排除；settings.json 只保留
provider 类型/地址/vault 等非敏感字段）。明文 token 只在注册响应中出现一次。

注册后：
  - 本机已初始化过存储：立即生效，后续同步使用新 client 凭证
  - 全新机器：执行 senv init --server 接入 vault（地址与 token 自动取自本机）`,
	RunE: runServerRegister,
}

func init() {
	serverRegisterCmd.Flags().StringVar(&serverRegisterAddress, "address", "", "senv-server 地址（https://host[:port]）")
	serverRegisterCmd.Flags().StringVar(&serverRegisterCode, "code", "", "管理员签发的一次性注册码")
	serverRegisterCmd.Flags().StringVar(&serverRegisterName, "name", "", "本机设备名（同用户下唯一）")
	serverRegisterCmd.Flags().StringVar(&serverRegisterVault, "vault", "main", "server 端 vault 名")
	serverRegisterCmd.MarkFlagRequired("address")
	serverRegisterCmd.MarkFlagRequired("code")
	serverRegisterCmd.MarkFlagRequired("name")
	serverCmd.AddCommand(serverRegisterCmd)
	rootCmd.AddCommand(serverCmd)
	// 注册命令自身不触发自动同步
	serverRegisterCmd.Annotations = map[string]string{"senv/skip-auto-push": "true"}
}

func runServerRegister(cmd *cobra.Command, args []string) error {
	if err := provider.ValidateServerAddress(serverRegisterAddress); err != nil {
		return err
	}
	res, err := provider.RegisterClient(context.Background(), serverRegisterAddress, serverRegisterCode, serverRegisterName)
	if err != nil {
		return err
	}
	token, clientName := res.Token, res.ClientName

	// token 写入机器本地 server-token.json（git 同步永远排除）；settings.json
	// 只保留非敏感的 provider 类型/地址/vault。未初始化的全新机器也可先
	// 注册——settings.json 与 metadata.json 相互独立，init 防呆不受影响
	manager := getStorage()
	settings, err := manager.LoadSettings()
	if err != nil {
		settings = storage.NewSettings()
	}
	settings.Provider = storage.ProviderConfig{
		Type:    provider.TypeServer,
		Address: serverRegisterAddress,
		Vault:   serverRegisterVault,
	}
	if err := manager.SaveSettings(settings); err != nil {
		return fmt.Errorf("保存 provider 配置失败: %w", err)
	}
	if err := manager.SaveServerToken(token); err != nil {
		return fmt.Errorf("保存 server token 失败: %w", err)
	}

	fmt.Printf("✓ 本机已注册为 client %q（server: %s，vault: %s）\n", clientName, serverRegisterAddress, serverRegisterVault)
	fmt.Printf("  token 已写入 %s（机器本地文件，不入 git 同步，请勿泄露）\n", storage.ServerTokenFile)
	if manager.IsInitialized() {
		fmt.Println("  后续: senv sync 验证连通性")
	} else {
		fmt.Println("  后续: senv init --server 接入 vault（地址与 token 自动取自本机 settings）")
	}
	return nil
}
