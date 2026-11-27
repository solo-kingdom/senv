package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
)

var (
	syncMessage          string
	syncAcceptRemote     bool
	syncForcePush        bool
	syncNoInteractive    bool
	syncResolve          []string
	syncResolveDefault   string
	syncNoHealEquivalent bool
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
			return runServerSync(cmd, sp)
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
			auditOp(session.AuditOpSync, "git", false, "git sync 失败")
			return err
		}
		auditOp(session.AuditOpSync, "git", true, "git sync")
		fmt.Printf("✓ 同步完成\n")
		postPullSelfCheck(getConfigPath(), getDataPath(), os.Stdout)
		return nil
	},
}

func init() {
	syncCmd.Flags().StringVarP(&syncMessage, "message", "m", "", "commit message（git provider）")
	syncCmd.Flags().BoolVar(&syncAcceptRemote, "accept-remote", false, "冲突时放弃本地改动，以远端为准（server provider）")
	syncCmd.Flags().BoolVar(&syncForcePush, "force-push", false, "冲突时放弃远端改动，以本地为准（server provider）")
	syncCmd.Flags().BoolVar(&syncNoInteractive, "no-interactive", false, "冲突时禁止交互式解决器，仅输出报告")
	syncCmd.Flags().StringSliceVar(&syncResolve, "resolve", nil, "非交互按条目裁决冲突，格式 <selector>=<local|remote>（可多次指定）")
	syncCmd.Flags().StringVar(&syncResolveDefault, "resolve-default", "", "未匹配 --resolve 规则的冲突条目默认裁决（local|remote）")
	syncCmd.Flags().BoolVar(&syncNoHealEquivalent, "no-heal-equivalent", false, "禁用客户端内容等价假冲突自动收养")
	rootCmd.AddCommand(syncCmd)
}

// runServerSync server 模式同步：冲突解决标志优先，否则正常双向同步
func runServerSync(cmd *cobra.Command, sp *provider.ServerProvider) error {
	ctx := context.Background()
	target := "vault:" + syncVaultName()

	if syncAcceptRemote && syncForcePush {
		return fmt.Errorf("--accept-remote 与 --force-push 不能同时使用")
	}
	if (syncAcceptRemote || syncForcePush) && (len(syncResolve) > 0 || syncResolveDefault != "") {
		return fmt.Errorf("--accept-remote / --force-push 不能与 --resolve / --resolve-default 同时使用")
	}
	rules, defaultDecision, err := parseResolveFlags(syncResolve, syncResolveDefault)
	if err != nil {
		return err
	}
	if syncAcceptRemote {
		fmt.Println("以远端为准覆盖本地改动...")
		if err := sp.AcceptRemote(ctx); err != nil {
			auditOp(session.AuditOpSync, target, false, "accept-remote 失败")
			return err
		}
		auditOp(session.AuditOpSync, target, true, "accept-remote")
		fmt.Println("✓ 已以远端为准完成同步")
		return nil
	}
	if syncForcePush {
		fmt.Println("以本地为准覆盖远端改动...")
		if err := sp.ForcePush(ctx); err != nil {
			auditOp(session.AuditOpSync, target, false, "force-push 失败")
			return err
		}
		auditOp(session.AuditOpSync, target, true, "force-push")
		fmt.Println("✓ 已以本地为准完成同步")
		return nil
	}

	fmt.Println("正在与 server 同步（pull → push）...")
	res, err := sp.SyncWithReport(ctx)
	if err != nil {
		var conflictErr *provider.SyncConflictError
		if errors.As(err, &conflictErr) {
			var remoteMeta []byte
			if conflictErr.Metadata != nil {
				remoteMeta = conflictErr.Metadata.Remote
			}
			auth := cachedConflictAuth(remoteMeta)

			if !syncNoHealEquivalent {
				healedCount, remaining, healErr := tryHealEquivalentConflicts(ctx, sp, conflictErr, auth)
				if healErr != nil {
					return healErr
				}
				if healedCount > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "✓ 已自动收养 %d 条内容等价冲突（仅密文/压缩格式差异）\n", healedCount)
					if remaining == nil {
						auditOp(session.AuditOpConflict, target, true, fmt.Sprintf("自动收养 %d 项等价冲突", healedCount))
						return finishSyncAfterResolution(ctx, cmd.OutOrStdout(), target, sp)
					}
					conflictErr = remaining
				}

				if mergedIdx, remAfterMerge, mergeErr := tryMergeSkillIndexConflict(ctx, sp, conflictErr, auth); mergeErr != nil {
					return mergeErr
				} else if mergedIdx {
					fmt.Fprintln(cmd.OutOrStdout(), "✓ 已自动三方合并 skill_index（无字段冲突）")
					if remAfterMerge == nil {
						auditOp(session.AuditOpConflict, target, true, "自动三方合并 skill_index")
						return finishSyncAfterResolution(ctx, cmd.OutOrStdout(), target, sp)
					}
					conflictErr = remAfterMerge
				}
			}

			if len(rules) > 0 || defaultDecision != "" {
				plan, planErr := buildResolvePlan(conflictErr, rules, defaultDecision, auth)
				if planErr != nil {
					auditOp(session.AuditOpSync, target, false, "resolve 计划校验失败")
					return planErr
				}
				if rerr := sp.ResolveConflicts(ctx, plan); rerr != nil {
					auditOp(session.AuditOpConflict, target, false, "resolve 执行失败")
					return rerr
				}
				auditOp(session.AuditOpConflict, target, true, fmt.Sprintf("非交互解决 %d 项冲突", len(plan.Items)))
				fmt.Fprintf(cmd.OutOrStdout(), "✓ 已按规则解决 %d 项冲突\n", len(plan.Items))
				return finishSyncAfterResolution(ctx, cmd.OutOrStdout(), target, sp)
			}

			auditOp(session.AuditOpSync, target, false, formatSyncConflictAuditMessage(conflictErr))
			if syncConflictResolverAvailable() {
				return runSyncConflictResolver(cmd, sp, conflictErr)
			}
			// 非交互环境输出安全对比摘要；错误保留给脚本判定退出码。
			{
				writeSyncConflictReport(cmd.ErrOrStderr(), conflictErr)
			}
			return err
		}
		auditOp(session.AuditOpSync, target, false, "同步失败")
		return err
	}
	auditOp(session.AuditOpSync, target, true,
		fmt.Sprintf("pull %d 条 / push %d 条", res.Pull.Applied, res.Push.Pushed))
	writeSyncSuccessReport(os.Stdout, res)
	return nil
}

