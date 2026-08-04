## ADDED Requirements

### Requirement: git sync 执行双向同步

`senv git sync` SHALL 按顺序执行：若工作区有未提交更改则 add+commit；然后 fetch 并以 rebase 方式拉取远程更新；最后若本地有未推送提交则 push。MUST NOT 使用 force push。交互菜单中的「同步」MUST 使用同一流程。

#### Scenario: 本地有改动且远程 ahead

- **WHEN** 工作区有未提交更改，且远程分支领先本地
- **THEN** 系统先提交本地更改，再 `pull --rebase`，成功后 push，并报告同步成功

#### Scenario: 工作区干净且已与远程对齐

- **WHEN** 无未提交更改且无未推送提交，远程无新提交
- **THEN** 系统成功退出（不报错），并提示已是最新

#### Scenario: 仅需推送

- **WHEN** 工作区干净，本地有未推送提交，远程未领先
- **THEN** 系统跳过 commit，必要时 fetch 确认后 push

### Requirement: sync 提交信息

无 `-m` 时，`senv git sync` SHALL 使用时间戳自动生成提交信息；若无待提交更改则跳过 commit 步骤，不因缺少 `-m` 而失败。提供 `-m` 时 MUST 使用该信息提交（仅当存在待提交更改）。

#### Scenario: 无改动时不要求 -m

- **WHEN** 工作区干净，用户运行 `senv git sync`（无 `-m`）
- **THEN** 系统跳过 commit，继续 pull/push 流程且不报错

#### Scenario: 有改动时使用 -m

- **WHEN** 工作区有更改，用户运行 `senv git sync -m "msg"`
- **THEN** 系统以 `msg` 创建提交后再继续同步

### Requirement: rebase 冲突时安全中止

`pull --rebase` 发生冲突时，系统 MUST 自动 `rebase --abort`，MUST 向用户说明冲突及数据仓路径，MUST NOT 留下半成品 rebase 状态，MUST NOT 继续 push。

#### Scenario: rebase 冲突

- **WHEN** sync 过程中 rebase 产生冲突
- **THEN** 系统中止 rebase，返回明确错误（含路径/指引），且不执行 push

### Requirement: git 操作失败信息可读

push、pull、sync 中底层 git 命令失败时，系统 MUST 在错误信息中包含 git 的 stdout/stderr（或等价输出），不得仅暴露 `exit status 1`。

#### Scenario: push 被拒时展示原因

- **WHEN** `git push` 因 non-fast-forward 或权限等原因失败
- **THEN** 用户可见的错误包含 git 输出中的拒绝原因

### Requirement: 无待推送提交不视为错误

在 sync 流程末尾，若本地相对上游没有未推送提交，系统 SHALL 视为成功（已对齐），MUST NOT 因此返回错误。纯 `senv git push --only` 在无待推送提交时 MAY 保持提示性失败或改为成功，但 sync MUST 成功。

#### Scenario: sync 已对齐

- **WHEN** sync 完成 commit/pull 后无需 push
- **THEN** 命令以成功状态退出
