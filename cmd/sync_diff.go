package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/skill"
)

var (
	syncDiffReveal       bool
	syncDiffJSON         bool
	syncDiffExportRemote string
)

var syncDiffCmd = &cobra.Command{
	Use:   "diff [kind:grp:key]",
	Short: "只读检视待推送更改与远端的冲突差异（不修改任何一端）",
	Long: `在零副作用前提下向 server 探测当前待推送条目是否与远端发生冲突，并展示语义差异：
  - skill: 解码对比 tar/tar.gz 文件清单，--reveal 展示文本行级 diff；
  - skill_index / config_index: 展示条目/分组差异与同版本号碰撞提示；
  - --export-remote <dir>: 将远端非私钥条目（含 skill 解包目录）导出到指定目录以便本地 diff -ru。`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSyncDiff,
}

func init() {
	syncDiffCmd.Flags().BoolVar(&syncDiffReveal, "reveal", false, "展开文本与 skill 归档的行级差异（SSH 私钥永不展开）")
	syncDiffCmd.Flags().BoolVar(&syncDiffJSON, "json", false, "以结构化 JSON 输出冲突摘要（不含秘密明文）")
	syncDiffCmd.Flags().StringVar(&syncDiffExportRemote, "export-remote", "", "将远端冲突条目（不含私钥）无损导出到指定目录")
	markSkipAutoPush(syncDiffCmd)
	syncCmd.AddCommand(syncDiffCmd)
}

type syncDiffJSONItem struct {
	Kind               string                           `json:"kind"`
	Grp                string                           `json:"grp"`
	Key                string                           `json:"key"`
	Local              provider.ConflictSide            `json:"local"`
	Remote             provider.ConflictSide            `json:"remote"`
	Equivalent         bool                             `json:"equivalent,omitempty"`
	DeleteVsFormatOnly bool                             `json:"delete_vs_format_only,omitempty"`
	Reason             string                           `json:"reason,omitempty"`
	SkillDiff          *renderconflict.SkillArchiveDiff `json:"skill_diff,omitempty"`
}

func runSyncDiff(cmd *cobra.Command, args []string) error {
	sp, err := getServerProvider()
	if err != nil {
		return err
	}
	ctx := commandContext(cmd)
	conflictErr, err := sp.InspectConflicts(ctx)
	if err != nil {
		return err
	}
	if conflictErr == nil || (len(conflictErr.Details) == 0 && !conflictErr.MetadataConflict) {
		if syncDiffJSON {
			fmt.Fprintln(cmd.OutOrStdout(), `{"conflicts":[]}`)
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ 当前无同步冲突")
		return nil
	}

	details := conflictErr.Details
	if len(args) == 1 {
		details = filterConflictDetails(details, args[0])
		if len(details) == 0 {
			return fmt.Errorf("未找到匹配 %q 的冲突条目", args[0])
		}
	}

	var remoteMeta []byte
	if conflictErr.Metadata != nil {
		remoteMeta = conflictErr.Metadata.Remote
	}
	auth := cachedConflictAuth(remoteMeta)
	if len(auth.Key) == 0 && (syncDiffReveal || syncDiffExportRemote != "") {
		if resolved, rerr := resolveAuth(getConfigPath(), getDataPath(), authPrompt); rerr == nil {
			if key, kerr := resolveKeyForAuth(resolved); kerr == nil {
				localMeta, _ := os.ReadFile(filepath.Join(getConfigPath(), "metadata.json"))
				auth = renderconflict.NewAuthWithLocalMetadata(key, localMeta, remoteMeta)
			}
		}
	}

	if syncDiffExportRemote != "" {
		if err := exportRemoteConflictEntries(syncDiffExportRemote, details, auth); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ 已将 %d 条远端冲突内容导出到 %s\n", len(details), syncDiffExportRemote)
	}

	classifications := make([]renderconflict.ConflictClassification, len(details))
	for i, d := range details {
		cls := renderconflict.ClassifyConflict(d, auth)
		if d.Local.Deleted && !d.Remote.Deleted && d.Local.Revision > 0 && len(auth.Key) > 0 && auth.RemoteKeyCompatible {
			if hist, herr := sp.History(ctx, provider.HistoryFilter{Kind: d.Kind, Grp: d.Grp, Key: d.Key, Limit: 5}); herr == nil {
				cls.RemoteHistoryChecked = true
				for _, hv := range hist {
					if hv.Revision == d.Local.Revision && !hv.Deleted {
						if unchanged, reason := renderconflict.CheckDeleteVsFormatOnly(d, hv.Ciphertext, auth); unchanged {
							cls.DeleteVsFormatOnly = true
							cls.Reason = reason
						}
						break
					}
				}
			}
		}
		classifications[i] = cls
	}

	if syncDiffJSON {
		outItems := make([]syncDiffJSONItem, 0, len(details))
		for i, d := range details {
			cls := classifications[i]
			item := syncDiffJSONItem{
				Kind: d.Kind, Grp: d.Grp, Key: d.Key,
				Local: d.Local, Remote: d.Remote,
				Equivalent:         cls.Equivalent,
				DeleteVsFormatOnly: cls.DeleteVsFormatOnly,
				Reason:             cls.Reason,
			}
			if d.Kind == provider.KindSkill && !d.Local.Deleted && !d.Remote.Deleted && len(auth.Key) > 0 && auth.RemoteKeyCompatible {
				if lp, lerr := renderconflict.DecryptEntryPayload(d.Kind, d.Grp, d.Key, d.Local.Ciphertext, auth); lerr == nil {
					if rp, rerr := renderconflict.DecryptEntryPayload(d.Kind, d.Grp, d.Key, d.Remote.Ciphertext, auth); rerr == nil {
						if sdiff, derr := renderconflict.CompareSkillArchives(lp, rp); derr == nil {
							item.SkillDiff = sdiff
						}
					}
				}
			}
			outItems = append(outItems, item)
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"metadata_conflict": conflictErr.MetadataConflict,
			"conflicts":         outItems,
		})
	}

	for i, d := range details {
		if i > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 48))
		}
		fmt.Fprint(cmd.OutOrStdout(), renderconflict.RenderDetail(d, auth, syncDiffReveal))
		cls := classifications[i]
		if cls.DeleteVsFormatOnly {
			fmt.Fprintf(cmd.OutOrStdout(), "  ℹ %s\n", cls.Reason)
		} else if d.Local.Deleted && !d.Remote.Deleted && cls.RemoteHistoryChecked {
			fmt.Fprintln(cmd.OutOrStdout(), "  ⚠ 远端在本地删除后有实质内容修改（或基线前像不可比），请核对后再裁决")
		}
	}
	if conflictErr.MetadataConflict && len(args) == 0 {
		var localMeta []byte
		if conflictErr.Metadata != nil {
			localMeta = conflictErr.Metadata.Local
		}
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 48))
		fmt.Fprint(cmd.OutOrStdout(), renderconflict.RenderMetadata(localMeta, remoteMeta, auth))
	}
	return nil
}

