package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/ledger"
	"github.com/wii/senv/internal/session"
)

var (
	ledgerGroup   string
	ledgerLimit   int
	ledgerType    string
	ledgerSummary string
	ledgerBody    string
	ledgerBodyF   string
	ledgerKind    string
	ledgerAgent   string
	ledgerAt      string
	ledgerField   string
	ledgerText    string
	ledgerLevel   string
	ledgerDesc    string
	ledgerYes     bool
	ledgerJSON    bool
)

var ledgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "台账：实体头 + 不可变操作记录（server / git 模式均可）",
	Long: `台账保存「实体（设备 / 服务 / 基础设施 / agent 端点）+ 只追加操作记录」。

  senv ledger create device.lnv --group personal --description "自托管节点"
  senv ledger list [--group personal] [--type device] [--json]
  senv ledger get personal:device.lnv
  senv ledger append personal:device.lnv --summary "升级到 t0.1.26" --body-file note.md
  senv ledger log personal:device.lnv --limit 10
  senv ledger timeline --limit 20 [--type device]
  senv ledger edit personal:device.lnv --field "位置=家里"
  senv ledger set-status personal:device.lnv --text "在线，磁盘 53%" --level warn
  senv ledger delete-record personal:device.lnv@20261123T091500Z-3f9a

记录写入只新增条目（不可变），多机并发追加不会冲突；写错了用 --kind correction 追加更正。`,
}

var ledgerCreateCmd = &cobra.Command{
	Use:   "create <type>.<id>",
	Short: "登记实体头",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getLedgerManager()
		if err != nil {
			return err
		}
		typ, id, err := ledger.SplitHeadKey(args[0])
		if err != nil {
			return err
		}
		h, err := ledger.NewHead(typ, id, ledgerDesc, nil)
		if err != nil {
			return err
		}
		if err := m.SaveHead(ledgerGroup, h); err != nil {
			return err
		}
		fmt.Printf("✓ 已登记实体 %s.%s（group=%s）\n", typ, id, ledgerGroup)
		auditOp(session.AuditOpLedger, "ledger:"+ledgerGroup+":"+args[0], true, "create")
		return nil
	},
}

var ledgerListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出实体",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getLedgerManager()
		if err != nil {
			return err
		}
		heads, err := m.ListHeads(ledgerGroup)
		if err != nil {
			return err
		}
		filtered := heads[:0]
		for _, h := range heads {
			if ledgerType == "" || h.Type == ledgerType {
				filtered = append(filtered, h)
			}
		}
		if ledgerJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(filtered)
		}
		fmt.Printf("%-10s %-28s %-24s %s\n", "类型", "实体", "说明", "更新时间")
		for _, h := range filtered {
			fmt.Printf("%-10s %-28s %-24s %s\n", h.Type, h.ID, truncateCell(h.Description, 24),
				h.UpdatedAt.Local().Format("2006-01-02 15:04"))
		}
		return nil
	},
}

var ledgerGetCmd = &cobra.Command{
	Use:   "get <group>:<type>.<id>",
	Short: "查看实体头与最近记录",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		h, err := m.GetHead(grp, key)
		if err != nil {
			return err
		}
		fmt.Printf("# %s.%s  (%s)\n", h.Type, h.ID, h.Description)
		if len(h.Tags) > 0 {
			fmt.Printf("标签: %s\n", strings.Join(h.Tags, ", "))
		}
		if len(h.Fields) > 0 {
			fmt.Println("\n## 基本信息")
			for _, f := range h.Fields {
				fmt.Printf("- %s: %s\n", f.Name, f.Value)
			}
		}
		if len(h.Status) > 0 {
			fmt.Println("\n## 最近状态")
			for _, s := range h.Status {
				fmt.Printf("- %s —— %s\n", s.At.Local().Format("2006-01-02 15:04"), s.Text)
			}
		}
		recs, err := m.Log(grp, key, ledgerLimit)
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			fmt.Println("\n## 操作记录（新到旧）")
			for _, r := range recs {
				fmt.Printf("\n### %s · %s\n", r.At.Local().Format("2006-01-02"), r.Summary)
				if r.By.Agent != "" || r.By.Host != "" {
					fmt.Printf("- 操作人 / Agent: %s%s\n", r.By.Agent, hostSuffix(r.By.Host))
				}
				if r.Body != "" {
					fmt.Println(r.Body)
				}
			}
		}
		return nil
	},
}

