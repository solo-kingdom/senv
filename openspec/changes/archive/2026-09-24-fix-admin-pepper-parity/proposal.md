## Why

pepper 只在 `runServe` 里读 `SENV_SERVER_TOKEN_PEPPER`（`senv-server/main.go`），而 admin 全部子命令走 `withStore` 建 store，从未调用 `SetTokenPepper`。启用 pepper 的部署里：

- `admin revoke-token <明文>` → 按裸 SHA-256 匹配 `token_hash`，对 HMAC 存储的新 token 命中 0 行 → 报「token 不存在」，**吊销通道静默失效**（而吊销恰恰是 pepper 之后最需要的操作）
- `admin create-user` → 签出的 token 以裸 SHA-256 入库，绕过 pepper；serve 侧靠回退比对照样能认证，于是「已加固」的库实际混着两种哈希且无人察觉

`RevokeToken` 自身已处理「带 pepper 时兼容吊销存量裸哈希」的方向（`store.go`），缺的只是环境变量到 admin 进程这一段接线。滚更部署新增的 `ops` 容器（承载 admin CLI 与 prune）正是这条路径的执行者。

## What Changes

- `withStore` 建 store 后调用 `applyTokenPepper`，与 `runServe` 共用同一读取逻辑（env `SENV_SERVER_TOKEN_PEPPER`）
- 抽出 `applyTokenPepper` + 最小接口 `pepperSetter`（`*store.pgStore` 未导出），serve 侧原内联判断改为调用它，行为不变
- 部署文档与 `ops` 容器注明：admin 侧必须注入同一 pepper

## Non-goals

- 不改哈希算法与回退语义（`store` 层零改动）
- 不给 admin 加独立 pepper flag（env 与 serve 同名，避免两套配置漂移）

## 涉及面

| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | senv-server：`main.go` 的 `withStore` / `runServe` 接线 |

## 验收标准

- [x] 配置 pepper 时 admin 路径的 store 收到同一 pepper；未配置时不调用 `SetTokenPepper`（保持原 SHA-256 行为）
- [x] `runServe` 行为逐字节不变（同一 helper、同一 env 名）
- [x] `go test ./senv-server/...` 通过；`docs/senv-server.md` pepper 节说明 admin 侧同源要求
