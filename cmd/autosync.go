package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
)

const (
	autoSyncPullBudget   = 2 * time.Second
	preWritePullThrottle = 2 * time.Second
	autoSyncPushBudget   = 2 * time.Second
	blockingPushBudget   = 10 * time.Second
)

// ErrSyncConflictPending 表示在严格同步模式下，写前检测到远端已更新了本地待推送条目。
var ErrSyncConflictPending = errors.New("存在未解决的远端同步冲突风险")

func isStrictSync() bool {
	if strictSync {
		return true
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SENV_STRICT_SYNC")))
	return v == "1" || v == "true" || v == "yes"
}

// getAutoSyncServerProvider returns nil when automatic sync cannot run. The
// settings check happens before provider.New, so git mode and auto_sync=false
// never construct an HTTP client.
func getAutoSyncServerProvider() (*provider.ServerProvider, error) {
	settings, err := getStorage().LoadSettings()
	if err != nil {
		// Missing/corrupt settings already have the CLI's git-default behavior;
		// automatic sync must not make read commands noisier.
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(settings.Provider.Type), provider.TypeServer) {
		return nil, nil
	}
	if settings.Provider.AutoSync != nil && !*settings.Provider.AutoSync {
		return nil, nil
	}

	p, err := getSyncProvider()
	if err != nil {
		return nil, err
	}
	sp, ok := p.(*provider.ServerProvider)
	if !ok {
		return nil, fmt.Errorf("provider server: 构造结果类型异常 %T", p)
	}
	return sp, nil
}

// exitOnClientBlocked 把「client 被屏蔽」升级为命令失败：本地解锁缓存清理
// 已由 provider 完成，这里保证屏蔽提示与重新注册指引可见，且进程以非零码
// 退出（spec：检测到被屏蔽后命令不得静默成功）。
func exitOnClientBlocked(out io.Writer, err error) {
	if errors.Is(err, provider.ErrClientBlocked) {
		fmt.Fprintf(out, "✗ %v\n", err)
		os.Exit(1)
	}
}

// autoPull performs the bounded, best-effort pre-read pull. Provider and network
// failures are deliberately swallowed so the local cache remains usable.
func autoPull(cmd *cobra.Command, refresh bool) {
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return
	}
	st := perflog.Start("sync.autopull")
	ctx, cancel := context.WithTimeout(commandContext(cmd), autoSyncPullBudget)
	defer cancel()
	res, _, err := sp.AutoPull(ctx, sp.SyncThrottleWindow(), refresh)
	if err != nil {
		st.End(false)
		exitOnClientBlocked(cmd.ErrOrStderr(), err)
		if !errors.Is(err, provider.ErrClientBlocked) {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "auto pull 失败")
		}
		return
	}
	applied := 0
	if res != nil {
		applied = res.Applied
		if res.Applied > 0 || res.MetadataUpdated {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), true, fmt.Sprintf("auto pull %d 条", res.Applied))
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ 已从 server 更新 %d 条\n", res.Applied)
			if res.MetadataUpdated {
				fmt.Fprintln(cmd.ErrOrStderr(), "✓ 已从 server 更新 vault metadata")
			}
		}
	}
	st.With("applied", applied).End(true)
}

// newAutoPuller lets a long-running MCP server reuse the same throttled path on
// each read tool call without tying provider lifecycle to manager construction.
func newAutoPuller(cmd *cobra.Command) func() {
	return func() { autoPull(cmd, false) }
}

// preWriteAutoPull 在写命令执行前做一次短防抖（2s）的 best-effort 增量拉取，
// 降低多端并发写入撞车概率。离线/超时仍允许本地写入；若远端已更新本地待推送
// 条目（SkippedDirty > 0），默认打强警告，--strict-sync / SENV_STRICT_SYNC=1 时早停报错。
func preWriteAutoPull(cmd *cobra.Command, args []string) error {
	if skipPreWritePull(cmd, args) {
		return nil
	}
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return nil
	}
	st := perflog.Start("sync.prewrite-pull")
	ctx, cancel := context.WithTimeout(commandContext(cmd), autoSyncPullBudget)
	defer cancel()
	res, skip, err := sp.AutoPull(ctx, preWritePullThrottle, false)
	if err != nil {
		st.End(false)
		exitOnClientBlocked(cmd.ErrOrStderr(), err)
		if !errors.Is(err, provider.ErrClientBlocked) {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "写前 auto pull 失败")
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 写前拉取失败（%v），将在本地缓存基线上写入\n", err)
		}
		return nil
	}
	if skip == provider.AutoSyncSkipThrottled || skip == provider.AutoSyncSkipLocked {
		st.End(true)
		return nil
	}
	if res != nil {
		if res.Applied > 0 || res.MetadataUpdated {
			auditOp(session.AuditOpSync, "vault:"+syncVaultName(), true, fmt.Sprintf("写前 auto pull %d 条", res.Applied))
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ 写前已从 server 更新 %d 条\n", res.Applied)
		}
		if res.SkippedDirty > 0 {
			st.With("skipped_dirty", res.SkippedDirty).End(false)
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 远端已更新 %d 个本地待推送条目，继续写入可能扩大冲突；建议先运行 senv sync diff 查看差异\n", res.SkippedDirty)
			if isStrictSync() {
				return fmt.Errorf("%w（%d 个待推送条目在远端已更新）", ErrSyncConflictPending, res.SkippedDirty)
			}
			return nil
		}
	}
	st.End(true)
	return nil
}

