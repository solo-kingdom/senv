## Why
探索新增 senv-server 的可能性：现在是基于 git + 本地仓库的模式，新增 senv server provider，数据保存在数据库内

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 本阶段只做探索与方案收敛，不落地实现代码

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] 明确 senv server provider 的目标形态（与现有 git + 本地仓库模式的关系：替代 / 并存）→ **并存**：server 与 git 双 provider 共存
- [x] 明确数据模型与数据库选型（schema、迁移策略、嵌入库 vs 外部服务）→ 已定 **Postgres 多用户**；schema 已落地（users/tokens/vaults/vault_metadata/entries + schema_migrations，内置顺序迁移）
- [x] 明确 server 的接口形态（HTTP API / gRPC / CLI 对接方式）与鉴权方案 → 双层身份已定（API token + vault 口令分离，metadata 密文托管）；具体 API 形态待 propose 设计
- [x] 明确与现有 provider 抽象的对接点（代码改动面清单）→ 方向已定：窄 Provider 接口 + 统一构造入口 + local 缓存复用现有格式；清单在 propose 阶段细化
- [x] 探索结论与决策写入 driver proposal 验证记录，作为后续 propose 阶段的输入

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `senv-server-provider-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/senv-server-provider-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录

### 2026-09-01 探索：现状架构
- `cmd/*` 直接调用 `storage.NewManager(configPath, dataPath)`，调用点分散（auth/doctor/git/init/session/interactive_main），无 provider 抽象层
- `internal/storage.Manager`：文件系统加密存储，AES-256-GCM + PBKDF2，每个 entry 一个加密文件（env per-var 文件 + group meta、text、config + index）
- `internal/git.Manager`：把 dataPath 所在目录当 git 仓，`Sync` = commit + pull + push
- 关键洞察：加密是端到端的，server 只见密文 blob → senv-server 本质是零知识密文托管，git history ≈ revision 表

### 2026-09-01 探索：用户决策（五条线索已收敛）
1. **定位**：server + git **并存**，双 provider 共存 → 需要引入 provider 抽象层
2. **数据库**：**Postgres，支持多用户** → 有租户/账号体系
3. **并发**：**乐观锁**（revision 版本号冲突检测）
4. **离线**：**本地缓存兜底**（断网可读写，恢复后同步）
5. **迁移**：提供 git ↔ server 的 **import/export 工具**

### 2026-09-01 探索：决策引出的开放问题（propose 阶段需收敛）
- ~~A. 双层身份~~ ✅ **已定：方案 1，托管**。salt/passwordKey 以密文形式托管到 server；server 账号（API token）与 vault 加密口令双层分离。新机器凭 server 账号 + vault 口令即可完整恢复。代价：DB 泄漏后可离线爆破 vault 口令，以 PBKDF2 高迭代数缓解
- ~~B. 乐观锁粒度~~ ✅ **采用默认：per-entry revision**（env var / text / config 各自带版本号），propose 阶段在协议设计中细化
- ~~C. 离线缓存形态~~ ✅ **采用默认：复用现有文件存储格式**，server 成为"另一个 remote"，与 git 模式对称；provider 接口围绕 push/pull 语义设计
- ~~D. 改造面~~ ✅ **方向已定**：窄 Provider 接口（push/pull 语义），现有 storage.Manager 收敛为 local provider；cmd 层 7+ 处构造调用统一入口；session cache（bootid）语义对齐——具体清单在 propose 阶段产出
- ~~E. import/export 形态~~ ✅ **采用默认：CLI 侧双向迁移子命令**（server 不碰明文，做不了转换）

### 2026-09-02 propose：拆分与契约已备齐
- driver artifacts：proposal / design / tasks 全部 done，`validate --strict` 通过
- 子 change（artifacts 已一次性备齐，全部 `validate --strict` 通过）：
  - `senv-server-provider-interface`（8 任务）：窄 Provider 接口 + git 适配 + cmd 统一构造入口，纯重构
  - `senv-server-provider-server`（9 任务）：senv-server 二进制，Postgres schema + token 鉴权 + 乐观锁 API
  - `senv-server-provider-client`（9 任务）：CLI server provider，本地缓存 + 同步引擎 + 离线兜底
  - `senv-server-provider-migrate`（6 任务）：git ↔ server 双向迁移
- 依赖链：interface → server / client → migrate

### 2026-09-02 实施：全仓回归（task 3.1）
- 命令：`make check`（gofmt + go vet + golangci-lint + go test -race ./...）
- 结果：全绿（"All checks passed!"）
- 覆盖：interface 纯重构无断言变更；server 经 testcontainers 起临时 Postgres 跑 store/handler 单测与冲突、隔离集成测试；client 同步引擎单测（增量/冲突/离线恢复/accept-remote/force-push）+ 与真实 server 的端到端联调；migrate 双向迁移集成测试（中断重试、非空目标保护、口令不变）
- 依赖变化：新增 pgx/v5（仅 server 二进制）、testcontainers-go（仅测试）；go directive 保持 1.24.9

### 2026-09-02 实施：子 change 完成记录
- `senv-server-provider-interface` 8/8：`internal/provider` 窄接口（Push/Pull/Sync/Status）+ git 薄适配层 + `provider.New` 统一构造入口与配置校验；settings.json 新增 `provider` 字段（机器本地）；cmd 构造点收敛（getStorage/getSyncProvider/getGitProvider），行为不变
- `senv-server-provider-server` 9/9：`senv-server` 独立二进制（serve/migrate/admin），标准库 ServeMux 模式路由 + pgx 连接池；schema 内置顺序迁移与启动版本校验；token 只存 SHA-256 哈希；per-entry revision（vaults.seq 行锁单调序列）乐观锁批量推送（整批事务、409 冲突清单、单批 1000/单条 512KB 上限）；增量拉取含删除标记；跨用户访问一律 404
- `senv-server-provider-client` 9/9：HTTP client（零 pgx 依赖，wire 类型独立）；同步状态文件 `.senv-sync-state.json`（last_synced_revision + 内容哈希快照，dirty 判定取设计允许的"简单可靠者"：哈希快照优于显式打标，写路径零侵入）；Pull 保护本地 dirty 条目不被覆盖；`senv init --server` 接入已有 vault（口令不发 server）；`senv sync` 统一命令（git/server 分支）+ `--accept-remote`/`--force-push` 冲突出口；session 缓存机制零改动复用
- `senv-server-provider-migrate` 6/6：`senv migrate to-server`/`from-server`（既有 migrate 命令下新增子命令，原格式迁移行为不变）；全程零明文、不需口令；非空目标冲突分析先于任何写入；幂等重试（一致条目跳过）；force 显式覆盖并保留/计数目标额外条目