// finishSyncAfterResolution 在冲突收养/三方合并/逐条裁决完成后，继续完成同批
// 剩余非冲突 dirty 条目的推送与远端拉取。
func finishSyncAfterResolution(ctx context.Context, out io.Writer, target string, sp *provider.ServerProvider) error {
	res, err := sp.SyncWithReport(ctx)
	if err != nil {
		return err
	}
	auditOp(session.AuditOpSync, target, true,
		fmt.Sprintf("冲突收敛后续推 pull %d 条 / push %d 条", res.Pull.Applied, res.Push.Pushed))
	writeSyncSuccessReport(out, res)
	return nil
}

// syncVaultName 返回当前 server vault 名（审计 target 用）
func syncVaultName() string {
	if settings, err := getStorage().LoadSettings(); err == nil && settings.Provider.Vault != "" {
		return settings.Provider.Vault
	}
	return "main"
}

// writeSyncSuccessReport 输出双向同步成功报告；自愈发生时低噪声提示修复数量。
func writeSyncSuccessReport(out io.Writer, res *provider.SyncResult) {
	if res.Dirty == 0 && res.Pull.Applied == 0 && !res.Pull.MetadataUpdated && !res.Push.MetadataPushed && res.Healed == 0 {
		fmt.Fprintln(out, "✓ 已是最新，无需同步")
		return
	}
	if res.Healed > 0 {
		fmt.Fprintf(out, "✓ 已自动修复 %d 条同步状态（快照/哈希收养，两端数据未变）\n", res.Healed)
	}
	if res.Pull.Applied > 0 || res.Pull.MetadataUpdated {
		fmt.Fprintf(out, "✓ 拉取 %d 条远端更改\n", res.Pull.Applied)
	}
	if res.Pull.SkippedDirty > 0 {
		fmt.Fprintf(out, "  （%d 条本地待推送条目跳过覆盖，由推送阶段判定）\n", res.Pull.SkippedDirty)
	}
	if res.Push.Pushed > 0 || res.Push.MetadataPushed {
		fmt.Fprintf(out, "✓ 推送 %d 条本地更改\n", res.Push.Pushed)
	}
	fmt.Fprintln(out, "✓ 同步完成")
}

