## Context

Server 同步目前先增量 pull，再对本地 dirty 条目做乐观锁 push。pull 会跳过本地 dirty 条目的远端版本并推进 `last_synced_revision`；随后 push 收到 409 时，客户端只有条目标识与远端 revision。因此冲突后无法直接还原“刚才被跳过的远端候选内容”，用户也缺乏 size、时间与内容差异来判断该选择哪一侧。

现有 `text` / `config` 编辑链路已经具备解密到临时文件、调用外部 editor、读回并重新加密的模式；TUI 也已有通过 `tea.ExecProcess` 挂起界面运行 editor 的先例。本设计复用这些边界，但不把合并结果直接当作普通编辑提交。

## Goals / Non-Goals

**Goals:**

- 在不自动覆盖数据的前提下，为每个冲突提供本地 / 远端非机密摘要。
- 在 TTY 中提供可键盘导航的冲突解决器，并支持显式明文查看。
- 为可文本化条目提供可选 editor 手动合并，并以远端 current revision 为推送 base。
- 保持旧 server、非 TTY 与现有 `--accept-remote` / `--force-push` 行为可用。
- 保证 server 仍不理解明文，409 不放大为密文下载。

**Non-Goals:**

- 不保存共同祖先版本，不提供自动 three-way merge。
- 不实现跨条目自动语义合并；`config_index` 只做可校验的手动 JSON 合并。
- 不让自动 push 进入交互 UI。
- 不支持二进制 config、删除冲突、`env_meta` 与 vault metadata 的 editor 合并。

## Decisions

### 1. 冲突描述符由 server 提供，409 不携带密文

`Entry` wire 结构增加可选 `updated_at`；store 从既有 `entries.updated_at` 读出，无需数据库 schema migration。409 的每个 `Conflict` 增加：

- `deleted`
- `size`（当前密文字节数，删除条目为 0）
- `updated_at`

不把 ciphertext 放入 409。冲突内容获取沿用受 token 保护的 pull 通道，避免一次冲突响应携带最多 1000 × 512KB 的密文。

**替代方案：** 让 409 直接返回完整远端 entry。该方案实现少一次请求，但响应体积和 DoS 面不可控，因此拒绝。

### 2. 保留 pull 阶段的 skipped dirty 远端候选

`pullLocked` 已经拿到过因本地 dirty 而跳过的远端 entry。将这些 entry 作为远端候选随 pull 结果返回，不落盘、不改变同步状态。push 冲突时按条目 ID 关联：

- 本地候选：本次待推送 entry 或删除标记；
- 远端候选：pull skipped entry；若 push 409 显示远端 revision 又变化，则用全量 `Pull(0)` 刷新一次候选。

这样能覆盖常见冲突且无需新增单条读取 API。全量刷新仅在描述符 revision 与候选 revision 不一致时发生。

**替代方案：** 新增按 identity/revision 的单条读取 API。该方案更适合大 vault，但会扩大 v1 API 面；后续性能不足时再演进。

### 3. 冲突摘要与内容渲染分层

Provider 层只处理 identity、revision、hash、size、ciphertext 与 metadata 兼容性；CLI/TUI 层负责类型化渲染：

- `env`：解密内部 entry，显示 value、plaintext size、业务更新时间，默认掩码。
- `text`：显示文本 diff / side-by-side 与业务更新时间。
- `config`：UTF-8 文本显示 diff；二进制只显示 size/hash/hex preview。
- `config_index`：默认显示按 config 名称的语义摘要（added/deleted/target/group/description）。
- `env_meta`：只显示名称与创建时间摘要。
- `vault metadata`：只显示版本、KDF 迭代次数、更新时间与 key 兼容性诊断。

本地文件 mtime 不作为业务更新时间；仅在条目内部没有时间字段时显示“不可用”，避免把 pull 落盘时间误当成用户修改时间。

### 4. 明文认证按需发生

`senv sync` 本身继续只需要 server token。冲突解决器启动时先显示脱敏摘要；用户请求明文或合并时：

1. 优先复用有效 session key / 进程内 auth memo；
2. 没有 key 且 stdin/stdout 均为 TTY 时，提示输入 vault 口令；
3. 解密失败则保持脱敏视图，不猜测、不覆盖。

若远端 metadata 不能用当前 key 解锁，标记 key incompatible。此时禁用 editor 合并；用户只能使用既有整体 `accept-remote` / `force-push` 语义，避免把用不同 key 加密的条目和 metadata 混合成不可解锁状态。

### 5. Editor 手动合并是两方合并

按下 `m` 后创建一次性私有目录和 0600 合并文件。缓冲区使用明确的 marker：

```text
<<<<<<< SENV_LOCAL
...
=======
...
>>>>>>> SENV_REMOTE
```

`env` 的编辑对象是最终 value，而不是内部 JSON；`config_index` 的编辑对象是 pretty JSON。读回后执行：

1. UTF-8 与大小校验；
2. unresolved marker 检查；
3. 条目类型 schema 校验（`config_index` 必须 parse 并 normalize）；
4. 明文等价比较与结果摘要；
5. 用户确认前仅保留在内存或私有临时目录。

Editor 使用 `VISUAL`、`EDITOR`，再沿用现有 fallback。CLI 直接运行 editor；若冲突解决器基于 Bubbletea，则使用 `tea.ExecProcess` 挂起界面，复用现有 TUI editor 模式。

**替代方案：** 构造 local/remote/base 三个文件交给 `vimdiff`。这依赖特定 editor 且仍没有可靠 base，第一版不采用。

### 6. Resolution plan 在 provider 内原子预检与应用

新增 provider 级 resolution plan，而不是把 UI 决策直接写文件后调用 `ForcePush`。计划包含每个冲突的决策：

