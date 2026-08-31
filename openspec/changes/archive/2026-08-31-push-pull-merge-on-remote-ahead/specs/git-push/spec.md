## ADDED Requirements

### Requirement: push 在远程有更新时先 merge 再推送

`senv push` 与 `senv git push`（默认 add+commit+push、`-m`、`--only`）以及交互菜单中的「推送」，在存在本地可推送提交且远程分支含有本地没有的提交时，MUST 先 fetch，再以 merge 方式拉取（允许快进），merge 成功后 MUST 再 push。MUST NOT 使用 force push。远程没有新提交时 MUST 直接 push，不得无故创建 merge commit。

#### Scenario: 远程领先且无冲突

- **WHEN** 本地有未推送提交，远程同一分支有本地没有的提交，且两边改动可以自动合并
- **THEN** 系统完成 merge（或快进）后 push，并报告推送成功

#### Scenario: 远程未领先

- **WHEN** 本地有未推送提交，远程没有本地缺少的提交
- **THEN** 系统不创建 merge commit，直接 push

#### Scenario: 没有本地可推送提交

- **WHEN** 相对上游没有未推送提交
- **THEN** 系统不执行 merge，并保持现有「无可推送」语义（`--only` 可失败提示；默认无改动路径可视为已对齐成功）

### Requirement: merge 冲突时停止且不推送

远程更新与本地提交合并产生冲突时，系统 MUST 停止推送流程，MUST 中止 merge 以恢复到 merge 前状态，MUST 向用户说明冲突及数据仓路径，MUST NOT 留下半成品 merge 状态，MUST NOT 继续 push，MUST NOT force push。

#### Scenario: merge 冲突

- **WHEN** push 过程中 merge 产生冲突
- **THEN** 系统中止 merge，返回明确错误（含路径/指引），远程与本地均不因本次 push 产生新推送，且仓库不处于 merge 进行中

### Requirement: 脏工作区需要 merge 时拒绝

当 push 路径未先提交（例如 `--only` 或交互纯推送）且工作区有未提交更改、同时又需要 merge 远程更新时，系统 MUST 拒绝 merge 与 push，并提示用户先提交或清理工作区。

#### Scenario: --only 时工作区脏且远程领先

- **WHEN** 用户使用 `--only`（或等价纯推送），工作区有未提交更改，且远程有新提交
- **THEN** 系统不执行 merge、不 push，并返回说明需先提交或清理的错误

### Requirement: 推送失败信息可读

push 流程中 fetch、merge、push 等底层 git 命令失败时，系统 MUST 在错误信息中包含 git 的 stdout/stderr（或等价输出），不得仅暴露 `exit status 1`。

#### Scenario: merge 后 push 仍被拒

- **WHEN** merge 已成功但随后 `git push` 失败（权限、钩子等）
- **THEN** 用户可见的错误包含 git 输出中的失败原因
