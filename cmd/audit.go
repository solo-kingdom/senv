package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
)

// auditOp 记录一条业务操作审计事件（best-effort）：任何失败都不影响命令
// 行为与退出码。target 只允许 kind/group/key、文件名等非敏感标识。
func auditOp(eventType session.AuditEventType, target string, success bool, detail string) {
	mgr := session.NewManager(getConfigPath(), getDataPath())
	defer mgr.Close()
	if al := mgr.GetAuditLogger(); al != nil {
		_ = al.LogOp(eventType, target, success, detail)
	}
}

var (
	auditSince string
	auditUntil string
	auditType  string
	auditLimit int
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "查看本机操作审计日志（含日期）",
	Long: `查看本机审计事件（业务操作 + 会话/认证事件），按时间新到旧展示。

  senv audit                                最近事件
  senv audit --since 2026-09-01             某日期（含）之后
  senv audit --type op                      全部业务操作事件
  senv audit --type op_sync --limit 20      指定类型 + 条数

仅记录操作元数据（操作、目标、日期时间、结果），不记录任何值。
审计随本机文件保存（~/.log/senv/audit.log），跨机器不可见。`,
	Args: cobra.NoArgs,
	RunE: runAuditView,
}

func init() {
	auditCmd.Flags().StringVar(&auditSince, "since", "", "起始日期（YYYY-MM-DD 或 RFC3339，含）")
	auditCmd.Flags().StringVar(&auditUntil, "until", "", "结束日期（YYYY-MM-DD 或 RFC3339，含当天）")
	auditCmd.Flags().StringVar(&auditType, "type", "", "事件类型过滤（如 op_env、op_sync；op 匹配全部业务操作）")
	auditCmd.Flags().IntVar(&auditLimit, "limit", 50, "最多显示条数")
	auditCmd.Annotations = map[string]string{"senv/skip-auto-push": "true"}
	rootCmd.AddCommand(auditCmd)
}

func runAuditView(cmd *cobra.Command, args []string) error {
	since, until, hasRange, err := parseAuditTimeRange(auditSince, auditUntil)
	if err != nil {
		return err
	}
	entries, skipped, err := loadAuditEntries()
	if err != nil {
		return err
	}
	shown := 0
	for _, e := range entries {
		if shown >= auditLimit {
			break
		}
		if hasRange {
			if !since.IsZero() && e.Timestamp.Before(since) {
				continue
			}
			if !until.IsZero() && e.Timestamp.After(until) {
				continue
			}
		}
		if !auditTypeMatches(auditType, string(e.EventType)) {
			continue
		}
		outcome := "✓"
		if !e.Success {
			outcome = "✗"
		}
		fmt.Printf("%s  %-18s %-8s %s  %s\n",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			string(e.EventType), outcome, e.Target,
			truncateAuditDetail(e.Message))
		shown++
	}
	if shown == 0 {
		fmt.Println("（无匹配的审计事件）")
	}
	if skipped > 0 {
		fmt.Printf("⚠ 跳过 %d 行无法解析的审计记录\n", skipped)
	}
	return nil
}

// truncateAuditDetail 控制说明列宽度，超出截断
func truncateAuditDetail(s string) string {
	const max = 60
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// auditTypeMatches 类型过滤：空 = 全部；"op" = 全部业务操作（op_ 前缀）；否则精确匹配
func auditTypeMatches(filter, eventType string) bool {
	if filter == "" {
		return true
	}
	if filter == "op" {
		return strings.HasPrefix(eventType, "op_")
	}
	return filter == eventType
}

// parseAuditTimeRange 解析 --since/--until；纯日期视为当天起止（until 含当天
// 全天）。返回 hasRange 表示是否给了任一日期。
func parseAuditTimeRange(sinceStr, untilStr string) (since, until time.Time, hasRange bool, err error) {
	if sinceStr != "" {
		since, err = parseAuditTime(sinceStr, false)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("--since %q: %w", sinceStr, err)
		}
		hasRange = true
	}
	if untilStr != "" {
		until, err = parseAuditTime(untilStr, true)
		if err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("--until %q: %w", untilStr, err)
		}
		hasRange = true
	}
	return since, until, hasRange, nil
}

func parseAuditTime(value string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		if endOfDay {
			return t.Add(24*time.Hour - time.Second), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("无法解析日期，支持 YYYY-MM-DD 或 RFC3339")
}

// loadAuditEntries 读取审计文件并按时间新到旧返回；坏行跳过并计数
func loadAuditEntries() ([]session.AuditEntry, int, error) {
	path := session.AuditLogPath()
	if path == "" {
		return nil, 0, fmt.Errorf("无法定位审计文件（缺少 HOME）")
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("打开审计文件失败: %w", err)
	}
	defer file.Close()

	var entries []session.AuditEntry
	skipped := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e session.AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return entries, skipped, fmt.Errorf("读取审计文件失败: %w", err)
	}
	// 新到旧
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, skipped, nil
}
