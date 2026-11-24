package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/tree"
)

var (
	treeJSON           bool
	treeFile           string
	treeExec           bool
	treeDesc           string
	treeYes            bool
	treeDryRun         bool
	treeDelete         bool
	treeIncludeIgnored bool
	treeOutput         string
	treeForce          bool
)

var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "加密多级文件树（deploy / wireguard 等编排目录；server / git 模式均可）",
	Long: `路径树：一文件一条加密记录，按 <group>:<相对路径> 寻址，保留可执行位。

  senv tree group add deploy --description "部署编排"
  senv tree ls deploy[:tcbj/caddy]
  senv tree get deploy:tcbj/caddy/Caddyfile
  senv tree set deploy:tcbj/caddy/Caddyfile --file ./Caddyfile
  senv tree edit deploy:tcbj/caddy/Caddyfile
  senv tree rm deploy:tcbj/caddy/Caddyfile --yes
  senv tree diff deploy:tcbj ./deploy/tcbj
  senv tree pull deploy:tcbj ./deploy/tcbj --dry-run
  senv tree push ./deploy/tcbj deploy:tcbj --dry-run

push 默认忽略 .git/、.env、*.dump、*.sql.gz、*.bak*、*.key、*.pem；
单文件明文上限 384 KiB。`,
}

// splitTreeAddr parses "<group>[:<path>]".
func splitTreeAddr(arg string) (group, rel string) {
	group, rel, _ = strings.Cut(arg, ":")
	return group, strings.Trim(rel, "/")
}

func getTreeManager() (*tree.Manager, error) {
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
	return tree.NewManager(auth.storage, key), nil
}

func treeFilePath(arg string) (group, rel string, err error) {
	group, rel = splitTreeAddr(arg)
	if group == "" || rel == "" {
		return "", "", fmt.Errorf("需要 <group>:<相对路径> 形式（如 deploy:tcbj/caddy/Caddyfile）")
	}
	return group, rel, nil
}

var treeGroupCmd = &cobra.Command{Use: "group", Short: "管理路径树分组"}

var treeGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出分组",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		groups, descs, err := m.ListGroups()
		if err != nil {
			return err
		}
		for _, g := range groups {
			fmt.Printf("%-16s %s\n", g, descs[g])
		}
		return nil
	},
}

var treeGroupAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "新建分组（必须带 --description）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		if err := m.AddGroup(args[0], treeDesc); err != nil {
			return err
		}
		fmt.Printf("✓ 已创建 tree 分组 %s\n", args[0])
		auditOp(session.AuditOpTree, "tree:"+args[0], true, "group-add")
		return nil
	},
}

var treeGroupDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "删除分组（非空需 --force）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		if !treeYes && !confirmPrompt(fmt.Sprintf("删除 tree 分组 %q？[y/N] ", args[0])) {
			fmt.Println("已取消")
			return nil
		}
		if err := m.DeleteGroup(args[0], treeForce); err != nil {
			return err
		}
		auditOp(session.AuditOpTree, "tree:"+args[0], true, "group-delete")
		fmt.Printf("✓ 已删除 tree 分组 %s\n", args[0])
		return nil
	},
}

var treeLsCmd = &cobra.Command{
	Use:   "ls <group>[:prefix]",
	Short: "列出路径树文件（不含内容）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		group, prefix := splitTreeAddr(args[0])
		metas, err := m.List(group, prefix)
		if err != nil {
			return err
		}
		if treeJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(metas)
		}
		for _, mt := range metas {
			fmt.Printf("%04o %8d  %s\n", mt.Mode, mt.Size, mt.Path)
		}
		return nil
	},
}

var treeGetCmd = &cobra.Command{
	Use:   "get <group>:<path>",
	Short: "输出单文件内容",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		group, rel, err := treeFilePath(args[0])
		if err != nil {
			return err
		}
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		e, err := m.Get(group, rel)
		if err != nil {
			return err
		}
		raw, err := e.DecodeBytes()
		if err != nil {
			return err
		}
		if treeOutput != "" {
			return os.WriteFile(treeOutput, raw, os.FileMode(e.Mode))
		}
		_, err = os.Stdout.Write(raw)
		return err
	},
}

