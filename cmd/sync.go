package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
)

var (
	syncMessage      string
	syncAcceptRemote bool
	syncForcePush    bool
)

// syncCmd 是与配置的远端 provider 双向同步的统一命令：
// git 模式等价于 senv git sync；server 模式走增量 pull + 乐观锁 push。
var syncCmd = &cobra.Command{
	Use:   "sync [-m <message>]",
	Short: "Sync with the configured remote provider",
	Long: `Bidirectional sync with the configured remote provider (settings.json provider).

git provider:    commit local changes, pull --rebase, then push (same as 'senv git sync').
server provider:  incremental pull into the local cache, then push pending changes with
                 per-entry optimistic locking. On conflict nothing is modified on either
                 side; resolve with --accept-remote or --force-push.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := getSyncProvider()
		if err != nil {
			return err
		}

		// server 模式分支
		if sp, ok := p.(*provider.ServerProvider); ok {
			return runServerSync(sp)
		}

		if syncAcceptRemote || syncForcePush {
			return fmt.Errorf("--accept-remote / --force-push 仅适用于 server provider")
		}

		// git 模式：与 senv git sync 行为一致（commit → pull --rebase → push）
		message := syncMessage
		if message == "" {
			message = fmt.Sprintf("Update configurations - %s", time.Now().Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("正在同步（commit → pull --rebase → push）...\n")
		if err := p.Sync(message); err != nil {
			return err
		}
		fmt.Printf("✓ 同步完成\n")
		postPullSelfCheck(getConfigPath(), getDataPath(), os.Stdout)
		return nil
	},
}

func init() {
	syncCmd.Flags().StringVarP(&syncMessage, "message", "m", "", "commit message（git provider）")
	syncCmd.Flags().BoolVar(&syncAcceptRemote, "accept-remote", false, "冲突时放弃本地改动，以远端为准（server provider）")
	syncCmd.Flags().BoolVar(&syncForcePush, "force-push", false, "冲突时放弃远端改动，以本地为准（server provider）")
	rootCmd.AddCommand(syncCmd)
}

// runServerSync server 模式同步：冲突解决标志优先，否则正常双向同步
func runServerSync(sp *provider.ServerProvider) error {
	ctx := context.Background()

	if syncAcceptRemote && syncForcePush {
		return fmt.Errorf("--accept-remote 与 --force-push 不能同时使用")
	}
	if syncAcceptRemote {
		fmt.Println("以远端为准覆盖本地改动...")
		if err := sp.AcceptRemote(ctx); err != nil {
			return err
		}
		fmt.Println("✓ 已以远端为准完成同步")
		return nil
	}
	if syncForcePush {
		fmt.Println("以本地为准覆盖远端改动...")
		if err := sp.ForcePush(ctx); err != nil {
			return err
		}
		fmt.Println("✓ 已以本地为准完成同步")
		return nil
	}

	fmt.Println("正在与 server 同步（pull → push）...")
	res, err := sp.SyncWithReport(ctx)
	if err != nil {
		var conflictErr *provider.SyncConflictError
		if errors.As(err, &conflictErr) {
			// 冲突清单与解决指引由 Error() 完整给出；两端数据均未改动
			return err
		}
		return err
	}
	if res.Dirty == 0 && res.Pull.Applied == 0 && !res.Pull.MetadataUpdated && !res.Push.MetadataPushed {
		fmt.Println("✓ 已是最新，无需同步")
		return nil
	}
	if res.Pull.Applied > 0 || res.Pull.MetadataUpdated {
		fmt.Printf("✓ 拉取 %d 条远端更改\n", res.Pull.Applied)
	}
	if res.Pull.SkippedDirty > 0 {
		fmt.Printf("  （%d 条本地待推送条目跳过覆盖，由推送阶段判定）\n", res.Pull.SkippedDirty)
	}
	if res.Push.Pushed > 0 || res.Push.MetadataPushed {
		fmt.Printf("✓ 推送 %d 条本地更改\n", res.Push.Pushed)
	}
	fmt.Println("✓ 同步完成")
	return nil
}
