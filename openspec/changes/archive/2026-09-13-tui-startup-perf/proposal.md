## Why

`senv tui` 启动后首屏数据长时间空白。代码承诺「local data renders first」，但实际首屏必须等待对加密 vault 的全量解密，且启动瞬间 8 个 tab 的全量加载与后台 sync pull 在 `.senv-vault.lock` 排它锁上串行（pull 还持锁跨网络，最长 2s），进一步放大等待。实测定位方法：`SENV_PERF_THRESHOLD=1 senv tui` 后看 `~/.log/senv/perf.log`。

## What Changes

- **A. 解除锁竞争**
  - sync pull 的 API 网络请求移出 vault mutation 锁外：先在锁外完成网络请求，再持锁应用变更落盘。
  - text 域读取从「逐条目 flock + 逐条目解密」改为单趟快照式读取（对齐 env 的 `Snapshot()` 待遇），消除 N 次排它锁。
- **B. 真懒加载**：TUI Init 只加载当前聚焦 tab 与共享数据（全局搜索/快照），其余 tab 首次聚焦时加载，替代当前「Init 全 tab 并发加载」。使 `model.go` 注释描述与实际行为一致。
- **C. 磁盘快照缓存（首屏即展示）**：退出/写入时把解密后的明文列表快照加密落盘；启动时先解密快照立即渲染，后台完成真实解密后校验并替换。快照用 vault 主密钥加密（AES-256-GCM，0600/0700 权限），不引入新的明文落盘。
- **非目标（Non-goals）**：不改变 sync 的 throttle 窗口与 `--refresh` 语义；不改动 push 路径；不改 UI 布局与键位；不引入新的外部依赖；不处理无 session 密码路径的 PBKDF2 逐次派生（独立问题，见 design 边界）。

## Capabilities

### New Capabilities
- `tui-startup`: TUI 启动性能契约——首屏数据时延、懒加载时机、快照缓存的读写与安全要求。

### Modified Capabilities
（无：锁内网络请求与逐条目读取属实现细节，未改变 server-sync/text-storage 等 spec 的外部行为契约。）

## Impact

- `internal/provider/server.go`（pull 锁范围）、`internal/storage/mutation.go`（锁语义不变，调用方调整）
- `internal/text/manager.go`（单趟快照读取）
- `internal/tui/model.go`（Init 加载策略、懒加载触发点）
- 新增快照缓存读写模块（`internal/tui` 或 `internal/storage` 子包，design 定）
- 测试：`internal/tui/*_test.go`、`internal/text`、`internal/provider` 相关测试
- 安全性分析：C 引入加密快照文件，需评估密钥来源、文件权限、失效与回退，见 design.md「安全性分析」。
