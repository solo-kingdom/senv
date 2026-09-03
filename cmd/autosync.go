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
	"github.com/wii/senv/internal/provider"
)

const (
	autoSyncPullBudget = 2 * time.Second
	autoSyncPushBudget = 2 * time.Second
	blockingPushBudget = 10 * time.Second
)

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

// autoPull performs the bounded, best-effort pre-read pull. Provider and network
// failures are deliberately swallowed so the local cache remains usable.
func autoPull(cmd *cobra.Command, refresh bool) {
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(commandContext(cmd), autoSyncPullBudget)
	defer cancel()
	res, _, err := sp.AutoPull(ctx, sp.SyncThrottleWindow(), refresh)
	if err != nil {
		return
	}
	if res != nil && (res.Applied > 0 || res.MetadataUpdated) {
		fmt.Fprintf(cmd.ErrOrStderr(), "✓ 已从 server 更新 %d 条\n", res.Applied)
		if res.MetadataUpdated {
			fmt.Fprintln(cmd.ErrOrStderr(), "✓ 已从 server 更新 vault metadata")
		}
	}
}

// newAutoPuller lets a long-running MCP server reuse the same throttled path on
// each read tool call without tying provider lifecycle to manager construction.
func newAutoPuller(cmd *cobra.Command) func() {
	return func() { autoPull(cmd, false) }
}

// postRunAutoPush is the root fallback for write commands. The local dirty scan
// happens first inside AutoPush; clean, disabled, and git paths make no request.
func postRunAutoPush(cmd *cobra.Command) {
	if skipAutoPush(cmd) {
		return
	}
	sp, err := getAutoSyncServerProvider()
	if err != nil || sp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(commandContext(cmd), autoSyncPushBudget)
	defer cancel()
	out, err := sp.AutoPush(ctx, autoSyncPushBudget)
	if err == nil || out == nil || out.Skip == provider.AutoSyncSkipClean || out.Skip == provider.AutoSyncSkipLocked {
		return
	}
	printAutoPushWarning(os.Stderr, out.Dirty, err)
}

func skipAutoPush(cmd *cobra.Command) bool {
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
	fmt.Fprintf(out, "⚠ %d 条待推送，server 暂不可达，恢复后将自动重试\n", dirty)
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
	if _, err := sp.PushBlocking(ctx); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ 关键更改已本地生效，但未能同步到 server；其他设备在执行 senv sync 前无法获得此次更改。请运行 senv sync 重试\n")
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
