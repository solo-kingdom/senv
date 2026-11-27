package conflict

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

// SkillFileChange 描述两个 skill 归档之间单个文件的差异。
type SkillFileChange struct {
	Path        string `json:"path"`
	Status      string `json:"status"` // "added" | "removed" | "modified"
	LocalSize   int    `json:"local_size,omitempty"`
	RemoteSize  int    `json:"remote_size,omitempty"`
	UnifiedDiff string `json:"unified_diff,omitempty"`
}

// SkillArchiveDiff 描述两个 skill 归档的规范化内容对比结果。
type SkillArchiveDiff struct {
	LocalCompressed        bool              `json:"local_compressed"`
	RemoteCompressed       bool              `json:"remote_compressed"`
	LocalFileCount         int               `json:"local_file_count"`
	RemoteFileCount        int               `json:"remote_file_count"`
	LocalUncompressedSize  int               `json:"local_uncompressed_size"`
	RemoteUncompressedSize int               `json:"remote_uncompressed_size"`
	LocalDigest            string            `json:"local_digest"`
	RemoteDigest           string            `json:"remote_digest"`
	ContentEquivalent      bool              `json:"content_equivalent"`
	Changes                []SkillFileChange `json:"changes,omitempty"`
}

func archiveFormatLabel(compressed bool) string {
	if compressed {
		return "tar.gz"
	}
	return "tar"
}

