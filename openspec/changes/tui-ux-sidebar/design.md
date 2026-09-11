# Design: tui-ux-sidebar

## Context

config tab 的侧栏（2026-09-01 tui-config-group-sidebar 交付）是唯一被 spec 固化的分组范式。env/text 的组列表功能等价但形态落后；两者数据模型已有组概念，升级只涉 UI 层。

## Goals / Non-Goals

**Goals:** env/text 与 config 同款侧栏；搜索跳转、过滤、（⑤ 的）多选在侧栏结构下行为一致。

**Non-Goals:** 不动数据模型与存储层；不给无组实体加组；不改 config 侧栏。

## Decisions

- **config 侧栏实现下沉共享**：侧栏渲染（All 伪组、计数、焦点态）从 `config_tab.go` 提取进列表组件，config/env/text 三处消费；config 行为零变化（范式源不动）。
- **All 视图行前缀统一 `group/key`**：与 config 一致；env 的 default 置顶与 `(default)` 标识在侧栏分组列表保留（config 无 default 概念，不互相污染）。
- **text 空分组改为显示（计数 0）**：与 config「过滤期 0 计数组仍显示」的行为对齐，消除「空组消失」的隐性规则；空组在 All 视图无条目、选中后右侧显示空状态提示。
- **游标语义**：侧栏与条目栏各自维护游标，焦点切到条目栏时定位到该组第一条（对齐 config-tui 既有要求）。

## 数据流与错误处理

数据装载与共享快照不变：All 视图条目集 = 快照全量按组拼接，组件按可见窗口渲染；过滤计数由过滤谓词对全量条目一次遍历得出，不触发重复读盘。

## Risks / Trade-offs

- [env/text 是用户最常驻的 Tab，改版适应成本高] → All 默认选中 + `group/key` 前缀让旧「平铺」习惯仍可用；组操作键位（`t`/重命名/删除）不变
- [搜索跳转定位逻辑需适配 All/真实分组两种侧栏态] → 沿用 config 既有「跳转选中所属真实分组」语义，跳转后侧栏态确定

## Open Questions

无。
