## ADDED Requirements

### Requirement: git add 与忽略规则覆盖全部机器本地工件

git 同步 SHALL 在提交前排除全部机器本地工件（server token、MCP 账本、agent 指针、同步/口令锁、同步状态快照、TUI 首屏快照、模型目录缓存等）。系统 SHALL 由同一份登记表派生 git add 排除路径与 `.gitignore` 条目，即使仓库缺 `.gitignore` MUST NOT 暂存这些文件。

#### Scenario: 机器本地工件不被提交
- **WHEN** 工作区包含 tui-snapshot.enc、agent-pointers.json、server-token.json 等机器本地工件
- **THEN** git add/commit 后这些文件不在暂存区，也不出现在提交内容中

#### Scenario: .gitignore 由登记表补全
- **WHEN** 初始化或写入机器本地文件时 `.gitignore` 缺少登记表中的条目
- **THEN** 系统补写缺失条目，不重复、不改写既有内容
