// Package skill 实现 Agent Skill 的入库格式：一个 skill 目录 → ustar tar 归档，
// 以及对归档（含来自远端的不可信归档）的白名单校验。
//
// 归档约定：tar 根即 skill 根（根下直接是 SKILL.md 等，不多包一层目录名）。
// 只接受普通文件与目录；符号链接、硬链接、FIFO、设备、pax/GNU 扩展等一律拒绝。
package skill

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

const (
	// SkillFileName 是 skill 根下必须存在的说明文件。
	SkillFileName = "SKILL.md"
	// MaxArchiveBytes 是 tar 明文大小上限（1 MiB）。
	MaxArchiveBytes = storage.MaxSkillPlaintext
)

// Entry 描述归档中的一个条目（路径相对 skill 根，正斜杠分隔）。
type Entry struct {
	Path string
	Dir  bool
	Size int64
	// Mode 只含权限位（0o755 / 0o644 之类）；展开方不得据此还原 setuid/setgid/sticky。
	Mode fs.FileMode
}

// Archive 是通过校验的归档清单。
type Archive struct {
	Entries []Entry
	// SkillMD 是 SKILL.md 的内容，仅供校验 frontmatter，不应对外打印。
	SkillMD []byte
}

// FileCount 返回普通文件数量。
func (a *Archive) FileCount() int {
	n := 0
	for _, e := range a.Entries {
		if !e.Dir {
			n++
		}
	}
	return n
}

// ErrArchiveTooLarge 表示明文 tar 超过 1 MiB 上限。
var ErrArchiveTooLarge = errors.New("skill archive exceeds the 1 MiB plaintext limit")

func errTooLarge(size int64) error {
	return fmt.Errorf("%w (%d bytes > %d bytes)", ErrArchiveTooLarge, size, MaxArchiveBytes)
}

// Pack 把 srcDir 的内容打成 ustar tar：根即 skill 根，条目按路径字典序，
// 元数据归一化（uid/gid 为 0，mtime 为 epoch，普通文件权限为 0644 或带可执行位的 0755）。
// 源目录里出现符号链接或特殊文件、路径无法写入 ustar、超过大小上限都会拒绝。
// 返回前会用 Inspect 重新校验产物。
func Pack(srcDir string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(srcDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source directory: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat source directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source %q is not a directory", srcDir)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	var fileBytes int64
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if err := validateEntryPath(name); err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case fi.IsDir():
			return writeHeader(tw, &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name + "/",
				Mode:     0o755,
				ModTime:  time.Unix(0, 0),
				Format:   tar.FormatUSTAR,
			}, name)
		case fi.Mode().IsRegular():
			fileBytes += fi.Size()
			if fileBytes > MaxArchiveBytes {
				return errTooLarge(fileBytes)
			}
			mode := int64(0o644)
			if fi.Mode().Perm()&0o111 != 0 {
				mode = 0o755
			}
			if err := writeHeader(tw, &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     name,
				Mode:     mode,
				Size:     fi.Size(),
				ModTime:  time.Unix(0, 0),
				Format:   tar.FormatUSTAR,
			}, name); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			// 文件在遍历期间被改大时 tar.Writer 会报 ErrWriteTooLong，不会写出越界数据。
			if _, err := io.Copy(tw, f); err != nil {
				return fmt.Errorf("archive %q: %w", name, err)
			}
			return nil
		default:
			return fmt.Errorf("unsupported file type %s at %q: only regular files and directories are allowed", fi.Mode().Type(), name)
		}
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxArchiveBytes {
		return nil, errTooLarge(int64(buf.Len()))
	}
	if _, err := Inspect(buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeHeader(tw *tar.Writer, hdr *tar.Header, name string) error {
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("cannot store %q in ustar archive (non-ASCII or too-long path?): %w", name, err)
	}
	return nil
}

// validateEntryPath 校验归档内相对路径（不含目录条目的尾部斜杠）：
// 拒绝空路径、绝对路径、空段、"." / ".." 段，以及反斜杠、冒号、NUL 与控制字符
// （这些在 Windows 或终端上有路径/显示语义）。
func validateEntryPath(name string) error {
	if name == "" {
		return fmt.Errorf("empty archive path")
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return fmt.Errorf("absolute archive path %q is not allowed", name)
	}
	for _, seg := range strings.Split(name, "/") {
		switch seg {
		case "":
			return fmt.Errorf("archive path %q has an empty segment", name)
		case ".", "..":
			return fmt.Errorf("archive path %q contains %q", name, seg)
		}
		if strings.ContainsAny(seg, "\\:") {
			return fmt.Errorf("archive path %q contains a forbidden character", name)
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("archive path %q contains a control character", name)
			}
		}
	}
	return nil
}

