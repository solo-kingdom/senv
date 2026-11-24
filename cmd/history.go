package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	historyMetadata  bool
	historyReveal    bool
)

var historyCmd = &cobra.Command{
	Use:   "history [kind:group:key]",
	Short: "查看配置条目的历史版本（server 模式）",
	Long: `查看 server 端为条目保留的最近密文历史（默认 3 版，由 server 端 --history-retain 配置）。

  senv history                        列出 vault 级最近历史变更（默认遮蔽敏感值）
  senv history env:deploy:API_KEY     查看单条目版本列表（默认遮蔽敏感值）
  senv history env:deploy:API_KEY --reveal      查看单条目明文内容（SSH 私钥永不输出）
  senv history env:deploy:API_KEY --restore 5   恢复到 revision 5（交互确认）

恢复 = 把历史值写回本地并走既有推送流程，会产生新 revision；
已删除条目可借此找回。仅 server 模式支持；git 模式请用 git log。`,
	Args: cobra.MaximumNArgs(1),
	RunE: runHistory,
}

func init() {
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "返回的历史版本条数")
	historyCmd.Flags().Int64Var(&historyRestore, "restore", 0, "恢复到指定 revision（条目）或序号（--metadata，1=最近一版）")
	historyCmd.Flags().BoolVar(&historyAssumeYes, "yes", false, "恢复时跳过确认")
	historyCmd.Flags().BoolVar(&historyMetadata, "metadata", false, "查看 vault metadata 的历史版本（覆盖写前像）")
	historyCmd.Flags().BoolVar(&historyReveal, "reveal", false, "仅单条目查询可用：展开显示非私钥历史明文")
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
	if historyMetadata {
		if len(args) > 0 {
			return fmt.Errorf("--metadata 不接受条目参数（metadata 属于整个 vault）")
		}
		return runHistoryMetadata(sp)
	}
	var filter provider.HistoryFilter
	if len(args) > 0 {
		kind, grp, key, ok := parseEntryAddress(args[0])
		if !ok {
			return fmt.Errorf("条目参数需为 kind:group:key 三段形式（如 env:deploy:API_KEY）")
		}
		filter = provider.HistoryFilter{Kind: kind, Grp: grp, Key: key, Limit: historyLimit}
	} else {
		if historyReveal {
			return fmt.Errorf("--reveal 仅适用于单条目查询（需传入 kind:group:key 参数）")
		}
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
	defer session.ZeroKey(key)

	keyEpoch := 0
	if md, mdErr := auth.storage.LoadMetadata(); mdErr == nil && md != nil {
		keyEpoch = md.KeyEpoch
	}

	if historyRestore != 0 {
		if len(args) == 0 {
			return fmt.Errorf("--restore 需要条目参数（kind:group:key）")
		}
		return restoreHistoryVersion(sp, history, key)
	}

	fmt.Printf("%-20s %-12s %-8s %-6s %s\n", "变更时间", "操作者", "kind", "rev", "值/内容")
	for _, h := range history {
		when := h.ReplacedAt
		if when.IsZero() {
			when = h.CreatedAt
		}
		fmt.Printf("%-20s %-12s %-8s %-6d %s\n",
			when.Local().Format("2006-01-02 15:04:05"),
			historyActor(h), h.Kind, h.Revision, historyDisplayWithEpoch(h, key, keyEpoch, historyReveal))
	}
	return nil
}

// historyActor 渲染服务端认定的操作者；旧版本无归因时显示 "-"。
func historyActor(h provider.HistoryVersion) string {
	if h.ActorClientName != "" {
		return h.ActorClientName
	}
	if h.ActorClientID > 0 {
		return fmt.Sprintf("client#%d", h.ActorClientID)
	}
	return "-"
}

