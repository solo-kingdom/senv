## Why
完善 config 功能，目标是对私密配置进行加密存储：
1. 配置管理应该是分组的，参考 env/text
2. 相比于 env/text，配置应该新增 meta 信息，包含描述、保存位置
3. 支持安装操作，把单个/分组/全部配置安装到对应位置
4. 安装时校验文件内容是否变化，有变化则需要先备份
5. 支持卸载操作，即删除对应位置的文件，如果文件有改动，需要确认，未改动则直接删除
6. 安装/卸载操作本身需要先给出操作 plan (dry-run)，然后确认

## What Changes
- 本 change 是 taskflow driver，不直接改代码，只编排子 change

## Non-goals
- 不改动 env / text 的既有行为与存储格式
- 不做跨机器同步或远程分发，安装/卸载仅针对本机文件系统

## 涉及面
| 仓库 | 角色 | 说明 |
|------|------|------|
| . | 必须 | 会修改，实施前切任务分支 |

## 验收标准
- [x] 配置按分组管理（模型参考 env/text），且内容加密存储
- [x] 配置携带 meta 信息（描述、保存位置）
- [x] 支持安装：单个配置 / 单个分组 / 全部配置，安装到 meta 声明的保存位置
- [x] 安装前校验目标文件内容是否变化，有变化先备份再安装
- [x] 支持卸载：删除对应位置文件，文件被改动需确认，未改动直接删除
- [x] 安装/卸载默认先输出 plan（dry-run），确认后才执行

## Driver 协议
- 本 change 无 spec 增量（`.openspec.yaml` 已设 `skip_specs: true`）
- 子 change 一律命名 `config-encrypt-<slice>`，与本 change 同一 planning root；跨 root 时在涉及面表显式记录 root 或 store id
- 实现进度只认子 change 自己的 `tasks.md`；本文件的 checkbox 只在对应子 change 全勾且 `validate --strict` 通过后才勾
- 涉及面里角色为 `必须` 的仓在实施前切任务分支：没有则 `git switch -c`，已有则 `git switch`。不许 stash / reset / 强制切换。工作树 dirty 时：未提交路径仅含当前 task 的 OpenSpec change（`openspec/changes/config-encrypt-*`）则直接切；否则列出路径并确认是否继续 checkout。用户不同意、git 拒绝或切错仓时停下
- 只有「checkbox 全勾」「需要用户决策」「本轮预算耗尽」三种情况允许结束一轮；单项做不了就保持未勾，在验证记录写一行原因后继续下一项
- 结束时逐条列出未勾项与原因，不按 change 汇总

## 验证记录
- 2026-09-01 `make check`（fmt + vet + lint + `go test -race ./...`）全绿，于分支 `feat/config-encrypt`
- 子 change 均 `openspec validate --strict` 通过：storage / install / uninstall / tui