func filterConflictDetails(details []provider.ConflictDetail, addr string) []provider.ConflictDetail {
	var out []provider.ConflictDetail
	for _, d := range details {
		if matchConflictSelector(addr, d) {
			out = append(out, d)
		}
	}
	return out
}

func matchConflictSelector(selector string, d provider.ConflictDetail) bool {
	s := strings.TrimSpace(selector)
	if s == "" {
		return true
	}
	if s == d.Kind && (d.Kind == provider.KindSkillIndex || d.Kind == provider.KindConfigIndex) {
		return true
	}
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return false
	}
	grp := d.Grp
	if grp == "" {
		grp = "-"
	}
	selGrp := parts[1]
	if selGrp == "" {
		selGrp = "-"
	}
	return globMatch(parts[0], d.Kind) &&
		(globMatch(selGrp, grp) || (selGrp == "-" && d.Grp == "")) &&
		globMatch(parts[2], d.Key)
}

func globMatch(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	ok, err := filepath.Match(pattern, value)
	if err != nil {
		return pattern == value
	}
	return ok
}

func exportRemoteConflictEntries(destDir string, details []provider.ConflictDetail, auth renderconflict.Auth) error {
	if len(auth.Key) == 0 {
		return fmt.Errorf("导出远端内容需要先解锁 vault（运行 senv session start）")
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("创建导出目录失败: %w", err)
	}
	for _, d := range details {
		if d.Kind == provider.KindSSHHost || d.Kind == provider.KindSSHKeypair {
			return fmt.Errorf("出于安全边界，禁止导出 %s/%s/%s", d.Kind, d.Grp, d.Key)
		}
		if d.Remote.Deleted {
			continue
		}
		plain, err := renderconflict.DecryptEntryPayload(d.Kind, d.Grp, d.Key, d.Remote.Ciphertext, auth)
		if err != nil {
			return fmt.Errorf("解密远端条目 %s/%s/%s 失败: %w", d.Kind, d.Grp, d.Key, err)
		}
		grp := d.Grp
		if grp == "" {
			grp = "_"
		}
		key := d.Key
		if key == "" {
			key = "index.json"
		}
		if d.Kind == provider.KindSkill {
			targetDir := filepath.Join(destDir, d.Kind, grp, key)
			if _, err := skill.ExtractToDir(plain, targetDir); err != nil {
				return fmt.Errorf("解包远端 skill %s/%s 失败: %w", grp, key, err)
			}
			continue
		}
		targetFile := filepath.Join(destDir, d.Kind, grp, key)
		if err := os.MkdirAll(filepath.Dir(targetFile), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(targetFile, plain, 0o600); err != nil {
			return err
		}
	}
	return nil
}
