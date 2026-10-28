package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/wii/senv/internal/codexdaemon"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

// Test seams: production probes and restarts the real codex CLI, and asks the
// user through confirmPrompt.
var (
	codexDaemonProbe   = codexdaemon.Probe
	codexDaemonRestart = codexdaemon.Restart
	codexDaemonExpVars = codexDaemonExportVars
	codexDaemonConfirm = confirmPrompt
)

// codexDaemonRestartPrompt 在本次写回命中 codex 时，若本机确实有 codex
// app-server daemon 在跑，就提示并询问是否重启它。
//
// daemon 在启动时拷贝一次环境，之后不再刷新：`senv ai switch codex` 写进
// config.toml 的 env_key、`senv mcp export` / `mcp install` 写入的
// mcp_servers 都要重启才生效。探测不到 daemon 就完全静默——没有 daemon 时
// 没有可重启的东西，提示只会是噪音。
//
// nonInteractive（--yes、`--print`、未接 TTY）不替用户决定重启一个共享进程，
// 只给一行提示。
func codexDaemonRestartPrompt(hitCodex, nonInteractive bool) {
	if !hitCodex {
		return
	}
	status, err := codexDaemonProbe(context.Background())
	if err != nil || !status.Running {
		return
	}
	if nonInteractive || !stdinIsTerminal() {
		fmt.Fprintln(os.Stderr,
			"提示：检测到 codex app-server daemon 在运行，重启后才能读到本次改动：codex app-server daemon restart")
		return
	}
	if !codexDaemonConfirm("检测到 codex app-server daemon 在运行，重启它使改动生效？(y/N): ") {
		fmt.Println("已跳过重启：可稍后手动执行 codex app-server daemon restart")
		return
	}
	if err := codexDaemonRestart(context.Background(), codexDaemonExpVars()); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ codex daemon 重启失败：%v\n", err)
		return
	}
	fmt.Println("✓ 已重启 codex app-server daemon")
}

// codexDaemonExportVars 尽力解析 `senv env export` 的变量集合；拿不到就返回
// nil——缺少注入只影响凭据，不影响重启本身。
//
// 用免交互的 vault 解析：用户刚在确认框里按了 y，再弹一次密码框等于把「重启
// 一下」变成打断。没有会话缓存时这次重启就不注入，重启本身照常进行。
func codexDaemonExportVars() map[string]string {
	envMgr, textMgr, ok := tryGetExportManagers()
	if !ok {
		return nil
	}
	vars, _, err := codexdaemon.ExportVars(envMgr, newRefGetter(envMgr, textMgr))
	if err != nil {
		return nil
	}
	return vars
}

// tryGetExportManagers resolves the env/text managers without prompting for a
// password: it succeeds only off an in-process memo or a valid session cache.
func tryGetExportManagers() (*env.Manager, *text.Manager, bool) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), func(string) (string, error) {
		return "", ErrNeedSession
	})
	if err != nil {
		return nil, nil, false
	}
	if auth.hasKey() {
		return env.NewManagerWithKey(auth.storage, auth.key), text.NewManagerWithKey(auth.storage, auth.key), true
	}
	return env.NewManager(auth.storage, auth.password), text.NewManager(auth.storage, auth.password), true
}
