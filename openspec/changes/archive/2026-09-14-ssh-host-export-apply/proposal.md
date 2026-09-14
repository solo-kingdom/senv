## Why

ADR-0023 定案：`host export` 现状是纯渲染到 stdout，导出后还要手工接线（README 教 `>> ~/.ssh/config.d/senv` + 手写 `Include`），私钥落盘要逐个手动 `keypair materialize`，按组导出无从谈起。四个诉求一次解决：按组导出、自动落盘关联密钥、导出位置自动推断、导出后直接可用。

## What Changes

- **BREAKING** `senv host export` 默认从「渲染到 stdout」改为「应用模式」：按组整树维护 `~/.ssh/senv/`（`groups/<组>.conf` 组片段 + `keys/<组>/<名>` 落盘私钥，未分组入 `_ungrouped`），自动落盘被引用密钥（已存在跳过、缺失 keypair 只 warning 不阻断），幂等注册 `~/.ssh/config` 顶部一行 glob `Include ~/.ssh/senv/groups/*.conf`；`--output -` 保留纯渲染预览
- `export` 新增 `--group` 过滤；apply 模式以组片段为写入单元（整文件重渲染，不回读合并）；跨组 ProxyJump 闭包并入发起导出的 fragment
- 新增 `senv host unexport`：幂等摘除注册行 + 删除组片段，不碰 vault 档案与落盘私钥
- 新增 `senv keypair prune`：列出并清理未被引用的落盘私钥（显式确认，不自动删）
- `keypair materialize` 与 TUI 落盘/导出预览自动跟随分组布局（`IdentityFile` 指向 `keys/<组>/<名>`）
- 分组名写入/编辑时新增 `/` 校验；组在 vault 中消失时 export 自动清理对应组片段（防幽灵别名），未引用落盘私钥只 warning

## Capabilities

### New Capabilities

（无——新行为全部落在既有 ssh-assets 能力内）

### Modified Capabilities

- `ssh-assets`: 导出片段 requirement 改为应用模式（组片段、自动落盘、Include 注册、`--output -` 预览）；materialize 落盘路径改为分组布局；新增 unexport/prune requirement；分组名校验收紧；TUI 预览与确认路径显示同步更新

## Impact

- `cmd/ssh.go`（export 改造 + unexport/prune 新命令）、`internal/ssh`（路径推导分组化、注册/撤回、纯渲染与应用分离）、`internal/tui/ssh_tab.go`（预览/确认路径）、`.agents/skills/senv-cli/SKILL.md` 与 README/EXAMPLES（AGENTS.md「同一变更」约束）
- 破坏面：依赖 `export` stdout 默认行为的既有脚本；ADR-0001 扁平布局被取代（老机器残留不迁移不删除，由「未引用 warning」与顶部 glob 压盖，见 ADR-0023 Consequences）

## Non-goals

- TUI 新增「一键应用导出」交互（本期最小对齐，后续独立 change）
- MCP SSH 工具保持只读，不新增写操作
- 旧扁平落盘文件与手写 `config.d/senv` Include 的自动迁移/删除
- vault 同步协议与 server 侧的任何改动

## 安全性分析

export 新增副作用但解密面不扩大：被引用 keypair 本就随 vault 解锁可用，取分组仅为渲染 `IdentityFile` 路径。私钥落盘沿用 0700/0600 与「存在不覆盖」语义；`~/.ssh/config` 只追加/删除 senv 拥有的注册行，写入前留 `~/.ssh/config.senv-bak`，用户其余内容一字不动；unexport/prune 不触碰 vault 密文。组片段自动清理防止失效别名被 glob 继续提供；未引用私钥只警告不自动删——它可能被你手写 ssh config（senv 不可见）引用。

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 单仓变更 |

## 验收标准

- [ ] `senv host export` 默认应用：写组片段 + 落盘缺失密钥 + 注册 Include；`--output -` 纯渲染到 stdout
- [ ] `--group prod` 只重建 prod 组片段；跨组 ProxyJump 闭包并入发起 fragment；组消失自动清理幽灵片段
- [ ] `host unexport` 摘除注册行并删除组片段；`keypair prune` 先列后删；二者均不动 vault 与未选中的私钥
- [ ] `go test ./... -race` 全绿；SKILL.md、README/EXAMPLES 已同步更新