// parseEntryAddress 解析 kind:group:key 三段地址
func parseEntryAddress(s string) (kind, grp, key string, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// historyDisplay 解密并渲染历史版本内容；默认遮蔽敏感值，仅单条目 --reveal 展开非私钥明文。
func historyDisplay(h provider.HistoryVersion, key []byte, reveal bool) string {
	return historyDisplayWithEpoch(h, key, 16, reveal)
}

func historyDisplayWithEpoch(h provider.HistoryVersion, key []byte, keyEpoch int, reveal bool) string {
	if h.Deleted {
		return "(墓碑记录，无可恢复内容)"
	}
	plaintext, err := decryptHistoryValueWithEpoch(h, key, keyEpoch)
	if err != nil {
		return fmt.Sprintf("<无法解密该版本: %v>", err)
	}
	if reveal {
		return renderRevealedHistory(h.Kind, plaintext)
	}
	return renderMaskedHistory(h.Kind, plaintext)
}

func renderMaskedHistory(kind string, plaintext []byte) string {
	switch kind {
	case "ssh_keypair":
		return renderSSHKeypairSummary(plaintext)
	case "skill":
		return fmt.Sprintf("<skill 归档，%d 字节>", len(plaintext))
	case "env_meta", "text_meta", "backup_meta", "ledger_meta", "tree_meta":
		var meta storage.EnvGroupMeta
		if err := json.Unmarshal(plaintext, &meta); err == nil {
			return meta.Name
		}
	}
	return fmt.Sprintf("********（%d 字节，单条目加 --reveal 查看明文）", len(plaintext))
}

func renderSSHKeypairSummary(plaintext []byte) string {
	var kp storage.KeyPairEntry
	if err := json.Unmarshal(plaintext, &kp); err == nil {
		fp := kp.Fingerprint
		if fp == "" {
			fp = "-"
		}
		grp := kp.Group
		if grp == "" {
			grp = "_ungrouped"
		}
		return fmt.Sprintf("<ssh_keypair · 私钥已隐藏 · group=%s fingerprint=%s>", grp, fp)
	}
	return "<ssh_keypair · 私钥已隐藏>"
}

// renderRevealedHistory 按条目类型渲染已解密的历史内容（ssh_keypair 永不输出私钥）。
func renderRevealedHistory(kind string, plaintext []byte) string {
	switch kind {
	case "ssh_keypair":
		return renderSSHKeypairSummary(plaintext)
	case "env":
		var entry storage.EnvVarEntry
		if err := json.Unmarshal(plaintext, &entry); err == nil {
			return entry.Value
		}
		return contentPreviewString(plaintext)
	case "text", "backup":
		var entry storage.TextEntry
		if err := json.Unmarshal(plaintext, &entry); err == nil {
			return entry.Value
		}
		return contentPreviewString(plaintext)
	case "skill":
		// skill 正文是 tar 归档，只显示大小，不展开内容。
		return fmt.Sprintf("<skill 归档，%d 字节>", len(plaintext))
	case "tree":
		var entry struct {
			Mode     uint32 `json:"mode"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(plaintext, &entry); err != nil {
			return contentPreviewString(plaintext)
		}
		if entry.Encoding == "base64" {
			return fmt.Sprintf("<二进制文件，mode %04o，%d 字符 base64>", entry.Mode, len(entry.Content))
		}
		return entry.Content
	case "env_meta", "text_meta", "backup_meta", "ledger_meta", "tree_meta":
		var meta storage.EnvGroupMeta
		if err := json.Unmarshal(plaintext, &meta); err == nil {
			return meta.Name
		}
		return contentPreviewString(plaintext)
	default:
		return contentPreviewString(plaintext)
	}
}

// renderDecryptedHistory 保留供 TUI 按需展开与现有测试调用。
func renderDecryptedHistory(kind string, plaintext []byte) string {
	return renderRevealedHistory(kind, plaintext)
}

// decryptHistoryValue 按条目类型解密：未加密的旧版索引直接返回明文；v2 密文按
// (kind, grp, key, keyEpoch) 校验 AAD，兼容当前及更早 epoch 与 v1 密文。
func decryptHistoryValue(h provider.HistoryVersion, key []byte) ([]byte, error) {
	return decryptHistoryValueWithEpoch(h, key, 16)
}

func decryptHistoryValueWithEpoch(h provider.HistoryVersion, key []byte, maxEpoch int) ([]byte, error) {
	if (h.Kind == "config_index" || h.Kind == "skill_index") && !crypto.IsV2Ciphertext(string(h.Ciphertext)) {
		return h.Ciphertext, nil
	}
	if !crypto.IsV2Ciphertext(string(h.Ciphertext)) {
		return crypto.Decrypt(key, string(h.Ciphertext))
	}
	if maxEpoch < 0 {
		maxEpoch = 0
	}
	for e := maxEpoch; e >= 0; e-- {
		aad := crypto.BuildEntryAAD(h.Kind, h.Grp, h.Key, e)
		if plain, err := crypto.DecryptWithAAD(key, string(h.Ciphertext), aad); err == nil {
			return plain, nil
		}
	}
	return nil, crypto.ErrDecryptionFailed
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

// runHistoryMetadata 查看 / 恢复 vault metadata 历史（覆盖写前像）。
// metadata 决定 vault 能否解锁，恢复必须显式确认；序号 1 = 最近一版。
func runHistoryMetadata(sp *provider.ServerProvider) error {
	versions, err := sp.MetadataHistory(context.Background(), historyLimit)
	if err != nil {
		if errors.Is(err, provider.ErrVaultNotFound) {
			return fmt.Errorf("server 上不存在 vault（先同步一次再查看历史）")
		}
		return err
	}
	if len(versions) == 0 {
		fmt.Println("（无 metadata 历史：未被覆盖过，或 server 未开启历史留存）")
		return nil
	}

	if historyRestore != 0 {
		idx := int(historyRestore)
		if idx < 1 || idx > len(versions) {
			return fmt.Errorf("--restore 需为 1..%d（1 = 最近一版）", len(versions))
		}
		v := versions[idx-1]
		fmt.Printf("将用 metadata 历史 #%d（替换时间 %s，%d 字节，sha256 %s）覆盖当前值。\n",
			idx, v.ReplacedAt.Local().Format("2006-01-02 15:04:05"), len(v.Blob), shortHash(v.Blob))
		fmt.Println("注意：metadata 决定所有设备的解锁，恢复后各端需重新同步。")
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
		if err := sp.RestoreMetadata(context.Background(), v.Blob); err != nil {
			auditOp(session.AuditOpRestore, "metadata", false, "metadata 恢复失败")
			return err
		}
		auditOp(session.AuditOpRestore, "metadata", true, fmt.Sprintf("恢复到历史 #%d", idx))
		fmt.Println("✓ metadata 已恢复并推送（产生新版本）")
		return nil
	}

	fmt.Printf("%-4s %-20s %-10s %s\n", "#", "替换时间", "大小", "sha256")
	for i, v := range versions {
		fmt.Printf("%-4d %-20s %-10d %s\n", i+1,
			v.ReplacedAt.Local().Format("2006-01-02 15:04:05"), len(v.Blob), shortHash(v.Blob))
	}
	return nil
}

// shortHash 返回 blob 的 sha256 前 12 位十六进制（展示用）
func shortHash(blob []byte) string {
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])[:12]
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
		historyDisplay(*chosen, key, historyReveal))
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