var ledgerLogCmd = &cobra.Command{
	Use:   "log <group>:<type>.<id>",
	Short: "列出某实体的记录",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		recs, err := m.Log(grp, key, ledgerLimit)
		if err != nil {
			return err
		}
		for _, r := range recs {
			fmt.Printf("%s  %-10s  %-12s  %s\n", r.At.Format(time.RFC3339), r.Kind,
				truncateCell(actorLabel(r.By), 12), r.Summary)
		}
		return nil
	},
}

var ledgerTimelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "跨实体时间线",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getLedgerManager()
		if err != nil {
			return err
		}
		recs, err := m.Timeline(ledgerLimit, ledgerType)
		if err != nil {
			return err
		}
		for _, r := range recs {
			fmt.Printf("%s  %s.%-16s  %-10s  %s\n", r.At.Format("2006-01-02 15:04"),
				r.Entity.Type, r.Entity.ID, r.Kind, r.Summary)
		}
		return nil
	},
}

var ledgerAppendCmd = &cobra.Command{
	Use:   "append <group>:<type>.<id>",
	Short: "追加一条记录（不可变）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		body := ledgerBody
		if ledgerBodyF != "" {
			data, err := os.ReadFile(ledgerBodyF)
			if err != nil {
				return err
			}
			body = string(data)
		}
		at := time.Time{}
		if ledgerAt != "" {
			parsed, err := time.ParseInLocation("2006-01-02", ledgerAt, time.Local)
			if err != nil {
				return fmt.Errorf("--at 需为 YYYY-MM-DD: %w", err)
			}
			at = parsed
		}
		host, _ := os.Hostname()
		rec, recKey, err := m.Append(grp, key, ledger.RecordInput{
			Kind: ledgerKind, Summary: ledgerSummary, Body: body, At: at,
			By: ledger.By{Host: host, User: os.Getenv("USER"), Agent: ledgerAgent},
		})
		if err != nil {
			return err
		}
		fmt.Printf("✓ 已追加 %s（%s）\n", recKey, rec.Summary)
		auditOp(session.AuditOpLedger, "ledger:"+grp+":"+recKey, true, "append")
		return nil
	},
}

var ledgerEditCmd = &cobra.Command{
	Use:   "edit <group>:<type>.<id>",
	Short: "修改实体头字段（--field 名称=值；空值删除字段）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		name, value, ok := strings.Cut(ledgerField, "=")
		if !ok {
			return fmt.Errorf("--field 需为 名称=值（值为空表示删除）")
		}
		if err := m.EditField(grp, key, name, value); err != nil {
			return err
		}
		fmt.Printf("✓ 已更新字段 %s\n", name)
		auditOp(session.AuditOpLedger, "ledger:"+grp+":"+key, true, "edit "+name)
		return nil
	},
}

var ledgerStatusCmd = &cobra.Command{
	Use:   "set-status <group>:<type>.<id>",
	Short: "写入最近状态（保留最近 2 条）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		host, _ := os.Hostname()
		if err := m.SetStatus(grp, key, ledger.StatusEntry{
			Text: ledgerText, Level: ledgerLevel,
			By: ledger.By{Host: host, User: os.Getenv("USER"), Agent: ledgerAgent},
		}); err != nil {
			return err
		}
		fmt.Println("✓ 已写入状态")
		auditOp(session.AuditOpLedger, "ledger:"+grp+":"+key, true, "status")
		return nil
	},
}

var ledgerDeleteCmd = &cobra.Command{
	Use:   "delete-record <group>:<type>.<id>@<ts>-<hex>",
	Short: "删除一条记录（同步为 tombstone；最后版本留在条目历史）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, grp, key, err := ledgerTarget(args[0])
		if err != nil {
			return err
		}
		name, rec, ok := strings.Cut(key, "@")
		if !ok {
			return fmt.Errorf("参数需为 <group>:<type>.<id>@<时间戳>-<hex>")
		}
		if !ledgerYes {
			fmt.Printf("将删除记录 %s（%s）。同步为 tombstone，最后版本留在历史。确认？[y/N] ", rec, name)
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("已取消")
				return nil
			}
		}
		if err := m.DeleteRecord(grp, name+"@"+rec); err != nil {
			return err
		}
		fmt.Println("✓ 已删除（同步后生效）")
		auditOp(session.AuditOpLedger, "ledger:"+grp+":"+name+"@"+rec, true, "delete-record")
		return nil
	},
}

