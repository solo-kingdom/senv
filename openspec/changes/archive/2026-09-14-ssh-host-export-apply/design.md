## Context

现状与约束（动因见 proposal.md 与 ADR-0023，此处不重复）：

- `internal/ssh/host.go` 的 `Export(alias)` 是纯渲染：只列 keypair 名单不解密（host.go:177 的刻意优化），`IdentityFile` 指向 ADR-0001 扁平路径；`cmd/ssh.go:398` 负责 stdout/`--output` 写出；TUI 预览（`internal/tui/ssh_tab.go:649,695`）与 MCP 只读路径复用同一渲染。
- `MaterializePath(name)`（manager.go:314）是唯一路径约定入口；`Materialize`（manager.go:324）已存在拒绝覆盖。
- 事务先例：ADR-0012 的 `<file>.senv-bak`；宽松写入先例：ADR-0020 D4（悬空 keypair 逐条 warning 不阻断）。

## Goals / Non-Goals

**Goals:**
- 纯渲染与副作用应用在代码层分离：TUI 预览、MCP 只读、CLI `--output` 走纯渲染；默认 export 走应用编排
- 分组路径推导只有一个入口，渲染、落盘、prune、TUI 显示全部共用
- `~/.ssh/config` 的改动被限定为「一行 senv 拥有行」的增删，可事务、可幂等、可撤回

**Non-Goals:**
- vault 数据格式与同步协议（`group` 字段已有，零数据迁移）
- TUI 新交互、MCP 写工具（proposal 已列）

## Decisions

### D1 纯渲染与应用分层

`Export` 拆为两层：

- `Render(filter) (*RenderResult, error)`：纯函数式。输入过滤（`--host`/`--group`），解密被引用 keypair 取 `group` 推导 `IdentityFile` 路径（放弃只列名单的优化，vault 已解锁，成本忽略），做 proxyJump 悬空校验与跨组闭包并入，返回 `map[组名]片段文本`、warning 清单（缺失 keypair、未引用落盘文件）、被引用 keypair 集合。无任何文件副作用。
- `Apply(filter) (*ApplyResult, error)`：编排 `Render` → 写组片段 → 落盘缺失密钥 → 注册 Include →（仅全量）幽灵片段清理 → 汇总摘要。两阶段保证：**渲染失败（如 proxyJump 悬空）时零文件副作用**。

TUI 与 MCP 继续调用 `Render`；`--output` 路径 = `Render` + 单文件写出。

### D2 路径推导唯一入口

新增未导出的 `groupDir(group string) string`（空组 → `_ungrouped`；其余原样）。`fragmentPath(group)` = `~/.ssh/senv/groups/<groupDir>.conf`，`materializePath(entry)` = `~/.ssh/senv/keys/<groupDir>/<name>`。渲染与落盘共用，杜绝两处漂移。`MaterializePath` 旧函数改签名或内联替换（仅测试引用，无外部消费者）。

### D3 Include 注册/撤回（新文件 `internal/ssh/config_register.go`）

- 常量：`includeLine = "Include ~/.ssh/senv/groups/*.conf"`，`configPath = ~/.ssh/config`。
- `RegisterInclude() (changed bool, err error)`：读 `~/.ssh/config`（不存在则创建 0600 并写入一行）；逐行**精确匹配** `includeLine`，命中返回 `false`；否则顶部插入；写入走「拷 `.senv-bak` → temp 文件同权限 → rename 原子替换」。
- `UnregisterInclude() (changed bool, err error)`：逐行精确匹配删除；其余内容一字不动。
- 精确匹配语义：senv 拥有行 = 内容完全等于约定行。用户手写的等价行（空格差异）会导致两行并存——OpenSSH 重复 Include 同一 glob 首匹配生效，无害，不主动清理。
- 全量 `Unexport()` = `UnregisterInclude` + 删除 `groups/` 目录（纯 senv 产物）；`keys/` 与 vault 不动。

### D4 组片段写入与幽灵清理

- 写入：`EnsurePrivateDir(groups/, 0700)` + `WriteSensitiveFile(0600)`，整文件重渲染不回读（ADR-0007 单向模型）。
- 幽灵清理：**仅全量 Apply**。列出 `groups/*.conf` 与渲染结果键集合作差集删除；`--group`/`--host` 单组重建绝不触碰其他文件（spec 已同步收窄）。
- 单组重建仍整文件重写该组——「写入单元 = 组」恒定。

### D5 自动落盘编排

Apply 内对每个被引用 keypair：派生目标路径 → `Lstat`：不存在 → 复用 `Materialize` 的写盘原语（0700/0600）；存在 → 记 skipped；keypair 不在本机 vault → 渲染阶段已记 warning，此处跳过。导出**不提供**覆盖已存在文件的开关（「存在跳过」恒定，`--force` 只属于显式 `keypair materialize`）。

### D6 prune

