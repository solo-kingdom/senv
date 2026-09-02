package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
)

var (
	migrateServerAddress string
	migrateServerToken   string
	migrateServerVault   string
	migrateForce         bool
)

func init() {
	migrateToServerCmd.Flags().StringVar(&migrateServerAddress, "server", "", "senv-server 地址（默认取 settings 或 SENV_SERVER_ADDRESS）")
	migrateToServerCmd.Flags().StringVar(&migrateServerToken, "token", "", "server token（默认取 settings 或 SENV_SERVER_TOKEN）")
	migrateToServerCmd.Flags().StringVar(&migrateServerVault, "vault", "main", "server 端 vault 名")
	migrateToServerCmd.Flags().BoolVar(&migrateForce, "force", false, "目标非空时显式确认覆盖（以源为准）")
	migrateFromServerCmd.Flags().StringVar(&migrateServerAddress, "server", "", "senv-server 地址（默认取 settings 或 SENV_SERVER_ADDRESS）")
	migrateFromServerCmd.Flags().StringVar(&migrateServerToken, "token", "", "server token（默认取 settings 或 SENV_SERVER_TOKEN）")
	migrateFromServerCmd.Flags().StringVar(&migrateServerVault, "vault", "main", "server 端 vault 名")
	migrateFromServerCmd.Flags().BoolVar(&migrateForce, "force", false, "目标非空时显式确认覆盖（以源为准）")
	migrateCmd.AddCommand(migrateToServerCmd, migrateFromServerCmd)
}

// resolveServerConn 解析 server 连接参数：flag > settings.json provider 配置 > 环境变量
func resolveServerConn() (address, token, vault string, err error) {
	address = migrateServerAddress
	token = migrateServerToken
	vault = migrateServerVault
	if settings, serr := getStorage().LoadSettings(); serr == nil {
		if address == "" {
			address = settings.Provider.Address
		}
		if token == "" {
			token = settings.Provider.Token
		}
	}
	if address == "" {
		address = os.Getenv("SENV_SERVER_ADDRESS")
	}
	if token == "" {
		token = os.Getenv("SENV_SERVER_TOKEN")
	}
	if address == "" {
		return "", "", "", fmt.Errorf("缺少 server 地址：请提供 --server 或设置 SENV_SERVER_ADDRESS")
	}
	if token == "" {
		return "", "", "", fmt.Errorf("缺少 server token：请提供 --token 或设置 SENV_SERVER_TOKEN")
	}
	return address, token, vault, nil
}

var migrateToServerCmd = &cobra.Command{
	Use:   "to-server",
	Short: "Migrate the local (git) vault to senv-server",
	Long: `把本地数据仓的全部密文条目（metadata + env/text/config）搬到 server vault。

全程零明文：以密文条目为单位搬运，不需要 vault 口令，口令迁移前后不变。
目标 vault 已有与本地不一致的数据时中止（除非 --force 显式确认覆盖）。
迁移幂等：中断后重跑会跳过已完成条目，直至全部完成。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		address, token, vault, err := resolveServerConn()
		if err != nil {
			return err
		}
		if err := provider.ValidateServerAddress(address); err != nil {
			return err
		}
		sp := provider.NewServerProvider(address, token, getConfigPath(), getDataPath(), vault)

		fmt.Printf("正在迁移到 server vault %q（%s）...\n", vault, address)
		res, err := sp.MigrateToServer(context.Background(), migrateForce)
		if err != nil {
			return err
		}
		printMigrateResult(res, "server")
		fmt.Println()
		fmt.Println("后续：本机切换 provider 可在 settings.json 配置 provider，新机器执行 senv init --server")
		return nil
	},
}

var migrateFromServerCmd = &cobra.Command{
	Use:   "from-server",
	Short: "Migrate a senv-server vault back to the local (git) repository",
	Long: `把 server vault 的全部密文条目落回本地数据仓（git 模式的回滚通道）。

全程零明文：以密文条目为单位搬运，不需要 vault 口令，口令迁移前后不变。
本地已有与远端不一致的数据时中止（除非 --force 显式确认覆盖）。
迁移幂等：中断后重跑会跳过已完成条目，直至全部完成。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		address, token, vault, err := resolveServerConn()
		if err != nil {
			return err
		}
		if err := provider.ValidateServerAddress(address); err != nil {
			return err
		}
		sp := provider.NewServerProvider(address, token, getConfigPath(), getDataPath(), vault)

		fmt.Printf("正在从 server vault %q 迁回本地...\n", vault)
		res, err := sp.MigrateFromServer(context.Background(), migrateForce)
		if err != nil {
			return err
		}
		printMigrateResult(res, "本地")
		return nil
	},
}

// printMigrateResult 输出分类计数与幂等跳过统计
func printMigrateResult(res *provider.MigrateResult, target string) {
	fmt.Printf("✓ 迁移完成（目标: %s）\n", target)
	kinds := make([]string, 0, len(res.Counts))
	for k := range res.Counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	total := 0
	for _, k := range kinds {
		fmt.Printf("  %-12s %d 条\n", k+":", res.Counts[k])
		total += res.Counts[k]
	}
	if res.MetadataMoved {
		fmt.Println("  metadata:    已搬运")
	}
	if res.Skipped > 0 {
		fmt.Printf("  幂等跳过:     %d 条（两端已一致）\n", res.Skipped)
	}
	if res.ExtraKept > 0 {
		fmt.Printf("  目标额外条目: %d 条（已保留）\n", res.ExtraKept)
	}
	fmt.Printf("  合计搬运:     %d 条\n", total)
}
