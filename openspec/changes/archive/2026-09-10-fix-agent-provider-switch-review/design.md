## Context

当前 switch 使用固定 `.senv-bak` 和逐文件写入；恢复时会先覆盖备份。TOML 通过行前缀编辑，Pi 需要连续写两份文件，MCP/TUI/CLI 可并发进入 switch。`llm_providers` 已有读写 API，但 rekey、orphan、consistency 生命周期仍只识别旧集合。provider add 还支持 `--api-key`，并允许宽松 HTTP URL。

## Goals / Non-Goals

**Goals:**

- 让一次 agent switch 成为可恢复、可串行化的配置事务。
- 让 provider 密文参与 vault 全生命周期，并避免自有凭据孤儿。
- 消除 CLI argv 明文凭据，集中 URL 和身份校验。

**Non-Goals:**

- 不实现跨进程文件锁或凭据代理。
- 不同步指针，不改变 models.dev 缓存格式。
- 不主动改写用户已有 agent 配置的无关业务字段。

## Decisions

### 1. 配置事务与备份

引入 switch 事务对象，按绝对路径持锁并保存初始快照：

```
resolve/validate
      │
      ▼
lock(agent config paths) ──► snapshot originals
      │
      ▼
apply file 1 ─► temp+fsync+rename ◄─┐
apply file 2 ─► failure ────────────┘
      │ success all
      ▼
save pointer
      │ success                 │ pointer failure
      ▼                         ▼
remove transaction backups  restore every touched path
```

事务内每个 touched path 保存独立临时备份，不再复用可能已被覆盖的 `.senv-bak`。恢复时读取快照后写新 temp、fsync、rename，最后 fsync 目录；只有恢复成功才删除备份。Pi 的 `models.json` 和 `settings.json` 进入同一事务；成功提交指针后统一清理备份。

进程内锁使用按规范化绝对路径索引的 mutex map。跨进程并发暂不承诺；文档说明 MCP server 与多个 CLI 进程同机并发仍需用户避免。

备选方案是文件锁。它能覆盖多进程，但锁定语义、NFS 行为和 stale lock 处理会显著扩大本次修复。

### 2. TOML 使用语法感知解析器

新增 `github.com/pelletier/go-toml/v2`。Codex/Kimi 更新流程改为 decode → 修改本功能拥有的 key → encode → temp/rename。这样多行数组、多行字符串和 array of tables 不会让插入点落入 literal，也可消除 Codex 二次切换的重复 `model_provider`。本功能只设置已知 path，不触碰无法表示的扩展类型。

权衡是 TOML 重编码可能规范化空白和注释；验收以既有 key/value 语义不变为准。相比继续维护行级 editor，这是更小的正确性风险。若实现阶段发现注释保留是硬需求，应另开 change 评估 TOML CST 库。

### 3. Provider 凭据更新采用补偿动作

vault mutation 仍不是跨集合事务，因此在同一锁内定义顺序和补偿：

- 新增/force：先读旧自有凭据；写新凭据；成功保存档案后提交。档案保存失败则恢复旧凭据或删除新建凭据。
- owned → external：成功保存新档案后删除旧自有凭据；删除失败则恢复旧档案并返回错误。
- remove：读取自有凭据值 → 删除凭据 → 删除档案；档案删除失败则用旧值恢复凭据；凭据本不存在时直接删除档案。

补偿失败返回复合错误，不静默吞掉 primary cause。该策略不要求把 text 集合与 provider 集合改造成原子存储格式。

### 4. Vault lifecycle 分类与兼容

`llm_providers` 增加 rekey entry kind、consistency report 分组和 `HasOrphanedData` 分支。manifest identity 继续使用 `llm_providers/<alias>/<file>`，不改变既有密文、metadata 或旧 manifest 条目格式。旧版本不可能产生 provider rekey manifest，所以新增 kind 向后兼容。加载 provider 后调用同一 validator，作为 save 之外的第二道边界。

### 5. CLI 输入

移除 `--api-key`。交互 TTY 用 `golang.org/x/term` 不回显读取；非交互用 `--api-key-stdin`。Cobra 只接收布尔 flag，secret 不进入 flag binding；读取值仅在 provider manager mutation 内使用。`--base-url` 与档案加载共用 validator，默认 HTTPS，`--allow-http` 显式放行 HTTP，并拒绝 userinfo。

使用示例：

```bash
# interactive
senv ai provider add acme \
  --base-url https://api.acme.com/v1 \
  --catalog-provider acme --default-model m1

# script
printf '%s' "$ACME_KEY" | senv ai provider add acme \
  --base-url https://api.acme.com/v1 \
  --api-key-stdin \
  --catalog-provider acme --default-model m1

# local model server
senv ai provider add local \
  --base-url http://127.0.0.1:11434/v1 --allow-http \
  --key-ref env:llm/LOCAL_KEY --model qwen3
```

## Error Handling

| 阶段 | 策略 |
| --- | --- |
| URL/身份/模型校验 | 写档案、凭据、审计成功记录前失败 |
| 单文件写失败 | 不提交指针，回滚本事务已写文件 |
| 多文件写失败 | 按快照逆序恢复全部已写文件 |
| 指针失败 | 回滚全部配置；指针保持旧值或缺失 |
| 恢复失败 | 保留独立快照，复合错误包含 primary 与 restore cause |
| 凭据补偿失败 | 保留可回退状态并返回复合错误 |

## Risks / Trade-offs

- [TOML 重编码改变注释/格式] → 规格要求语义不变；测试断言无关值、表结构和默认选择仍正确。
- [进程内锁不阻止两个 OS 进程] → 文档明示限制；事务和独立快照仍降低单进程 TUI/MCP 竞争风险。
- [移除 `--api-key` 破坏脚本] → 提供 `--api-key-stdin` 和 release note migration；错误提示直接指向新输入方式。
- [收紧既有 agent 目录权限可能影响共享目录用法] → 目标目录含凭据明文，0700 是安全默认；用户需手动选择更宽布局时不应把凭据配置放该目录。