var treeSetCmd = &cobra.Command{
	Use:   "set <group>:<path>",
	Short: "写入单文件（--file 或 stdin 管道）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		group, rel, err := treeFilePath(args[0])
		if err != nil {
			return err
		}
		var raw []byte
		mode := os.FileMode(0o644)
		switch {
		case treeFile != "":
			raw, err = os.ReadFile(treeFile)
			if err != nil {
				return err
			}
			if st, serr := os.Stat(treeFile); serr == nil {
				mode = st.Mode()
			}
		case !stdinIsTerminal():
			raw, err = io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("需要 --file 或通过管道提供内容（交互编辑请用 senv tree edit）")
		}
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		e, err := m.Set(group, rel, raw, tree.NormalizeMode(mode, treeExec), treeDesc, !cmd.Flags().Changed("description"))
		if err != nil {
			return err
		}
		fmt.Printf("✓ 已写入 %s:%s（%04o，%d 字节）\n", group, rel, e.Mode, len(raw))
		auditOp(session.AuditOpTree, "tree:"+group+":"+rel, true, "set")
		return nil
	},
}

var treeEditCmd = &cobra.Command{
	Use:   "edit <group>:<path>",
	Short: "用 $EDITOR 编辑单文件（仅交互终端）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !stdinIsTerminal() {
			return fmt.Errorf("tree edit 仅支持交互终端；非交互请用 tree get / set")
		}
		group, rel, err := treeFilePath(args[0])
		if err != nil {
			return err
		}
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		var before []byte
		mode := uint32(0o644)
		desc := ""
		if e, gerr := m.Get(group, rel); gerr == nil {
			before, err = e.DecodeBytes()
			if err != nil {
				return err
			}
			mode, desc = e.Mode, e.Description
		}
		dir, err := os.MkdirTemp("", "senv-tree-edit-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		tmp := filepath.Join(dir, filepath.Base(rel))
		if err := os.WriteFile(tmp, before, 0o600); err != nil {
			return err
		}
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vim"
		}
		parts := strings.Fields(editor)
		c := exec.Command(parts[0], append(parts[1:], tmp)...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("编辑器退出失败: %w", err)
		}
		after, err := os.ReadFile(tmp)
		if err != nil {
			return err
		}
		if string(after) == string(before) {
			fmt.Println("无变更")
			return nil
		}
		if _, err := m.Set(group, rel, after, mode, desc, true); err != nil {
			return err
		}
		auditOp(session.AuditOpTree, "tree:"+group+":"+rel, true, "edit")
		fmt.Printf("✓ 已更新 %s:%s\n", group, rel)
		return nil
	},
}

var treeRmCmd = &cobra.Command{
	Use:   "rm <group>:<path>",
	Short: "删除单文件",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		group, rel, err := treeFilePath(args[0])
		if err != nil {
			return err
		}
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		if !treeYes && !confirmPrompt(fmt.Sprintf("删除 %s:%s？[y/N] ", group, rel)) {
			fmt.Println("已取消")
			return nil
		}
		if err := m.Remove(group, rel); err != nil {
			return err
		}
		auditOp(session.AuditOpTree, "tree:"+group+":"+rel, true, "rm")
		fmt.Printf("✓ 已删除 %s:%s\n", group, rel)
		return nil
	},
}

func printTreePlan(plan []tree.DiffItem) {
	for _, it := range plan {
		fmt.Printf("  [%s] %s  %s\n", it.Action, it.Path, it.Detail)
	}
}

var treeDiffCmd = &cobra.Command{
	Use:   "diff <group>[:prefix] <local-dir>",
	Short: "对比 vault 与本地目录",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		group, prefix := splitTreeAddr(args[0])
		plan, err := m.Diff(group, prefix, args[1])
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("无差异")
			return nil
		}
		printTreePlan(plan)
		return nil
	},
}

