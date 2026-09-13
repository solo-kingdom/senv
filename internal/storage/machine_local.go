// machine_local：机器本地工件（machine-local artifact）登记表。
//
// 机器本地工件指只在单机才有意义、不得进入同步通道或版本库的文件。
// 本表是唯一事实来源；server 同步收集、TUI 首屏指纹、rekey 预检、
// orphan/一致性检测与 git add/.gitignore 都从这里派生行为。
//
// 新增任何写入 dataPath/configPath 顶层、且不属于 vault 受管条目的文件时，
// 必须在此登记；否则会被当成受管条目（config 密文）同步、rekey 或误判。
package storage

// MachineLocalScope 标识工件所属的根目录。
type MachineLocalScope int

const (
	// ScopeDataPath 表示工件位于 dataPath（vault 数据树）顶层。
	ScopeDataPath MachineLocalScope = iota
	// ScopeConfigPath 表示工件位于 configPath（仓库根）顶层。
	ScopeConfigPath
)

// MachineLocalArtifact 是一条机器本地工件登记。
type MachineLocalArtifact struct {
	// Name 是单路径段文件名或目录名（不含路径分隔符）。
	Name string
	// Scope 是工件所在的根目录。
	Scope MachineLocalScope
	// IsDir 表示目录（影响 .gitignore 与 git 排除路径的形状）。
	IsDir bool
}

// machineLocalArtifacts 是机器本地工件的权威清单，顺序即 .gitignore 写出顺序。
var machineLocalArtifacts = []MachineLocalArtifact{
	// dataPath（vault 数据树）
	{Name: "tui-snapshot.enc", Scope: ScopeDataPath},      // TUI 首屏加密快照
	{Name: ".senv-sync-state.json", Scope: ScopeDataPath}, // server 同步状态
	{Name: ".senv-sync.lock", Scope: ScopeDataPath},       // 同步进程锁
	// configPath（仓库根）
	{Name: ServerTokenFile, Scope: ScopeConfigPath},       // server provider token
	{Name: "mcp-exports.json", Scope: ScopeConfigPath},    // MCP 导出账本
	{Name: "agent-pointers.json", Scope: ScopeConfigPath}, // 本机 agent 指向（ADR-0003）
	{Name: vaultMutationLockFile, Scope: ScopeConfigPath}, // vault 变更锁
	{Name: "cache", Scope: ScopeConfigPath, IsDir: true},  // 机器本地缓存目录（模型目录等）
}

// IsMachineLocalDataArtifact 报告 name 是否是 dataPath 顶层的机器本地工件。
// 仅做单路径段精确匹配，不解析子路径或通配。
func IsMachineLocalDataArtifact(name string) bool {
	return isMachineLocalArtifact(name, ScopeDataPath)
}

// IsMachineLocalConfigArtifact 报告 name 是否是 configPath 顶层的机器本地工件。
func IsMachineLocalConfigArtifact(name string) bool {
	return isMachineLocalArtifact(name, ScopeConfigPath)
}

func isMachineLocalArtifact(name string, scope MachineLocalScope) bool {
	for _, artifact := range machineLocalArtifacts {
		if artifact.Scope == scope && artifact.Name == name {
			return true
		}
	}
	return false
}

// MachineLocalGitIgnoreEntries 返回全部机器本地工件的 .gitignore 条目。
// 目录追加 "/"；裸名在任意深度匹配。
func MachineLocalGitIgnoreEntries() []string {
	out := make([]string, 0, len(machineLocalArtifacts))
	for _, artifact := range machineLocalArtifacts {
		if artifact.IsDir {
			out = append(out, artifact.Name+"/")
			continue
		}
		out = append(out, artifact.Name)
	}
	return out
}

// MachineLocalGitExcludeGlobs 返回 git add 的排除 pathspec（任意深度），
// 作为 .gitignore 缺失时的兜底防线。
func MachineLocalGitExcludeGlobs() []string {
	out := make([]string, 0, len(machineLocalArtifacts)*2)
	for _, artifact := range machineLocalArtifacts {
		out = append(out, ":(exclude,glob)**/"+artifact.Name)
		if artifact.IsDir {
			out = append(out, ":(exclude,glob)**/"+artifact.Name+"/**")
		}
	}
	return out
}
