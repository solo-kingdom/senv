## Context

见 proposal.md 与 driver design.md D1。约束：纯重构，现有测试套件（make test）必须不改断言地通过。

## Goals / Non-Goals

**Goals:** 窄 Provider 接口落地；git 实现接入；cmd 构造点收敛。

**Non-Goals:** 不实现 server provider（后续子 change）；不改 storage.Manager 的方法集；不改配置文件的现有字段含义。

## Decisions

### 接口只覆盖同步语义

Provider 接口方法集为 `Push` / `Pull` / `Sync` / `Status`（签名以实现时设计为准），本地读写仍直连 `storage.Manager`。理由见 driver D1：git 与 server 都是 remote，缓存即工作副本。

### git 实现做适配器而非重写

现有 `internal/git.Manager` 不变，新 git provider 实现做薄适配层调用它，避免在抽象过程中引入行为漂移。

### 构造入口放 internal/provider

`internal/provider.New(cfg)` 返回具体 provider；cmd 层只依赖该入口。配置解析（含校验缺失项）也收敛于此。

## Risks / Trade-offs

- 抽象层时机过早、接口设计被 server 子 change 推翻 → 接口刻意保持最小，server 子 change 允许小幅演进接口（走 MODIFIED spec）
- 适配层与 git.Manager 重复错误包装 → 适配层不重复包装，透传底层错误