func init() {
	ledgerCmd.PersistentFlags().StringVarP(&ledgerGroup, "group", "g", "default", "台账分组（归属桶，如 personal / feg / mv）")
	ledgerCmd.PersistentFlags().BoolVar(&ledgerJSON, "json", false, "JSON 输出")

	ledgerListCmd.Flags().StringVar(&ledgerType, "type", "", "按实体类型过滤")
	ledgerGetCmd.Flags().IntVar(&ledgerLimit, "limit", 5, "附带展示的记录条数")
	ledgerLogCmd.Flags().IntVar(&ledgerLimit, "limit", 20, "记录条数")
	ledgerTimelineCmd.Flags().IntVar(&ledgerLimit, "limit", 20, "条数")
	ledgerTimelineCmd.Flags().StringVar(&ledgerType, "type", "", "按实体类型过滤")

	ledgerAppendCmd.Flags().StringVar(&ledgerSummary, "summary", "", "一行标题（必填）")
	ledgerAppendCmd.Flags().StringVar(&ledgerBody, "body", "", "正文（Markdown）")
	ledgerAppendCmd.Flags().StringVar(&ledgerBodyF, "body-file", "", "正文文件（优先于 --body）")
	ledgerAppendCmd.Flags().StringVar(&ledgerKind, "kind", "op", "记录类型：op / note / correction")
	ledgerAppendCmd.Flags().StringVar(&ledgerAgent, "agent", "", "操作 agent 名称（如 Claude Code）")
	ledgerAppendCmd.Flags().StringVar(&ledgerAt, "at", "", "事件日期 YYYY-MM-DD（默认现在）")
	ledgerAppendCmd.MarkFlagRequired("summary")

	ledgerEditCmd.Flags().StringVar(&ledgerField, "field", "", "字段赋值（名称=值）")
	ledgerEditCmd.MarkFlagRequired("field")

	ledgerStatusCmd.Flags().StringVar(&ledgerText, "text", "", "状态文本")
	ledgerStatusCmd.Flags().StringVar(&ledgerLevel, "level", "", "级别（如 ok / warn）")
	ledgerStatusCmd.Flags().StringVar(&ledgerAgent, "agent", "", "操作 agent 名称")
	ledgerStatusCmd.MarkFlagRequired("text")

	ledgerDeleteCmd.Flags().BoolVar(&ledgerYes, "yes", false, "跳过确认")

	ledgerCreateCmd.Flags().StringVar(&ledgerDesc, "description", "", "一句话用途")

	ledgerCmd.AddCommand(ledgerCreateCmd, ledgerListCmd, ledgerGetCmd, ledgerLogCmd,
		ledgerTimelineCmd, ledgerAppendCmd, ledgerEditCmd, ledgerStatusCmd, ledgerDeleteCmd)
	rootCmd.AddCommand(ledgerCmd)
}

// getLedgerManager 构造台账管理器（session key 优先，否则由口令派生）
func getLedgerManager() (*ledger.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	key := auth.key
	if len(key) == 0 {
		key, err = auth.storage.DeriveKeyFromPassword(auth.password)
		if err != nil {
			return nil, err
		}
	}
	return ledger.NewManager(auth.storage, key), nil
}

// ledgerTarget 解析 <group>:<key>（group 省略时用 --group）
func ledgerTarget(arg string) (*ledger.Manager, string, string, error) {
	m, err := getLedgerManager()
	if err != nil {
		return nil, "", "", err
	}
	grp, key := ledgerGroup, arg
	if g, k, ok := strings.Cut(arg, ":"); ok {
		grp, key = g, k
	}
	if _, _, err := ledger.SplitHeadKey(strings.SplitN(key, "@", 2)[0]); err != nil {
		return nil, "", "", err
	}
	return m, grp, key, nil
}

func hostSuffix(host string) string {
	if host == "" {
		return ""
	}
	return "（" + host + "）"
}

func actorLabel(b ledger.By) string {
	switch {
	case b.Agent != "":
		return b.Agent
	case b.User != "":
		return b.User
	case b.Host != "":
		return b.Host
	}
	return "-"
}