// Inspect 对 tar 明文做白名单校验并返回清单。入库（Pack 之后）与展开（apply）
// 都应调用它：
//   - 明文不超过 1 MiB；
//   - 条目类型只能是 TypeReg / TypeRegA / TypeDir，且必须是纯 ustar 头（无 pax/GNU 扩展）；
//   - 路径无绝对路径、空段、"."、".."；无重复条目，也不允许同一路径既是文件又是目录/父目录；
//   - 根下必须有普通文件 SKILL.md。
func Inspect(archive []byte) (*Archive, error) {
	if len(archive) > MaxArchiveBytes {
		return nil, errTooLarge(int64(len(archive)))
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	result := &Archive{}
	files := map[string]bool{}
	dirs := map[string]bool{}         // 显式目录条目
	implicitDirs := map[string]bool{} // 由文件/子目录路径隐含的父目录
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid skill archive: %w", err)
		}
		isDir := false
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
		case tar.TypeDir:
			isDir = true
		default:
			return nil, fmt.Errorf("archive entry %q has unsupported type %q: only regular files and directories are allowed", hdr.Name, string(hdr.Typeflag))
		}
		if hdr.Format != tar.FormatUSTAR {
			return nil, fmt.Errorf("archive entry %q is not a plain ustar header (format %v)", hdr.Name, hdr.Format)
		}
		name := hdr.Name
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		if err := validateEntryPath(name); err != nil {
			return nil, err
		}
		if files[name] || dirs[name] {
			return nil, fmt.Errorf("duplicate archive path %q", name)
		}
		if isDir {
			dirs[name] = true
		} else {
			if implicitDirs[name] {
				return nil, fmt.Errorf("archive path %q is both a file and a directory", name)
			}
			files[name] = true
		}
		segs := strings.Split(name, "/")
		for i := 1; i < len(segs); i++ {
			parent := strings.Join(segs[:i], "/")
			if files[parent] {
				return nil, fmt.Errorf("archive path %q is under file %q", name, parent)
			}
			implicitDirs[parent] = true
		}

		entry := Entry{Path: name, Dir: isDir, Mode: fs.FileMode(hdr.Mode).Perm()}
		if !isDir {
			entry.Size = hdr.Size
			if name == SkillFileName {
				content, err := io.ReadAll(io.LimitReader(tr, MaxArchiveBytes+1))
				if err != nil {
					return nil, fmt.Errorf("invalid skill archive: %w", err)
				}
				result.SkillMD = content
			} else if _, err := io.Copy(io.Discard, tr); err != nil {
				return nil, fmt.Errorf("invalid skill archive: %w", err)
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	if !files[SkillFileName] {
		return nil, fmt.Errorf("skill archive has no %s at its root", SkillFileName)
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}

// ParseName 从 SKILL.md 的 YAML frontmatter（首行 "---" 到下一个 "---" 行）里取顶层 name。
// 只做按行扫描，不是完整 YAML：description 等字段里未加引号的冒号不会让解析失败。
// 不支持块标量写法（name: > 之类）。
func ParseName(skillMD []byte) (string, error) {
	text := strings.TrimPrefix(string(skillMD), "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", fmt.Errorf("%s has no YAML frontmatter (must start with ---)", SkillFileName)
	}
	closed := false
	name := ""
	found := false
	for _, line := range lines[1:] {
		if strings.TrimRight(line, " \t") == "---" {
			closed = true
			break
		}
		if found || !strings.HasPrefix(line, "name:") {
			continue
		}
		found = true
		value := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		name = value
	}
	if !closed {
		return "", fmt.Errorf("%s frontmatter is not closed (missing second ---)", SkillFileName)
	}
	if !found || name == "" {
		return "", fmt.Errorf("%s frontmatter has no name", SkillFileName)
	}
	return name, nil
}

// CheckName 要求 SKILL.md 的 frontmatter name 与 skill id 一致。
func CheckName(skillMD []byte, id string) error {
	name, err := ParseName(skillMD)
	if err != nil {
		return err
	}
	if name != id {
		return fmt.Errorf("%s frontmatter name %q does not match skill id %q", SkillFileName, name, id)
	}
	return nil
}

// Validate 是入库与展开共用的完整校验：白名单 + SKILL.md frontmatter name == id。
func Validate(archive []byte, id string) (*Archive, error) {
	a, err := Inspect(archive)
	if err != nil {
		return nil, err
	}
	if err := CheckName(a.SkillMD, id); err != nil {
		return nil, err
	}
	return a, nil
}
