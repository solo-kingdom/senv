## Why

senv 即将同时支持 git 与 server 两种远端模式（见 driver `senv-server-provider-driver`）。当前 `cmd/*` 有 7+ 处直接调用 `storage.NewManager`，git 同步逻辑与 CLI 耦合，没有扩展点。需要先落地一个行为不变的抽象层，作为后续 server provider 的对接基础。

## What Changes

- 新增围绕 push/pull 语义的窄 Provider 接口（同步通道抽象，不抽象 storage 的全部读写方法）
- 现有 git 流程收敛为该接口的一个实现，对外行为完全不变
- cmd 层新增统一构造入口，按配置选择 provider（默认 git），消除散落的直接构造调用
- 纯重构：不新增用户可见功能，不改变任何现有命令行为

## Capabilities

### New Capabilities
- `provider-abstraction`: provider 的选择、配置与统一构造入口的行为约定

### Modified Capabilities
（无——git 同步行为不变，不动 `git-sync` 等现有 spec）

## Impact

- `internal/`：新增 provider 包（接口 + git 实现 + 构造入口）
- `cmd/`：auth/doctor/git/init/session/interactive_main 等构造点收敛到统一入口
- 依赖：无新增外部依赖