`Prune(force bool)`：遍历 `keys/<组>/` 全部文件 → 派生 `(group, name)` → 与「vault 中每个 host 的 `identityKey` 所指 keypair 的当前 `(group, name)`」集合比对，不在集合内即未引用（覆盖 keypair 已改组的旧路径文件）。列清单（路径 + 对应 keypair 是否仍在 vault）→ TTY 确认或 `--force` → 删除；未确认不删任何文件。非 TTY 且无 `--force` → 报错要求显式确认。

### D7 分组名校验

`validateGroup`（manager.go:91）增加 `/` 拒绝；host `validateAndSave` 路径挂上该校验（keypair 的 `UpdateKeyPair`/`ImportKeyPairWithGroup` 已调 `validateGroup`，一处改动三处生效）。`_ungrouped` 不预留——真实组撞名在导出时检测报错（渲染阶段比对，fail fast）。

### D8 CLI 形态与使用示例

```
senv host export                     # 全量应用：组片段 + 落盘 + 注册
senv host export --group prod        # 只重建 prod 组片段
senv host export --host web          # 重建 web 所在组整文件
senv host export --output -          # 纯渲染到 stdout（无副作用）
senv host export --output /tmp/x.conf # 纯渲染到指定文件（无副作用）
senv host unexport                   # 摘注册行 + 删组片段
senv keypair prune [--force]         # 列未引用私钥，确认后清理
```

应用导出摘要示例：`✓ groups: prod.conf, staging.conf（重建）/ skipped keys: web-key / materialized: deploy-key / include: 已注册 / warnings: host x 引用的 keypair y 不在本机 vault`。`export`/`unexport`/`prune` 均写操作审计（条目名清单）。

### 数据流图

```
cmd/ssh.go export ─▶ Manager.Apply(filter)
                        ├─ Render(filter) ──vault──▶ hosts + 被引用 keypairs(解密取 group)
                        │     ├─ proxyJump 校验(悬空即失败,零副作用)
                        │     ├─ 跨组闭包并入发起组
                        │     └─ 返回 map[组]片段 / warnings / keypairs
                        ├─ 写 groups/*.conf(0700/0600,全量时清幽灵)
                        ├─ 落盘缺失 keys/<组>/<名>(存在跳过)
                        ├─ RegisterInclude(.senv-bak 事务,幂等)
                        └─ 摘要 + 审计

TUI 预览 / MCP 只读 ─▶ Manager.Render   （纯,无副作用）
cmd --output ─▶ Manager.Render + 单文件写出
```

## 错误处理策略

- **渲染阶段失败**（proxyJump 悬空、vault 读取/解密失败、组名撞 `_ungrouped`）：整体失败，不产生任何文件副作用（两阶段架构保证）。
- **写盘阶段局部失败**（某组片段、某密钥落盘、注册失败）：尽力而为 + 汇总——逐项收集错误，已完成项保留，最终输出部分成功摘要 + 错误明细，退出码非零。
- **`~/.ssh/config` 写入**：先 `.senv-bak` 备份再原子 rename；备份失败不阻断但警告。
- **非 TTY 的 prune**：无 `--force` 直接报错，不猜。
- **OpenSSH < 7.3**：glob Include 不生效。尽力检测（`ssh -V` 解析版本；命令缺失则跳过），命中则导出输出警告；文档注明。

## 向后兼容

- vault 数据零迁移（`group` 字段既有，空 = 未分组）。
- 老扁平落盘文件 `~/.ssh/senv/<名>` 不迁移不删除，落入「未引用 warning」由用户 prune。
- 老手写 `Include ~/.ssh/config.d/senv` 不代删；顶部注册行使新片段首匹配生效，旧块不复活（README 提示移除）。
- **BREAKING**：`export` 默认 stdout → 应用模式；`--output <file>` 保持纯渲染语义不变。

## Risks / Trade-offs

- 组改名 → 旧片段被全量导出清理，但旧路径私钥变「未引用」只警告 → prune 收尾，两步显式。
- 并发编辑 `~/.ssh/config`（极小概率）→ `.senv-bak` 可人工恢复；单用户 CLI 场景接受。
- 导出解密 keypair 取 group → 私钥明文短暂驻留进程内存，与 `materialize` 同级，Go 内存不落盘。
- 跨组闭包在多个 fragment 产生同别名块 → OpenSSH 首匹配 + 内容同源（vault），无害；单组导出后若 jump 目标配置已变且其组未重建，重复块可能短暂陈旧——下一次全量导出货真价实。

## Migration Plan

无数据迁移。发布说明标注 BREAKING（`export` 默认行为）；README「SSH 资产管理」一节的导出演示与 `config.d` 教学整段改写；ADR-0023 status 在实现合入时改 accepted；SKILL.md 同步。

## Open Questions

无。grilling 三轮已闭合全部设计决策（见 ADR-0023 决策 1–8）。