var treePullCmd = &cobra.Command{
	Use:   "pull <group>[:prefix] <dest-dir>",
	Short: "增量拉取到本地目录（保留可执行位）",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		group, prefix := splitTreeAddr(args[0])
		plan, err := m.Pull(group, prefix, args[1], treeDelete, true)
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("无变更")
			return nil
		}
		printTreePlan(plan)
		if treeDryRun {
			fmt.Println("Dry run: nothing was written")
			return nil
		}
		if !treeYes {
			if !stdinIsTerminal() {
				return fmt.Errorf("非交互 stdin 下需 --yes")
			}
			if !confirmPrompt(fmt.Sprintf("写入 %d 项到 %s？[y/N] ", len(plan), args[1])) {
				fmt.Println("已取消")
				return nil
			}
		}
		if _, err := m.Pull(group, prefix, args[1], treeDelete, false); err != nil {
			return err
		}
		auditOp(session.AuditOpTree, "tree:"+args[0], true, "pull")
		fmt.Printf("✓ 已拉取 %d 项\n", len(plan))
		return nil
	},
}

var treePushCmd = &cobra.Command{
	Use:   "push <local-dir> <group>[:prefix]",
	Short: "增量推送本地目录（默认忽略 .env/.git/dump 等）",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := getTreeManager()
		if err != nil {
			return err
		}
		group, prefix := splitTreeAddr(args[1])
		plan, err := m.Push(args[0], group, prefix, treeIncludeIgnored, treeDelete, true)
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Println("无变更")
			return nil
		}
		printTreePlan(plan)
		if treeDryRun {
			fmt.Println("Dry run: nothing was written")
			return nil
		}
		if !treeYes {
			if !stdinIsTerminal() {
				return fmt.Errorf("非交互 stdin 下需 --yes")
			}
			if !confirmPrompt(fmt.Sprintf("推送 %d 项到 %s？[y/N] ", len(plan), args[1])) {
				fmt.Println("已取消")
				return nil
			}
		}
		if _, err := m.Push(args[0], group, prefix, treeIncludeIgnored, treeDelete, false); err != nil {
			return err
		}
		auditOp(session.AuditOpTree, "tree:"+args[1], true, "push")
		fmt.Printf("✓ 已推送 %d 项\n", len(plan))
		return nil
	},
}

func init() {
	treeLsCmd.Flags().BoolVar(&treeJSON, "json", false, "JSON 输出（仅元数据）")
	treeGetCmd.Flags().StringVarP(&treeOutput, "output", "o", "", "写入文件而非 stdout")
	treeSetCmd.Flags().StringVar(&treeFile, "file", "", "从文件读取内容")
	treeSetCmd.Flags().BoolVar(&treeExec, "exec", false, "标记可执行位 (0755)")
	treeSetCmd.Flags().StringVar(&treeDesc, "description", "", "文件说明（省略则保留原说明）")
	treeRmCmd.Flags().BoolVar(&treeYes, "yes", false, "跳过确认")
	treeGroupAddCmd.Flags().StringVar(&treeDesc, "description", "", "分组说明（必填）")
	treeGroupDeleteCmd.Flags().BoolVar(&treeYes, "yes", false, "跳过确认")
	treeGroupDeleteCmd.Flags().BoolVar(&treeForce, "force", false, "分组非空时强制删除")
	for _, c := range []*cobra.Command{treePullCmd, treePushCmd} {
		c.Flags().BoolVar(&treeDryRun, "dry-run", false, "只打印计划")
		c.Flags().BoolVar(&treeYes, "yes", false, "跳过确认")
		c.Flags().BoolVar(&treeDelete, "delete", false, "同步删除对端多余文件")
	}
	treePushCmd.Flags().BoolVar(&treeIncludeIgnored, "include-ignored", false, "不应用默认忽略规则")

	treeGroupCmd.AddCommand(treeGroupListCmd, treeGroupAddCmd, treeGroupDeleteCmd)
	treeCmd.AddCommand(treeGroupCmd, treeLsCmd, treeGetCmd, treeSetCmd, treeEditCmd, treeRmCmd,
		treeDiffCmd, treePullCmd, treePushCmd)
	rootCmd.AddCommand(treeCmd)
}
