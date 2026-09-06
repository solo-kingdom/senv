package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var (
	historyLimit     int
	historyRestore   int64
	historyAssumeYes bool
)

var historyCmd = &cobra.Command{
	Use:   "history [kind:group:key]",
	Short: "查看配置条目的历史版本（server 模式）",
	Long: `查看 server 端为条目保留的最近密文历史（默认 3 版，由 server 端 --history-retain 配置）。

  senv history                        列出 vault 级最近历史变更（含日期时间）
  senv history env:deploy:API_KEY     查看单条目版本列表（本地解密，新到旧）
  senv history env:deploy:API_KEY --restore 5   恢复到 revision 5（交互确认）

恢复 = 把历史值写回本地并走既有推送流程，会产生新 revision；
已删除条目可借此找回。仅 server 模式支持；git 模式请用 git log。`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "返回的历史版本条数")
	historyCmd.Flags().Int64Var(&historyRestore, "restore", 0, "恢复到指定 revision（需条目参数）")
	historyCmd.Flags().BoolVar(&historyAssumeYes, "yes", false, "恢复时跳过确认")
	rootCmd.AddCommand(historyCmd)
}

// getServerProvider 构造 provider 并要求为 server 类型
func getServerProvider() (*provider.ServerProvider, error) {
	p, err := getSyncProvider()
	if err != nil {
		return nil, err
	}
	sp, ok := p.(*provider.ServerProvider)
	if !ok {
		return nil, fmt.Errorf("仅 server 模式支持历史查看；git 模式请用 git log 查看历史")
	}
	return sp, nil
}

func runHistory(cmd *cobra.Command, args []string) error {
	sp, err := getServerProvider()
	if err != nil {
		return err
	}
	var filter provider.HistoryFilter
	if len(args) > 0 {
		kind, grp, key, ok := parseEntryAddress(args[0])
		if !ok {
			return fmt.Errorf("条目参数需为 kind:group:key 三段形式（如 env:deploy:API_KEY）")
		}
		filter = provider.HistoryFilter{Kind: kind, Grp: grp, Key: key, Limit: historyLimit}
	} else {
		filter = provider.HistoryFilter{Limit: historyLimit}
	}

	history, err := sp.History(context.Background(), filter)
	if err != nil {
		if errors.Is(err, provider.ErrVaultNotFound) {
			return fmt.Errorf("server 上不存在 vault（先同步一次再查看历史）")
		}
		return err
	}
	if len(history) == 0 {
		fmt.Println("（无历史版本：条目未被修改过，或 server 未开启历史留存）")
		return nil
	}

	// 解密需要 vault 口令派生 key（config_index 条目本身是明文 JSON）
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return err
	}
	key, err := resolveKeyForAuth(auth)
	if err != nil {
		return err
	}

	if historyRestore != 0 {
		if len(args) == 0 {
			return fmt.Errorf("--restore 需要条目参数（kind:group:key）")
		}
		return restoreHistoryVersion(sp, history, key)
	}

	fmt.Printf("%-20s %-8s %-6s %s\n", "时间", "kind", "rev", "值/内容")
	for _, h := range history {
		fmt.Printf("%-20s %-8s %-6d %s\n",
			h.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			h.Kind, h.Revision, historyDisplay(h, key))
	}
	return nil
}

// parseEntryAddress 解析 kind:group:key 三段地址
func parseEntryAddress(s string) (kind, grp, key string, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// historyDisplay 解密并渲染历史版本内容；解密失败明确提示而非中断
func historyDisplay(h provider.HistoryVersion, key []byte) string {
	if h.Deleted {
		return "(墓碑记录，无可恢复内容)"
	}
	plaintext, err := decryptHistoryValue(h, key)
	if err != nil {
		return fmt.Sprintf("<无法解密该版本: %v>", err)
	}
	return renderDecryptedHistory(h.Kind, plaintext)
}

// renderDecryptedHistory 按条目类型渲染已解密的历史内容
func renderDecryptedHistory(kind string, plaintext []byte) string {
	switch kind {
	case "env":
		var entry storage.EnvVarEntry
		if err := json.Unmarshal(plaintext, &entry); err == nil {
			return entry.Value
		}
		return contentPreviewString(plaintext)
	case "text":
		var entry storage.TextEntry
		if err := json.Unmarshal(plaintext, &entry); err == nil {
			return entry.Value
		}
		return contentPreviewString(plaintext)
	case "env_meta":
		var meta storage.EnvGroupMeta
		if err := json.Unmarshal(plaintext, &meta); err == nil {
			return meta.Name
		}
		return contentPreviewString(plaintext)
	default:
		return contentPreviewString(plaintext)
	}
}

// decryptHistoryValue 按条目类型解密：config_index 同步条目本身是明文 JSON
func decryptHistoryValue(h provider.HistoryVersion, key []byte) ([]byte, error) {
	if h.Kind == "config_index" {
		return h.Ciphertext, nil
	}
	return crypto.Decrypt(key, string(h.Ciphertext))
}

func contentPreviewString(data []byte) string {
	if !utf8.Valid(data) {
		return fmt.Sprintf("<二进制内容，%d 字节>", len(data))
	}
	const max = 512
	if len(data) > max {
		return string(data[:max]) + fmt.Sprintf("…（共 %d 字节）", len(data))
	}
	return string(data)
}

// restoreHistoryVersion 从已拉取的历史中找到目标 revision，确认后恢复
func restoreHistoryVersion(sp *provider.ServerProvider, history []provider.HistoryVersion, key []byte) error {
	var chosen *provider.HistoryVersion
	for i := range history {
		if history[i].Revision == historyRestore {
			chosen = &history[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("历史中不存在 revision %d（先用不带 --restore 的命令查看可用版本）", historyRestore)
	}
	fmt.Printf("将恢复 %s:%s:%s 到 revision %d（%s）：\n%s\n",
		chosen.Kind, chosen.Grp, chosen.Key, chosen.Revision,
		chosen.CreatedAt.Local().Format("2006-01-02 15:04:05"),
		historyDisplay(*chosen, key))
	if !historyAssumeYes {
		fmt.Print("确认恢复？[y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("已取消")
			return nil
		}
	}
	if err := sp.RestoreEntry(context.Background(), chosen.Kind, chosen.Grp, chosen.Key, chosen.Ciphertext); err != nil {
		auditOp(session.AuditOpRestore, chosen.Kind+":"+chosen.Grp+":"+chosen.Key, false, "恢复失败")
		return err
	}
	auditOp(session.AuditOpRestore, chosen.Kind+":"+chosen.Grp+":"+chosen.Key, true,
		fmt.Sprintf("恢复到 revision %d", chosen.Revision))
	fmt.Println("✓ 已恢复并推送（产生新 revision）")
	return nil
}
