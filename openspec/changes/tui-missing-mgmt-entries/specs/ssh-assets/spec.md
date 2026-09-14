## ADDED Requirements

### Requirement: TUI 导出撤回与私钥清理

TUI SHALL 为两条 CLI 管理命令补齐入口，语义与 CLI 逐一等价。SSH Tab SHALL 提供 `u`（unexport）键：按键后系统 SHALL 先只读预检（`~/.ssh/config` 的 senv 注册行是否在位、`~/.ssh/senv/groups/` 下组片段数），无可撤回项时 toast 告知且不进确认框；否则 SHALL 弹确认框，逐项列出将发生的动作——移除 senv Include 注册行（仅在位时）、删除 N 个组片段（仅 fragments>0 时，N 实填）、并明示「`~/.ssh/senv/keys/` 下落盘私钥保留」；`enter/y` 确认后异步执行与 CLI `senv host unexport` 同一 `Manager.Unexport` 编排，`esc/n` 取消且零副作用。KeyPair Tab SHALL 提供 `p`（prune）键：按键后异步取候选清单，非空时 SHALL 先弹列表（逐条路径，vault 中仍有对应 keypair 的标注 `(keypair still in vault)`）再于同屏请求确认；`enter/y` 确认后异步删除与 CLI `senv keypair prune` 同一 `PruneCandidates`/`DeletePrunedFiles` 白名单集合，`esc/n` 取消且 MUST NOT 删除任何文件。两操作 MUST NOT 触碰 vault 档案；prune 的删除集合 MUST NOT 包含被任何 host 引用的落盘私钥。两入口执行后 SHALL 以 toast 报告实际结果（撤回了几项/删除了几个文件）并记录本机操作审计。

#### Scenario: SSH Tab 撤回已应用的导出

- **WHEN** 已应用导出（注册行在位、`groups/` 有 3 个片段），用户在 SSH Tab 按 `u`，确认框按 `y`
- **THEN** 系统 SHALL 异步执行 unexport：`~/.ssh/config` 的 senv Include 行被移除（其余内容不动并留 `.senv-bak`），3 个组片段被删除，`keys/` 下落盘私钥与 vault 档案原样保留，toast 报告实际撤回项并记 `op_ssh_host` 审计

#### Scenario: SSH Tab 无可撤回项

- **WHEN** 本无注册行且 `groups/` 无片段，用户在 SSH Tab 按 `u`
- **THEN** 系统 SHALL toast 告知 nothing to unexport，不弹确认框、不产生任何文件副作用

#### Scenario: SSH Tab 取消撤回

- **WHEN** 用户在 unexport 确认框按 `esc`/`n`
- **THEN** 系统 MUST NOT 改动 `~/.ssh/config` 与 `groups/`，返回 normal mode

#### Scenario: KeyPair Tab 清理未引用私钥

- **WHEN** `keys/` 下存在 2 个未被任何 host 引用的落盘私钥（其中 1 个对应 keypair 仍在 vault），用户在 KeyPair Tab 按 `p`，列表逐条展示路径与 `(keypair still in vault)` 标注，用户按 `y` 确认
- **THEN** 系统 SHALL 异步删除该 2 个文件并 toast `deleted 2 file(s)`，被引用 keypair 的落盘文件与 vault 均无变化，记 `op_ssh_keypair` 审计

#### Scenario: KeyPair Tab 无候选可清理

- **WHEN** 所有落盘私钥均被 host 引用，用户按 `p`
- **THEN** 系统 SHALL toast 提示无可清理项，不弹列表、不删除任何文件

#### Scenario: KeyPair Tab 取消清理

- **WHEN** 用户在 prune 列表确认屏按 `esc`/`n`
- **THEN** 系统 MUST NOT 删除任何文件，返回 normal mode

#### Scenario: prune 部分删除失败

- **WHEN** 确认删除后其中 1 个文件因权限缺失删除失败
- **THEN** 系统 SHALL 保留已删结果不回滚，以警告呈现 `deleted N-1 of N` 与错误原因，列表刷新反映实际删除并记失败审计