func skipPreWritePull(cmd *cobra.Command, args []string) bool {
	if skipAutoPush(cmd) {
		return true
	}
	if cmd != nil && cmd.Parent() == nil {
		if len(args) == 0 {
			return true
		}
		if _, _, ok := parseAddress(args[0]); !ok {
			return true
		}
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "init", "migrate", "server", "session", "audit", "update",
			"interactive", "git", "push", "passwd", "sync", "mcp",
			"tui", "doctor", "version", "history", "rebase", "help", "completion",
			"__complete", "__completeNoDesc":
			return true
		}
	}
	return false
}

// postRunAutoPush is the root fallback for write commands. The local dirty scan
// happens first inside AutoPush; clean, disabled, and git paths make no request.
func postRunAutoPush(cmd *cobra.Command) error {
	if skipAutoPush(cmd) {
		return nil
	}
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return nil
	}
	st := perflog.Start("sync.autopush")
	ctx, cancel := context.WithTimeout(commandContext(cmd), maxPushWaitBudget())
	defer cancel()
	// 传 budget<=0 让 AutoPush 按待推送载荷字节数自适应放大预算（2s–15s）。
	out, err := sp.AutoPush(ctx, 0)
	if err == nil || out == nil || out.Skip == provider.AutoSyncSkipClean || out.Skip == provider.AutoSyncSkipLocked {
		st.End(true)
		return nil
	}
	st.End(false)
	exitOnClientBlocked(os.Stderr, err)
	if errors.Is(err, provider.ErrClientBlocked) {
		auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "auto push 被屏蔽拦截")
	} else {
		auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, fmt.Sprintf("auto push %d 条待推送失败", out.Dirty))
	}
	printAutoPushWarning(os.Stderr, out.Dirty, err)
	var conflict *provider.SyncConflictError
	if isStrictSync() && errors.As(err, &conflict) {
		return err
	}
	return nil
}

func maxPushWaitBudget() time.Duration {
	return 15 * time.Second
}

func init() {
	for _, c := range []*cobra.Command{
		aiCatalogStatusCmd,
		aiProviderListCmd, aiProviderShowCmd,
		aiStatusCmd,
		backupGetCmd, backupExportCmd, backupListCmd, backupGroupListCmd,
		configExportCmd, configListCmd, configGetCmd,
		doctorCmd,
		envGetCmd, envListCmd, envExportCmd, envGroupListCmd,
		gitStatusCmd,
		historyCmd,
		ledgerListCmd, ledgerGetCmd, ledgerLogCmd, ledgerTimelineCmd,
		mcpServeCmd, mcpListToolsCmd, mcpGetCmd, mcpListCmd,
		searchCmd,
		sessionStatusCmd,
		skillListCmd, skillShowCmd, skillGroupListCmd,
		keypairListCmd, keypairExportCmd, hostGetCmd, hostListCmd,
		syncCmd,
		textGetCmd, textExportCmd, textListCmd, textGroupListCmd,
		treeGroupListCmd, treeLsCmd, treeGetCmd, treeDiffCmd, treePullCmd,
		tuiCmd,
		versionCmd,
	} {
		markSkipAutoPush(c)
	}
}

func markSkipAutoPush(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations["senv/skip-auto-push"] = "true"
}

func skipAutoPush(cmd *cobra.Command) bool {
	if cmd != nil {
		switch cmd.Name() {
		case "help", "completion", "__complete", "__completeNoDesc":
			return true
		}
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations["senv/skip-auto-push"] == "true" {
			return true
		}
	}
	return false
}

func printAutoPushWarning(out io.Writer, dirty int, err error) {
	var conflict *provider.SyncConflictError
	if errors.As(err, &conflict) {
		items := make([]string, 0, len(conflict.Conflicts))
		for _, c := range conflict.Conflicts {
			items = append(items, fmt.Sprintf("%s/%s/%s", c.Kind, c.Grp, c.Key))
		}
		if conflict.MetadataConflict {
			items = append(items, "vault/metadata")
		}
		fmt.Fprintf(out, "⚠ %d 条推送冲突：%s；运行 senv sync 解决\n", dirty, strings.Join(items, ", "))
		return
	}
	if provider.IsTimeout(err) {
		fmt.Fprintf(out, "⚠ %d 条待推送，同步超时，请运行 senv sync 重试\n", dirty)
		return
	}
	if provider.IsUnreachable(err) {
		fmt.Fprintf(out, "⚠ %d 条待推送，server 暂不可达，恢复后将自动重试\n", dirty)
		return
	}
	fmt.Fprintf(out, "⚠ %d 条待推送，同步失败：%v；请运行 senv sync 重试\n", dirty, err)
}

// pushBlockingAfterCriticalWrite confirms low-frequency, high-impact writes on
// the server before return. The local write is already durable; a sync failure
// is only a warning. The annotation prevents the root fallback from retrying
// the same 10s-budget operation immediately.
func pushBlockingAfterCriticalWrite(cmd *cobra.Command) {
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return
	}
	cmd.Annotations["senv/skip-auto-push"] = "true"
	ctx, cancel := context.WithTimeout(commandContext(cmd), blockingPushBudget)
	defer cancel()
	if pushed, err := sp.PushBlocking(ctx); err != nil {
		exitOnClientBlocked(cmd.ErrOrStderr(), err)
		auditOp(session.AuditOpSync, "vault:"+syncVaultName(), false, "关键更改阻塞推送失败")
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 关键更改已本地生效，但未能同步到 server；其他设备在执行 senv sync 前无法获得此次更改。请运行 senv sync 重试\n")
	} else if pushed != nil {
		auditOp(session.AuditOpSync, "vault:"+syncVaultName(), true, "关键更改阻塞推送完成")
	}
}

func addRefreshFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("refresh", false, "force a server sync before reading (server provider only)")
}

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func refreshRequested(cmd *cobra.Command) bool {
	value, err := cmd.Flags().GetBool("refresh")
	return err == nil && value
}
