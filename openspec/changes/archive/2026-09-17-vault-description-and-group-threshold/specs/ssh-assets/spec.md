## ADDED Requirements

### Requirement: Host 与 KeyPair 说明
Host 与 KeyPair 档案 SHALL 各有可选说明字段。`senv host add/edit`、`senv host get/list`、`senv keypair import/edit`、`senv keypair list` 以及 TUI 对应表单/详情 SHALL 读写该字段。说明上限与空说明规则遵循 vault-description。导出 OpenSSH 片段与私钥落盘 MUST NOT 把说明写入 ssh config 或密钥文件。MCP `ssh_host_list`/`ssh_host_get` SHALL 返回 Host 说明；MUST NOT 因此新增写工具。

#### Scenario: add host with description
- **WHEN** 用户执行 `senv host add web --hostname 10.0.0.1 --description "广告实验机"`
- **THEN** `host get web` 展示该说明，导出的 ssh 片段不含该文本

#### Scenario: list host shows description
- **WHEN** 该 Host 有说明
- **THEN** `host list` 与 MCP `ssh_host_list` 含该说明

#### Scenario: keypair description independent of comment
- **WHEN** 导入私钥自带 OpenSSH comment `user@host`，并设置说明「gitlab deploy」
- **THEN** comment 仍为密钥派生字段，说明为独立档案字段