- `local`
- `remote`
- `merged(ciphertext)`

以及 metadata 的整体决策。应用时在同一同步锁内：

1. 重新收集本地候选，校验其 hash 仍等于 UI 展示的版本；
2. 获取远端描述符 / 候选，校验 revision 仍等于 UI 展示的版本；
3. 校验所有 merged plaintext / ciphertext 与 metadata 决策完整；
4. 先通过本地 cache transaction 原子写入选定 / 合并条目；
5. 以远端冲突 revision 为 `base_revision` 批量推送 local/merged 决策；
6. server 成功后再保存新 sync state。

如果本地写入成功但网络失败，保留本地合并后的待推送状态并提示重试；这是既有“本地工作副本优先”的恢复模型。如果 push 再次 409，不覆盖新远端版本，重新进入冲突流程。

**替代方案：** 先 push 再写本地。远端成功而本地写失败会造成下一轮冲突且丢失用户编辑上下文，因此拒绝。

### 7. CLI / TTY 行为

```bash
# TTY：冲突时进入交互解决器
senv sync

# 脚本：显式禁用交互，输出增强后的文本报告
senv sync --no-interactive

# 保持既有非交互策略
senv sync --accept-remote
senv sync --force-push
```

交互器建议使用既有 Bubbletea 依赖实现 alt-screen UI：

- `j/k` 或方向键选择；
- `Enter` 查看详情；
- `v` 显式揭示 / 重新掩码；
- `m` editor 手动合并；
- `l/r` 当前条目使用本地 / 远端；
- `L/R` 未逐条处理项全部使用本地 / 远端；
- `q` 不修改退出；
- `?` 显示帮助；
- 应用前强制确认覆盖计划。

自动 push 失败仍只输出一行 warning 并引导 `senv sync`，避免写命令在退出阶段突然接管终端。

## Data Flow

```text
senv sync
  │
  ├─ pull remote delta
  │    ├─ clean local entry → apply remote
  │    └─ dirty local entry → keep local, retain remote candidate
  │
  ├─ collect dirty local candidates → optimistic push
  │
  ├─ 409 conflict descriptors
  │    ├─ non-TTY / --no-interactive
  │    │    └─ enhanced text report + existing flags guidance
  │    └─ TTY resolver
  │         ├─ pair local candidate + remote candidate
  │         ├─ show identity/revision/size/hash/time/deleted
  │         ├─ optional authenticated plaintext compare
  │         ├─ optional editor two-way merge
  │         └─ build resolution plan + confirmation
  │
  └─ provider resolve
       ├─ revalidate local hash + remote revision
       ├─ atomic local cache transaction
       ├─ batch push local/merged using remote base revision
       └─ save sync state only after successful push
```

## Error Handling Strategy

- **Editor 非零退出 / 文件缺失：** 不应用结果，清理私有目录，返回冲突列表。
- **残留冲突标记或 schema 无效：** 拒绝该条 merge，保留原始冲突，指出具体校验原因。
- **无法获得 vault key：** 禁止明文查看和合并，保留安全摘要与本地 / 远端选择。
- **远端 metadata key 不兼容：** 禁止逐条 merge，限制为既有整体策略并显示明确原因。
- **远端 revision 在编辑期间变化：** 丢弃过期 plan，刷新冲突并要求重新确认。
- **本地候选在编辑期间变化：** 丢弃过期 plan，提示重新运行，避免覆盖其他进程写入。
- **旧 server 缺少新增字段：** 显示 `N/A`，继续使用现有 revision 冲突语义和 pull 候选。
- **本地合并已落盘但 push 网络失败：** 保留待推送状态，提示下次 `senv sync` 重试；不得回滚用户已确认的合并。
- **临时目录清理失败：** 输出安全警告与路径，不假装清理成功。

## Storage / API Compatibility

- 本地加密文件格式、`.senv-sync-state.json` 结构和数据库 schema 均不变。
- 新 JSON 字段全部可选或向后兼容：旧 client 忽略新增响应字段，新 client 对旧 server 的零值显示为不可用。
- 409 仍是整批拒绝；新增字段只描述当前冲突，不改变 revision 或写入语义。
- `--accept-remote` 与 `--force-push` 保持现有含义，并提供给脚本和非交互用户。

## Risks / Trade-offs

- [409 描述符增加 handler/store 查询] → 在同一冲突查询中返回 `deleted`、ciphertext length 与 `updated_at`，避免额外按条目查询。
- [全量 Pull(0) 刷新候选在大 vault 上变慢] → 只在 pull 候选与 409 revision 不一致时使用；后续可增加按条目读取 API。
- [Editor 可能留下 swap / backup] → 使用一次性 0700 目录并在结束递归清理；帮助文案说明外部 editor 自动备份不在 senv 控制范围。
- [逐条选择可能产生索引 / 内容不一致] → 应用前做 existence 与类型校验；`config_index` merge 必须 normalize，缺失关联文件时给出警告。
- [Bubbletea UI 增加测试面] → 将状态更新与渲染拆为纯函数，用表驱动测试 keybinding、mask、plan 与校验。

## Migration Plan

1. 扩展 server wire 结构、SQL scan 与 409 handler；旧客户端无需变更。
2. 扩展 provider pull / conflict 结构，先保持 CLI 输出向后兼容。
3. 加入增强非交互报告与 `--no-interactive`。
4. 加入 TTY 摘要 / 明细视图与安全认证路径。
5. 加入 resolution plan 与本地 / 远端选择应用。
6. 加入 editor merge、校验与清理。
7. 更新用户文档并保留现有命令示例。

回滚时，新增响应字段可继续忽略；CLI 可回退为现有错误输出。若已产生合并后的本地待推送条目，回滚代码仍应按普通 dirty 工作副本处理，不自动删除用户确认过的内容。
