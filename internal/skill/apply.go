package skill

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/securefs"
)

// OwnedMarker 是 senv 在 apply 出的 skill 根目录下写入的托管标记文件。
// 带它的目录才允许被 senv 备份并覆盖；没有它的同名目录一律视为用户或其他工具所有。
// 带版本安装时写入 JSON 元数据（OwnedMeta），无版本或低层调用保持空文件。
const OwnedMarker = ".senv-owned"

// OwnedMeta 是写入 .senv-owned 的本地已装版本元数据。
type OwnedMeta struct {
	ID        string    `json:"id"`
	Version   string    `json:"version,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	AppliedAt time.Time `json:"applied_at,omitempty"`
}

// ReadOwnedMeta 读取目标目录下的 .senv-owned 元数据。
// 空文件或非 JSON 普通文件均视为已托管但版本未知（ok=true, meta.Version=""）。
func ReadOwnedMeta(dir string) (OwnedMeta, bool, error) {
	markerPath := filepath.Join(dir, OwnedMarker)
	fi, err := os.Lstat(markerPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OwnedMeta{}, false, nil
		}
		return OwnedMeta{}, false, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return OwnedMeta{}, false, nil
	}
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		return OwnedMeta{}, false, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return OwnedMeta{ID: filepath.Base(dir)}, true, nil
	}
	var meta OwnedMeta
	if err := json.Unmarshal(trimmed, &meta); err != nil {
		return OwnedMeta{ID: filepath.Base(dir)}, true, nil
	}
	return meta, true, nil
}

// DefaultRoots 是 apply 的目标根（相对 Base：家目录或项目根）。不含 kiro。
var DefaultRoots = []string{".agents/skills", ".claude/skills"}

// 目标简称（CLI --target）。
const (
	TargetAgents = "agents"
	TargetClaude = "claude"
	TargetAll    = "all"
)

// TargetShortName 把相对根路径映射回 CLI 简称（agents / claude）。
func TargetShortName(root string) string {
	switch filepath.ToSlash(root) {
	case ".agents/skills":
		return TargetAgents
	case ".claude/skills":
		return TargetClaude
	default:
		return root
	}
}

// RootForTarget 把 agents / claude 映射成相对根路径。
func RootForTarget(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case TargetAgents, "agent":
		return ".agents/skills", nil
	case TargetClaude, "claude-code":
		return ".claude/skills", nil
	default:
		return "", fmt.Errorf("unknown skill apply target %q (want agents, claude, or all)", name)
	}
}

// ResolveRoots 解析 --target 列表：空或含 all → DefaultRoots；否则按简称展开并去重保序。
func ResolveRoots(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return append([]string(nil), DefaultRoots...), nil
	}
	seen := map[string]bool{}
	var roots []string
	for _, raw := range targets {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.EqualFold(part, TargetAll) {
				return append([]string(nil), DefaultRoots...), nil
			}
			root, err := RootForTarget(part)
			if err != nil {
				return nil, err
			}
			if seen[root] {
				continue
			}
			seen[root] = true
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no skill apply targets resolved")
	}
	return roots, nil
}

// Action 是单个目标根上的计划动作。
type Action string

const (
	ActionCreate            Action = "create"
	ActionSkip              Action = "skip"
	ActionSkipConflict      Action = "skip-conflict"
	ActionSkipMissingPath   Action = "skip-missing-path"
	ActionSkipProjectOnly   Action = "skip-project-only"
	ActionSkipScopeMismatch Action = "skip-scope-mismatch"
	ActionBackupOverwrite   Action = "backup+overwrite"
	ActionRemove            Action = "remove"
)

// ExpandInstallPath 把存储的 install_path（~ / ~/... / 绝对路径）展开为本机路径。
func ExpandInstallPath(home, raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil
	}
	if p == "~" {
		if home == "" {
			return "", fmt.Errorf("cannot expand %q: home directory is empty", raw)
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		if home == "" {
			return "", fmt.Errorf("cannot expand %q: home directory is empty", raw)
		}
		return filepath.Join(home, filepath.FromSlash(p[2:])), nil
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("install_path %q must be absolute or start with ~/", raw)
	}
	return filepath.Clean(p), nil
}

// Changes 报告该动作是否会写入磁盘（含 apply 与 unapply）。
func (a Action) Changes() bool {
	return a == ActionCreate || a == ActionBackupOverwrite || a == ActionRemove
}

// PlanItem 是一个 skill 在一个目标根上的计划。
type PlanItem struct {
	ID               string
	Root             string // 相对 Base 的根，如 ".agents/skills"
	Target           string // 绝对路径 <base>/<root>/<id>
	Action           Action
	Reason           string
	InstalledVersion string
	TargetVersion    string
	// Backup 仅在 ActionBackupOverwrite 且执行后填充：旧树被移到的位置。
	Backup string
}

// ApplyOptions 是 PlanWithOptions / ApplyWithOptions 的选项。
type ApplyOptions struct {
	ExtraInfo string
	Version   string
}

// Result 是一个目标根的执行结果。
type Result struct {
	PlanItem
	Err error
}

// Applier 把 skill 归档展开到 Base 下的 skills 根。
//
// Base 是家目录（全局）或项目根（当前目录）。备份与崩溃遗留的临时目录放在
// StateDir（默认 <Base>/.local/state/senv/skill-apply；项目 scope 时应改成用户家目录下同路径），
// 须在任何 skills 根之外。StateDir 必须与各 skills 根在同一文件系统上（展开靠 rename）。
type Applier struct {
	Base     string
	Roots    []string
	StateDir string
	Now      func() time.Time
}

// NewApplier 以 base 为 skills 根基准创建 Applier。全局安装传 UserHomeDir；项目安装传 Getwd。
func NewApplier(base string) *Applier {
	return &Applier{
		Base:     base,
		Roots:    append([]string(nil), DefaultRoots...),
		StateDir: filepath.Join(base, ".local", "state", "senv", "skill-apply"),
		Now:      time.Now,
	}
}

type treeNode struct {
	dir  bool
	exec bool
	data []byte
}

// expanded 是通过校验、已读入内存的归档（压缩前 ≤8 MiB，压缩后 ≤1 MiB），含可选追加的 extra_info 与目标版本。
type expanded struct {
	id      string
	tree    map[string]treeNode // 文件与目录（含隐含父目录），不含根
	bytes   []byte              // 解压后的 ustar tar
	extra   string
	version string
	sha256  string
}

// AppendExtraToSkillMD 将扩展信息（extra_info）作为受管区块追加到 SKILL.md 末尾。
// extra 为空（或纯空白）时原样返回 orig。
func AppendExtraToSkillMD(orig []byte, extra string) []byte {
	trimmed := strings.TrimSpace(extra)
	if trimmed == "" {
		return orig
	}
	var buf bytes.Buffer
	buf.Write(orig)
	if len(orig) > 0 && orig[len(orig)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString("\n<!-- senv:extra:start -->\n## Extra Context (senv)\n\n")
	buf.WriteString(trimmed)
	buf.WriteString("\n<!-- senv:extra:end -->\n")
	return buf.Bytes()
}

func prepare(id string, archive []byte, opts ApplyOptions) (*expanded, error) {
	if _, err := Validate(archive, id); err != nil {
		return nil, err
	}
	tarBytes, _, err := decodeArchive(archive)
	if err != nil {
		return nil, err
	}
	trimmedExtra := strings.TrimSpace(opts.ExtraInfo)
	sum := sha256.Sum256(archive)
	ex := &expanded{
		id:      id,
		tree:    map[string]treeNode{},
		bytes:   tarBytes,
		extra:   trimmedExtra,
		version: strings.TrimSpace(opts.Version),
		sha256:  hex.EncodeToString(sum[:]),
	}
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid skill archive: %w", err)
		}
		isDir := hdr.Typeflag == tar.TypeDir
		name := strings.TrimSuffix(hdr.Name, "/")
		if name == OwnedMarker || strings.HasPrefix(name, OwnedMarker+"/") {
			return nil, fmt.Errorf("archive uses reserved name %q", OwnedMarker)
		}
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return nil, fmt.Errorf("archive path %q escapes the skill root", name)
		}
		segs := strings.Split(name, "/")
		for i := 1; i < len(segs); i++ {
			parent := strings.Join(segs[:i], "/")
			if _, ok := ex.tree[parent]; !ok {
				ex.tree[parent] = treeNode{dir: true}
			}
		}
		if isDir {
			ex.tree[name] = treeNode{dir: true}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxUncompressedArchiveBytes+1))
		if err != nil {
			return nil, fmt.Errorf("invalid skill archive: %w", err)
		}
		if name == "SKILL.md" && trimmedExtra != "" {
			data = AppendExtraToSkillMD(data, trimmedExtra)
		}
		ex.tree[name] = treeNode{exec: fs.FileMode(hdr.Mode).Perm()&0o111 != 0, data: data}
	}
	return ex, nil
}

// Plan 校验归档并给出每个根上的动作，不写任何东西。
func (a *Applier) Plan(id string, archive []byte) ([]PlanItem, error) {
	return a.PlanWithOptions(id, archive, ApplyOptions{})
}

// PlanWithExtra 校验归档并在计算内容等价性时把 extra 追加到 SKILL.md 末尾。
func (a *Applier) PlanWithExtra(id string, archive []byte, extra string) ([]PlanItem, error) {
	return a.PlanWithOptions(id, archive, ApplyOptions{ExtraInfo: extra})
}

// PlanWithOptions 校验归档并根据 ApplyOptions（extra_info、目标版本）给出每个根上的动作。
func (a *Applier) PlanWithOptions(id string, archive []byte, opts ApplyOptions) ([]PlanItem, error) {
	ex, err := prepare(id, archive, opts)
	if err != nil {
		return nil, err
	}
	items := make([]PlanItem, 0, len(a.Roots))
	for _, root := range a.Roots {
		items = append(items, a.planRoot(ex, root))
	}
	return items, nil
}

func (a *Applier) rootDir(root string) string {
	return filepath.Join(a.Base, filepath.FromSlash(root))
}

func validateSkillTargetID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("invalid skill id %q", id)
	}
	if err := securefs.ValidateSegment(id); err != nil {
		return fmt.Errorf("invalid skill id %q: %w", id, err)
	}
	return nil
}

func rootSegments(root string) ([]string, error) {
	cleaned := strings.Trim(filepath.ToSlash(root), "/")
	if cleaned == "" {
		return nil, fmt.Errorf("skill root is empty")
	}
	parts := strings.Split(cleaned, "/")
	for _, seg := range parts {
		if err := securefs.ValidateSegment(seg); err != nil {
			return nil, fmt.Errorf("invalid skill root %q: %w", root, err)
		}
	}
	return parts, nil
}

// inspectParentAndTarget validates Base, root intermediate directories (without
// enumerating sibling skills), and returns target path segments relative to Base.
func (a *Applier) inspectParentAndTarget(root, id string) (targetSegs []string, baseRoot *securefs.Root, parentExists bool, conflictReason string) {
	if err := validateSkillTargetID(id); err != nil {
		return nil, nil, false, err.Error()
	}
	rootSegs, err := rootSegments(root)
	if err != nil {
		return nil, nil, false, err.Error()
	}
	targetSegs = append(append([]string(nil), rootSegs...), id)

	baseRoot, err = securefs.OpenRoot(a.Base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return targetSegs, nil, false, ""
		}
		if errors.Is(err, securefs.ErrSymlink) {
			return targetSegs, nil, false, "base directory is a symbolic link (not managed by senv)"
		}
		return targetSegs, nil, false, fmt.Sprintf("cannot inspect base directory: %v", err)
	}

	cur := a.Base
	for _, seg := range rootSegs {
		cur = filepath.Join(cur, seg)
		fi, statErr := os.Lstat(cur)
		switch {
		case errors.Is(statErr, fs.ErrNotExist):
			return targetSegs, baseRoot, false, ""
		case statErr != nil:
			_ = baseRoot.Close()
			return targetSegs, nil, false, fmt.Sprintf("cannot inspect parent directory: %v", statErr)
		case fi.Mode()&fs.ModeSymlink != 0:
			_ = baseRoot.Close()
			return targetSegs, nil, false, "parent directory is a symbolic link (not managed by senv)"
		case !fi.IsDir():
			_ = baseRoot.Close()
			return targetSegs, nil, false, "parent path exists and is not a directory"
		}
	}
	return targetSegs, baseRoot, true, ""
}

func hasOwnedMarkerInRoot(baseRoot *securefs.Root, targetSegs []string) bool {
	if baseRoot == nil {
		return false
	}
	mode, err := baseRoot.Mode(append(append([]string(nil), targetSegs...), OwnedMarker)...)
	return err == nil && mode.IsRegular()
}

func (a *Applier) planRoot(ex *expanded, root string) PlanItem {
	target := filepath.Join(a.rootDir(root), ex.id)
	item := PlanItem{ID: ex.id, Root: root, Target: target, TargetVersion: ex.version}
	targetSegs, baseRoot, parentExists, conflict := a.inspectParentAndTarget(root, ex.id)
	if baseRoot != nil {
		defer baseRoot.Close()
	}
	if conflict != "" {
		item.Action, item.Reason = ActionSkipConflict, conflict
		return item
	}
	if !parentExists {
		item.Action, item.Reason = ActionCreate, "not present"
		return item
	}
	fi, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		item.Action, item.Reason = ActionCreate, "not present"
	case err != nil:
		item.Action, item.Reason = ActionSkipConflict, fmt.Sprintf("cannot inspect target: %v", err)
	case fi.Mode()&fs.ModeSymlink != 0:
		item.Action, item.Reason = ActionSkipConflict, "target is a symbolic link (not managed by senv)"
	case !fi.IsDir():
		item.Action, item.Reason = ActionSkipConflict, "target exists and is not a directory"
	case !hasOwnedMarkerInRoot(baseRoot, targetSegs):
		item.Action, item.Reason = ActionSkipConflict, "target directory has no "+OwnedMarker+" (not managed by senv)"
	default:
		owned, _, _ := ReadOwnedMeta(target)
		item.InstalledVersion = owned.Version
		same, err := treeMatches(target, ex.tree)
		switch {
		case err != nil:
			item.Action, item.Reason = ActionSkipConflict, fmt.Sprintf("cannot compare target: %v", err)
		case same && (ex.version == "" || owned.Version == ex.version):
			item.Action, item.Reason = ActionSkip, "content already up to date"
		case same:
			item.Action = ActionBackupOverwrite
			if owned.Version != "" {
				item.Reason = fmt.Sprintf("installed version %s -> %s", owned.Version, ex.version)
			} else {
				item.Reason = fmt.Sprintf("version metadata updated to %s", ex.version)
			}
		default:
			item.Action = ActionBackupOverwrite
			switch {
			case owned.Version != "" && ex.version != "" && owned.Version != ex.version:
				item.Reason = fmt.Sprintf("installed %s differs from target %s; old tree will be backed up", owned.Version, ex.version)
			case owned.Version != "":
				item.Reason = fmt.Sprintf("managed directory differs (local drift at %s); old tree will be backed up", owned.Version)
			default:
				item.Reason = "managed directory differs; old tree will be backed up"
			}
		}
	}
	return item
}

// treeMatches 比较磁盘目录与归档树。比较时排除根下的 .senv-owned；
// 磁盘上的符号链接或特殊文件一律视为不等价。
func treeMatches(dir string, want map[string]treeNode) (bool, error) {
	seen := 0
	same := true
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == OwnedMarker {
			return nil
		}
		n, ok := want[name]
		if !ok {
			same = false
			return filepath.SkipAll
		}
		seen++
		switch {
		case d.IsDir():
			if !n.dir {
				same = false
				return filepath.SkipAll
			}
		case d.Type().IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if n.dir || fi.Size() != int64(len(n.data)) || (fi.Mode().Perm()&0o111 != 0) != n.exec {
				same = false
				return filepath.SkipAll
			}
			got, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, n.data) {
				same = false
				return filepath.SkipAll
			}
		default:
			same = false
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return same && seen == len(want), nil
}

// Apply 校验归档后，对每个根重新规划并执行。create 与 backup+overwrite 才会写盘；
// skip / skip-conflict 只返回计划。校验失败时一个根都不会碰。
// 单个根失败不影响其它根，错误记在对应 Result.Err。
func (a *Applier) Apply(id string, archive []byte) ([]Result, error) {
	return a.ApplyWithOptions(id, archive, ApplyOptions{})
}

// ApplyWithExtra 与 Apply 相同，但会在展开后的 SKILL.md 末尾追加 extra。
func (a *Applier) ApplyWithExtra(id string, archive []byte, extra string) ([]Result, error) {
	return a.ApplyWithOptions(id, archive, ApplyOptions{ExtraInfo: extra})
}

// ApplyWithOptions 根据 ApplyOptions（extra_info、目标版本）展开归档并写入 .senv-owned。
func (a *Applier) ApplyWithOptions(id string, archive []byte, opts ApplyOptions) ([]Result, error) {
	ex, err := prepare(id, archive, opts)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(a.Roots))
	for _, root := range a.Roots {
		item := a.planRoot(ex, root)
		res := Result{PlanItem: item}
		if item.Action.Changes() {
			backup, err := a.install(ex, item)
			res.Backup = backup
			res.Err = err
		}
		results = append(results, res)
	}
	return results, nil
}

func (a *Applier) install(ex *expanded, item PlanItem) (backup string, err error) {
	tmpRoot := filepath.Join(a.StateDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o700); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	work, err := os.MkdirTemp(tmpRoot, ex.id+"-*")
	if err != nil {
		return "", fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	staged := filepath.Join(work, ex.id)
	if err := extractTree(staged, ex.bytes, ex.extra); err != nil {
		return "", err
	}
	var ownedBytes []byte
	if ex.version != "" {
		meta := OwnedMeta{
			ID:        ex.id,
			Version:   ex.version,
			SHA256:    ex.sha256,
			AppliedAt: a.Now().UTC(),
		}
		ownedBytes, err = json.Marshal(meta)
		if err != nil {
			return "", fmt.Errorf("marshal %s: %w", OwnedMarker, err)
		}
		ownedBytes = append(ownedBytes, '\n')
	}
	if err := os.WriteFile(filepath.Join(staged, OwnedMarker), ownedBytes, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", OwnedMarker, err)
	}

	rootSegs, err := rootSegments(item.Root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.Base, 0o755); err != nil {
		return "", fmt.Errorf("create base directory: %w", err)
	}
	baseRoot, err := securefs.OpenRoot(a.Base)
	if err != nil {
		return "", fmt.Errorf("open base directory: %w", err)
	}
	defer baseRoot.Close()
	if err := baseRoot.EnsurePrivateParents(rootSegs, 0o755); err != nil {
		return "", fmt.Errorf("create skills root: %w", err)
	}
	if item.Action == ActionBackupOverwrite {
		backup, err = a.backupPath(ex.id)
		if err != nil {
			return "", err
		}
		if err := os.Rename(item.Target, backup); err != nil {
			return "", renameErr("move old tree to backup", err)
		}
	}
	if err := os.Rename(staged, item.Target); err != nil {
		if backup != "" {
			if rbErr := os.Rename(backup, item.Target); rbErr != nil {
				return "", fmt.Errorf("%w; restoring the old tree from %s also failed: %v", renameErr("install new tree", err), backup, rbErr)
			}
		}
		return "", renameErr("install new tree", err)
	}
	return backup, nil
}

func renameErr(what string, err error) error {
	return fmt.Errorf("%s: %w (the senv state directory and the skills root must be on the same filesystem)", what, err)
}

func (a *Applier) backupPath(id string) (string, error) {
	dir := filepath.Join(a.StateDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	stamp := a.Now().UTC().Format("20060102T150405.000000000Z")
	for i := 0; ; i++ {
		name := fmt.Sprintf("%s.senv-backup-%s", id, stamp)
		if i > 0 {
			name = fmt.Sprintf("%s-%d", name, i)
		}
		path := filepath.Join(dir, name)
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, fs.ErrNotExist) {
			return path, nil
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect backup path %s: %w", path, statErr)
		}
	}
}

// UnapplyPlan 给出撤回计划：只对带 OwnedMarker 的托管目录计划 remove；不存在则 skip；
// 软链 / 非目录 / 无标记一律 skip-conflict。不需要归档正文。
func (a *Applier) UnapplyPlan(id string) []PlanItem {
	items := make([]PlanItem, 0, len(a.Roots))
	for _, root := range a.Roots {
		items = append(items, a.unapplyRoot(id, root))
	}
	return items
}

func (a *Applier) unapplyRoot(id, root string) PlanItem {
	target := filepath.Join(a.rootDir(root), id)
	item := PlanItem{ID: id, Root: root, Target: target}
	targetSegs, baseRoot, parentExists, conflict := a.inspectParentAndTarget(root, id)
	if baseRoot != nil {
		defer baseRoot.Close()
	}
	if conflict != "" {
		item.Action, item.Reason = ActionSkipConflict, conflict
		return item
	}
	if !parentExists {
		item.Action, item.Reason = ActionSkip, "not present"
		return item
	}
	fi, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		item.Action, item.Reason = ActionSkip, "not present"
	case err != nil:
		item.Action, item.Reason = ActionSkipConflict, fmt.Sprintf("cannot inspect target: %v", err)
	case fi.Mode()&fs.ModeSymlink != 0:
		item.Action, item.Reason = ActionSkipConflict, "target is a symbolic link (not managed by senv)"
	case !fi.IsDir():
		item.Action, item.Reason = ActionSkipConflict, "target exists and is not a directory"
	case !hasOwnedMarkerInRoot(baseRoot, targetSegs):
		item.Action, item.Reason = ActionSkipConflict, "target directory has no "+OwnedMarker+" (not managed by senv)"
	default:
		item.Action, item.Reason = ActionRemove, "senv-managed directory will be removed"
	}
	return item
}

// Unapply 按 UnapplyPlan 删除托管目录。skip / skip-conflict 不碰盘；单根失败记在 Result.Err。
func (a *Applier) Unapply(id string) []Result {
	items := a.UnapplyPlan(id)
	results := make([]Result, 0, len(items))
	for _, item := range items {
		res := Result{PlanItem: item}
		if item.Action == ActionRemove {
			if err := a.removeManagedTree(item.Root, item.ID); err != nil {
				res.Err = fmt.Errorf("remove %s: %w", item.Target, err)
			}
		}
		results = append(results, res)
	}
	return results
}

func (a *Applier) removeManagedTree(root, id string) error {
	targetSegs, baseRoot, parentExists, conflict := a.inspectParentAndTarget(root, id)
	if baseRoot != nil {
		defer baseRoot.Close()
	}
	if conflict != "" {
		return errors.New(conflict)
	}
	if !parentExists || baseRoot == nil {
		return nil
	}
	if !hasOwnedMarkerInRoot(baseRoot, targetSegs) {
		return fmt.Errorf("target directory has no %s (not managed by senv)", OwnedMarker)
	}
	return baseRoot.RemoveTree(targetSegs...)
}

// extractTree 把（已校验并解压的）tar 归档展开到不存在的 dest。只取权限位的 Perm 部分
// （丢弃 setuid/setgid/sticky），并保证属主可读写（目录可进入）、组/其他不可写。
func extractTree(dest string, tarBytes []byte, extra string) error {
	if err := os.Mkdir(dest, 0o755); err != nil {
		return fmt.Errorf("create staged skill directory: %w", err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	type dirMode struct {
		path string
		perm fs.FileMode
	}
	var dirs []dirMode
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid skill archive: %w", err)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return fmt.Errorf("archive path %q escapes the skill root", name)
		}
		path := filepath.Join(dest, filepath.FromSlash(name))
		perm := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			dirs = append(dirs, dirMode{path, (perm | 0o700) &^ 0o022})
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			if name == "SKILL.md" && strings.TrimSpace(extra) != "" {
				orig, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
				if err != nil {
					_ = f.Close()
					return err
				}
				if _, err := f.Write(AppendExtraToSkillMD(orig, extra)); err != nil {
					_ = f.Close()
					return err
				}
			} else if _, err := io.Copy(f, io.LimitReader(tr, hdr.Size)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Chmod(path, (perm|0o600)&^0o022); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %q", hdr.Name, string(hdr.Typeflag))
		}
	}
	// 目录权限最后设置：先建内容，再按归档收紧/放开，避免中途不可写。
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].path) > len(dirs[j].path) })
	for _, d := range dirs {
		if err := os.Chmod(d.path, d.perm); err != nil {
			return err
		}
	}
	return nil
}
