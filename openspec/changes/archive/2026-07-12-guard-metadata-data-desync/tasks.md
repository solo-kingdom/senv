## 1. 错误分类体系（D1，高优先级：安全/正确性）

- [x] 1.1 在 `internal/session` 定义哨兵错误 `ErrSessionExpired`、`ErrSessionStaleMetadata`、`ErrSessionStaleKey`、`ErrNoSession`，并让 `GetCachedKey` 按失效原因分别返回它们（替换现有笼统的 `session expired or invalid` / `session stale`）。验证：`go build ./internal/session` 通过。
- [x] 1.2 在 `internal/storage` 定义 `ErrDataDesync` 哨兵错误，供 cmd 层判断"数据层不同步"。验证：`go vet ./...` 通过。
- [x] 1.3 编写 `internal/session/manager_test.go` 用例：分别构造 expired / salt-mismatch / key-invalid / no-cache 四种状态，断言 `errors.Is` 命中对应哨兵。验证：`go test ./internal/session -run ErrorClass -race`。

## 2. 非破坏性 stale 处理与诊断探针入口（D2，高优先级：安全）

- [x] 2.1 移除 `GetCachedKey` 中所有 stale 分支的 `_ = clearCache()` 调用；仅保留"过期且 cache 已无法用"的明确分支可清。在函数注释中写明"非破坏性"约定。验证：`go test ./internal/session -run StaleNoClear`（新增）通过。
- [x] 2.2 新增 `session.Manager.PeekCachedKey() ([]byte, *SessionCache, error)`：读取原始 cached key 与 cache，不做校验、不清缓存，专供诊断。验证：单测断言 stale cache 存在时 `PeekCachedKey` 仍能返回旧 key。
- [x] 2.3 单测：stale cache 在任意命令路径后仍可被 `LoadCache` 读到（未被清掉）。验证：`go test ./internal/session -race`。

## 3. 一致性探针 API（D3，高优先级：不得泄露明文）

- [x] 3.1 在 `internal/storage` 实现 `ConsistencyReport`/`FileProbes` 类型与 `Manager.CheckConsistency(key)`：探查 metadata.PasswordKey、各 `env_*.json.enc`、`texts/**`、config 文件可解性。验证：返回值结构仅含 bool/int/[]string。
- [x] 3.2 实现非 `KeySize` 长度 key 的安全处理（视为全部 failed，不 panic）。验证：单测传入 0 字节与 31 字节 key 不报错。
- [x] 3.3 单测：一致项目返回全 OK；构造单个 desync 文件后 failed 列表精准命中该文件；断言报告无任何明文字段。验证：`go test ./internal/storage -run CheckConsistency -race`。

## 4. cmd 层诊断回退（D2 落地到入口）

- [x] 4.1 抽取公共鉴权助手（统一 `cmd/{env,tui,config,text,interactive_main}.go` 的 `GetCachedKey → prompt → VerifyPassword` 模式），在 stale 错误时调用 `PeekCachedKey` + `CheckConsistency`：若旧 key 能解数据但不能解 metadata → 返回 `ErrDataDesync` 与指引，而非提示密码。验证：手写脚本复现 desync 时报真实原因。
- [x] 4.2 实现"password 校验成功后清理已知无用 stale cache"分支（仅当 `VerifyPassword` 通过时清）。验证：单测/集成测断言该路径后 cache 被清。
- [x] 4.3 集成测试：复用探索阶段的 desync 复现脚本逻辑转为 `cmd` 层测试，断言 (a) desync 时报 `ErrDataDesync`、(b) 仅密码错时报 `invalid password`、(c) stale cache 未被误清。验证：`go test ./cmd -run Desync -race`。

## 5. `senv doctor` 命令（D5）

- [x] 5.1 新增 `cmd/doctor.go`：复用 session key，无 session 时 `promptPassword` 临时认证（不写 session）；调用 `CheckConsistency` 输出人类可读报告（metadata↔env/text/config 计数、脱节文件清单、恢复建议）。验证：`senv doctor` 在一致项目输出全 OK。
- [x] 5.2 单测：注入伪造 key 与临时项目，断言报告格式与脱节文件命中；断言无明文输出。验证：`go test ./cmd -run Doctor -race`。

## 6. init 防呆（D4）

- [x] 6.1 在 `storage.Initialize` 前置检查：data 目录存在 `env_*.json.enc` 或 `texts/` 下文件但 config 无 `metadata.json` 时，返回明确错误并拒绝。验证：单测构造该状态断言拒绝且不创建 metadata。
- [x] 6.2 单测：空目录正常 init；已初始化项目仍报原 `already initialized`；防呆与既有检查互不干扰。验证：`go test ./internal/storage -run InitGuard -race`。

## 7. git pull 自检（D6）

- [x] 7.1 在 `senv git pull` 成功后：若有 session key 则调用 `CheckConsistency`，不一致时打印警告并提示 `senv doctor`；无 session 则跳过。验证：构造 pull 后 desync（改写 metadata），断言警告输出且不删改文件。
- [x] 7.2 单测：无 session 时不强制提示密码、pull 正常完成。验证：`go test ./cmd -run GitPullSelfCheck -race`。

## 8. 收尾与质量门

- [x] 8.1 更新 `cmd/root.go`/帮助文本与 README：补充 `senv doctor` 用法与 desync 排错指引。验证：`senv doctor --help` 输出正确。
- [x] 8.2 全量检查：`make check`（fmt + vet + lint + test with -race）全绿。验证：CI 本地等价命令通过。
- [x] 8.3 清理探索阶段产生的临时复现脚本/worktree 残留（`/tmp/opencode/*` 已清，确认仓库无新增临时文件）。验证：`git status` 干净。
