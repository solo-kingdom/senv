package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wii/senv/internal/codexdaemon"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

// Test seams: production probes and restarts the real codex CLI, and asks the
// user through confirmPromptDefaultYes (空输入=重启).
var (
	codexDaemonProbe   = codexdaemon.Probe
	codexDaemonRestart = codexdaemon.Restart
	codexDaemonExpVars = codexDaemonExportVars
	codexDaemonConfirm = confirmPromptDefaultYes
	codexDaemonMissing = codexdaemon.MissingEnvInDaemon
)

// codexDaemonRestartPrompt 在本次写回命中 codex 时，若本机确实有 codex
// app-server daemon 在跑，就提示并询问是否重启它。
//
// daemon 在启动时拷贝一次环境，之后不再刷新：`senv ai switch codex` 写进
// config.toml 的 env_key、`senv mcp export` / `mcp install` 写入的
// mcp_servers 都要重启才生效。探测不到 daemon 就完全静默——没有 daemon 时
// 没有可重启的东西，提示只会是噪音。
//
// requiredEnvKey 非空时（ai switch 写入的 env_key），跳过/非交互提示会点名
// 该变量，并在重启前后核对 export 注入与 daemon 进程环境，避免「切换成功、
// TUI 仍 Missing environment variable」。
//
// nonInteractive（--yes、`--print`、未接 TTY）不替用户决定重启一个共享进程，
// 只给一行提示。
func codexDaemonRestartPrompt(hitCodex, nonInteractive bool, requiredEnvKey string) {
	if !hitCodex {
		return
	}
	status, err := codexDaemonProbe(context.Background())
	if err != nil || !status.Running {
		return
	}
	envKey := strings.TrimSpace(requiredEnvKey)
	if nonInteractive || !stdinIsTerminal() {
		if envKey != "" {
			fmt.Fprintf(os.Stderr,
				"提示：检测到 codex app-server daemon 在运行；不重启则仍可能报 Missing environment variable: `%s`。请执行：codex app-server daemon restart（须在能 `senv env export` 出该变量的环境里）\n",
				envKey)
			return
		}
		fmt.Fprintln(os.Stderr,
			"提示：检测到 codex app-server daemon 在运行，重启后才能读到本次改动：codex app-server daemon restart")
		return
	}
	prompt := "检测到 codex app-server daemon 在运行，重启它使改动生效？(Y/n): "
	if envKey != "" {
		prompt = fmt.Sprintf(
			"检测到 codex app-server daemon 在运行；不重启则 Codex 仍会报 Missing environment variable: `%s`。现在重启？(Y/n): ",
			envKey)
	}
	if !codexDaemonConfirm(prompt) {
		if envKey != "" {
			fmt.Fprintf(os.Stderr,
				"⚠ 已跳过重启：Codex TUI 仍可能报 Missing environment variable: `%s`。可稍后在已 `eval \"$(senv env export)\"` 的 shell 执行：codex app-server daemon restart\n",
				envKey)
			return
		}
		fmt.Println("已跳过重启：可稍后手动执行 codex app-server daemon restart")
		return
	}
	vars := filterCodexDaemonVars(codexDaemonExpVars(), envKey)
	if envKey != "" {
		if vars == nil || vars[envKey] == "" {
			fmt.Fprintf(os.Stderr,
				"⚠ 即将注入的 `senv env export` 集合缺少 %s：重启后 daemon 仍可能报 Missing environment variable；请确认 vault 可解密且该名字在激活组中\n",
				envKey)
		}
	}
	if err := codexDaemonRestart(context.Background(), vars); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ codex daemon 重启失败：%v\n", err)
		return
	}
	fmt.Println("✓ 已重启 codex app-server daemon")
	if envKey == "" {
		return
	}
	if missing, checked := codexDaemonMissing([]string{envKey}); checked && len(missing) > 0 {
		fmt.Fprintf(os.Stderr,
			"⚠ 重启后 daemon 环境仍缺少 %s：请在已 export 的 shell 再执行 codex app-server daemon restart，或检查 `senv env export` 是否输出该名字\n",
			strings.Join(missing, ", "))
	}
}

// confirmPromptDefaultYes 与 confirmPrompt 同形，但空输入视为 y——写回
// codex 后默认建议重启，跳过才需要显式 n。
func confirmPromptDefaultYes(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	var answer string
	_, _ = fmt.Scanln(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func isDangerousDaemonEnvKey(k string) bool {
	upper := strings.ToUpper(strings.TrimSpace(k))
	if strings.HasPrefix(upper, "LD_") || strings.HasPrefix(upper, "DYLD_") {
		return true
	}
	switch upper {
	case "BASH_ENV", "ENV", "NODE_OPTIONS", "PYTHONSTARTUP", "PYTHONPATH", "RUBYOPT", "PERL5OPT":
		return true
	default:
		return false
	}
}

func filterCodexDaemonVars(vars map[string]string, requiredEnvKey string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	if requiredEnvKey != "" {
		if isDangerousDaemonEnvKey(requiredEnvKey) {
			return map[string]string{}
		}
		if v, ok := vars[requiredEnvKey]; ok {
			return map[string]string{requiredEnvKey: v}
		}
		return map[string]string{}
	}
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		if isDangerousDaemonEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
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