func syncConflictResolverAvailable() bool {
	return !syncNoInteractive && stdinIsTerminal() && stdoutIsTerminal()
}

func formatConflictTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatConflictHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func writeConflictSide(w io.Writer, label string, side provider.ConflictSide) {
	fmt.Fprintf(w, "    %-6s revision=%d deleted=%v size=%d hash=%s updated=%s\n",
		label, side.Revision, side.Deleted, side.Size,
		formatConflictHash(side.Hash), formatConflictTime(side.UpdatedAt))
}

func writeSyncConflictReport(w io.Writer, conflict *provider.SyncConflictError) {
	fmt.Fprintf(w, "\n同步中止：%d 个条目在远端已被更新（两端数据均未改动）\n", len(conflict.Conflicts))
	for i, detail := range conflict.Details {
		fmt.Fprintf(w, "  %d. %s/%s/%s\n", i+1, detail.Kind,
			displayConflictGroup(detail.Grp), displayConflictKey(detail.Kind, detail.Key))
		writeConflictSide(w, "local", detail.Local)
		writeConflictSide(w, "remote", detail.Remote)
		if isConfigSourceKind(detail.Kind) {
			fmt.Fprintf(w, "    ⚠ 配置源 %s %s 双端均有修改：local rev %d / remote rev %d，请人工核对后选择保留哪一侧\n",
				detail.Kind, displayConflictKey(detail.Kind, detail.Key), detail.Local.Revision, detail.Remote.Revision)
		}
	}
	// 旧 server / 极端缺失 detail 时仍保证冲突条目不会从报告中消失。
	if len(conflict.Details) == 0 {
		for _, c := range conflict.Conflicts {
			fmt.Fprintf(w, "  - %s/%s/%s remote_revision=%d\n", c.Kind,
				displayConflictGroup(c.Grp), displayConflictKey(c.Kind, c.Key), c.CurrentRevision)
		}
	}
	if conflict.MetadataConflict {
		fmt.Fprintln(w, "  M. vault metadata（本地与远端均已修改；只允许整体选择一边）")
	}
	fmt.Fprintln(w, "\n解决方式:")
	fmt.Fprintln(w, "  senv sync diff             查看冲突明细（skill 文件差异 / 索引碰撞）")
	fmt.Fprintln(w, "  senv sync --resolve ...    非交互按条目裁决（如 skill:on-demand:*=local）")
	fmt.Fprintln(w, "  senv sync --accept-remote  放弃全部本地改动，以远端为准")
	fmt.Fprintln(w, "  senv sync --force-push     放弃全部远端改动，以本地为准")
}

// isConfigSourceKind 报告 kind 是否为人工添加、跨机分发的配置源档案；
// 这类条目的冲突更可能是有意义的双端修改，报告需额外提示人工核对。
func isConfigSourceKind(kind string) bool {
	switch kind {
	case provider.KindLLMProvider, provider.KindMCPServer,
		provider.KindSSHHost, provider.KindSSHKeypair:
		return true
	}
	return false
}

// formatSyncConflictAuditMessage 生成审计用的冲突摘要；含配置源冲突时显式标注数量。
func formatSyncConflictAuditMessage(conflict *provider.SyncConflictError) string {
	source := 0
	for _, c := range conflict.Conflicts {
		if isConfigSourceKind(c.Kind) {
			source++
		}
	}
	if source == 0 {
		return fmt.Sprintf("同步冲突 %d 项", len(conflict.Conflicts))
	}
	return fmt.Sprintf("同步冲突 %d 项（含配置源 %d 项）", len(conflict.Conflicts), source)
}

func displayConflictGroup(group string) string {
	if group == "" {
		return "-"
	}
	return group
}

func displayConflictKey(kind, key string) string {
	if key == "" {
		return "(index/meta)"
	}
	return key
}