func canonicalArchiveDigest(info *skill.Archive, files map[string][]byte) (string, map[string]skill.Entry) {
	byPath := make(map[string]skill.Entry, len(info.Entries))
	paths := make([]string, 0, len(info.Entries))
	for _, e := range info.Entries {
		if e.Dir {
			continue
		}
		byPath[e.Path] = e
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		e := byPath[p]
		contentSum := sha256.Sum256(files[p])
		fmt.Fprintf(h, "%s\x00%o\x00%s\n", p, e.Mode&0o111, hex.EncodeToString(contentSum[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), byPath
}

// CompareSkillArchives 解码并对比两份 skill 归档（透明兼容裸 tar 与 tar.gz）。
func CompareSkillArchives(localBlob, remoteBlob []byte) (*SkillArchiveDiff, error) {
	localInfo, localFiles, err := skill.ReadArchiveFiles(localBlob)
	if err != nil {
		return nil, fmt.Errorf("decode local skill archive: %w", err)
	}
	remoteInfo, remoteFiles, err := skill.ReadArchiveFiles(remoteBlob)
	if err != nil {
		return nil, fmt.Errorf("decode remote skill archive: %w", err)
	}

	localDigest, localEntries := canonicalArchiveDigest(localInfo, localFiles)
	remoteDigest, remoteEntries := canonicalArchiveDigest(remoteInfo, remoteFiles)

	diff := &SkillArchiveDiff{
		LocalCompressed:        localInfo.Compressed,
		RemoteCompressed:       remoteInfo.Compressed,
		LocalFileCount:         localInfo.FileCount(),
		RemoteFileCount:        remoteInfo.FileCount(),
		LocalUncompressedSize:  localInfo.UncompressedSize,
		RemoteUncompressedSize: remoteInfo.UncompressedSize,
		LocalDigest:            localDigest,
		RemoteDigest:           remoteDigest,
		ContentEquivalent:      localDigest == remoteDigest,
	}
	if diff.ContentEquivalent {
		return diff, nil
	}

	allPaths := make(map[string]bool, len(localEntries)+len(remoteEntries))
	for p := range localEntries {
		allPaths[p] = true
	}
	for p := range remoteEntries {
		allPaths[p] = true
	}
	ordered := make([]string, 0, len(allPaths))
	for p := range allPaths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)

	for _, p := range ordered {
		le, hasL := localEntries[p]
		re, hasR := remoteEntries[p]
		switch {
		case !hasL && hasR:
			diff.Changes = append(diff.Changes, SkillFileChange{
				Path: p, Status: "added", RemoteSize: len(remoteFiles[p]),
			})
		case hasL && !hasR:
			diff.Changes = append(diff.Changes, SkillFileChange{
				Path: p, Status: "removed", LocalSize: len(localFiles[p]),
			})
		default:
			lb, rb := localFiles[p], remoteFiles[p]
			if string(lb) == string(rb) && (le.Mode&0o111) == (re.Mode&0o111) {
				continue
			}
			ch := SkillFileChange{
				Path: p, Status: "modified", LocalSize: len(lb), RemoteSize: len(rb),
			}
			if utf8.Valid(lb) && utf8.Valid(rb) {
				ch.UnifiedDiff = simpleLineDiff(string(lb), string(rb), 80)
			}
			diff.Changes = append(diff.Changes, ch)
		}
	}
	return diff, nil
}

// RenderSkillDiff 渲染 skill 归档差异摘要；reveal=true 时附带受限行数的文本 diff。
func RenderSkillDiff(diff *SkillArchiveDiff, reveal bool) string {
	if diff == nil {
		return ""
	}
	var b strings.Builder
	if diff.ContentEquivalent {
		fmt.Fprintf(&b, "  ≡ 内容完全一致（仅归档压缩格式或密文不同：local=%s / remote=%s）\n",
			archiveFormatLabel(diff.LocalCompressed), archiveFormatLabel(diff.RemoteCompressed))
		return b.String()
	}
	fmt.Fprintf(&b, "  skill archive differences (local=%s %d files, remote=%s %d files):\n",
		archiveFormatLabel(diff.LocalCompressed), diff.LocalFileCount,
		archiveFormatLabel(diff.RemoteCompressed), diff.RemoteFileCount)

	totalLines := 0
	const maxTotalDiffLines = 200
	for _, ch := range diff.Changes {
		switch ch.Status {
		case "added":
			fmt.Fprintf(&b, "    + %s (remote only, %dB)\n", ch.Path, ch.RemoteSize)
		case "removed":
			fmt.Fprintf(&b, "    - %s (local only, %dB)\n", ch.Path, ch.LocalSize)
		case "modified":
			fmt.Fprintf(&b, "    ~ %s (local=%dB, remote=%dB)\n", ch.Path, ch.LocalSize, ch.RemoteSize)
			if reveal && ch.UnifiedDiff != "" && totalLines < maxTotalDiffLines {
				for _, line := range strings.Split(strings.TrimRight(ch.UnifiedDiff, "\n"), "\n") {
					if totalLines >= maxTotalDiffLines {
						b.WriteString("      ... (diff truncated)\n")
						break
					}
					fmt.Fprintf(&b, "      %s\n", line)
					totalLines++
				}
			}
		}
	}
	return b.String()
}

// RenderSkillIndexDiff 渲染两侧 skill_index.json 的语义差异，并显式标出同版本号内容碰撞。
func RenderSkillIndexDiff(localBlob, remoteBlob []byte) string {
	var local, remote storage.SkillIndex
	localErr := storage.FromJSON(localBlob, &local)
	remoteErr := storage.FromJSON(remoteBlob, &remote)
	if localErr != nil || remoteErr != nil {
		return "  skill index: invalid JSON; semantic diff unavailable\n"
	}

	var b strings.Builder
	b.WriteString("  skill index differences:\n")

	groupNames := make(map[string]bool)
	for g := range local.Groups {
		groupNames[g] = true
	}
	for g := range remote.Groups {
		groupNames[g] = true
	}
	orderedGroups := make([]string, 0, len(groupNames))
	for g := range groupNames {
		orderedGroups = append(orderedGroups, g)
	}
	sort.Strings(orderedGroups)
	for _, g := range orderedGroups {
		lg, hasL := local.Groups[g]
		rg, hasR := remote.Groups[g]
		if !hasL {
			fmt.Fprintf(&b, "    group %s: remote added\n", g)
		} else if !hasR {
			fmt.Fprintf(&b, "    group %s: local only (remote deleted)\n", g)
		} else if lg.Description != rg.Description || lg.InstallScope != rg.InstallScope || lg.InstallPath != rg.InstallPath {
			fmt.Fprintf(&b, "    group %s: policy/description changed\n", g)
		}
	}

	skillIDs := make(map[string]bool)
	for id := range local.Skills {
		skillIDs[id] = true
	}
	for id := range remote.Skills {
		skillIDs[id] = true
	}
	orderedSkills := make([]string, 0, len(skillIDs))
	for id := range skillIDs {
		orderedSkills = append(orderedSkills, id)
	}
	sort.Strings(orderedSkills)

	for _, id := range orderedSkills {
		ls, hasL := local.Skills[id]
		rs, hasR := remote.Skills[id]
		if !hasL {
			fmt.Fprintf(&b, "    %s: remote added (group=%s, version=%s)\n", id, rs.NormalizedGroup(), versionOrDash(rs.CurrentVersion))
			continue
		}
		if !hasR {
			fmt.Fprintf(&b, "    %s: local only (group=%s, version=%s)\n", id, ls.NormalizedGroup(), versionOrDash(ls.CurrentVersion))
			continue
		}
		if ls.NormalizedGroup() != rs.NormalizedGroup() {
			fmt.Fprintf(&b, "    %s group: local=%s remote=%s\n", id, ls.NormalizedGroup(), rs.NormalizedGroup())
		}
		if ls.CurrentVersion != rs.CurrentVersion {
			fmt.Fprintf(&b, "    %s current_version: local=%s remote=%s\n", id, versionOrDash(ls.CurrentVersion), versionOrDash(rs.CurrentVersion))
		}
		if ls.Description != rs.Description {
			fmt.Fprintf(&b, "    %s description changed\n", id)
		}
		// 检查同版本号内容哈希碰撞
		for _, lv := range ls.Versions {
			if rv, ok := rs.FindVersion(lv.Version); ok && lv.SHA256 != "" && rv.SHA256 != "" && lv.SHA256 != rv.SHA256 {
				fmt.Fprintf(&b, "    ⚠ %s@%s version collision (local=%s remote=%s)；可运行 senv skill rebase %s 保留远端并顺延本地版本号\n",
					id, lv.Version, shortDigest(lv.SHA256), shortDigest(rv.SHA256), id)
			}
		}
	}

	if !strings.Contains(b.String(), "    ") {
		b.WriteString("    no semantic differences\n")
	}
	return b.String()
}

func versionOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func shortDigest(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

// simpleLineDiff 生成受限行数的 LCS 行级 diff（不引入第三方依赖）。
func simpleLineDiff(localText, remoteText string, maxLines int) string {
	a := strings.Split(strings.ReplaceAll(localText, "\r\n", "\n"), "\n")
	b := strings.Split(strings.ReplaceAll(remoteText, "\r\n", "\n"), "\n")

	// 超大文本直接按前后摘要输出，避免 O(N*M) DP 内存膨胀。
	if len(a)*len(b) > 250000 {
		return fmt.Sprintf("--- local (%d lines)\n+++ remote (%d lines)\n(file too large for inline line diff)\n", len(a), len(b))
	}

	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < m || j < n {
		switch {
		case i < m && j < n && a[i] == b[j]:
			i++
			j++
		case j < n && (i == m || dp[i][j+1] >= dp[i+1][j]):
			out = append(out, "+ "+b[j])
			j++
		default:
			out = append(out, "- "+a[i])
			i++
		}
		if maxLines > 0 && len(out) >= maxLines {
			out = append(out, "... (truncated)")
			break
		}
	}
	return strings.Join(out, "\n")
}
