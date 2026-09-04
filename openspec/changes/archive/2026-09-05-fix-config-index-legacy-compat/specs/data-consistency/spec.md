## ADDED Requirements

### Requirement: doctor 遇隔离配置条目降级报告

`CheckConsistency` 与 `senv doctor` 遇到被隔离的"仅不可移植"配置条目时 SHALL 继续完成体检：合法条目照常探测并计入报告，隔离条目以警告形式列出原名与修复指引，不计入可解密总数也不视为解密失败。体检 MUST NOT 尝试打开或解密被隔离条目的密文文件。

#### Scenario: 含隔离条目时体检继续

- **WHEN** 项目索引同时含一条合法配置与一条"仅不可移植"条目（如 `feg:ai-ops-portal.pub`），运行 `senv doctor`
- **THEN** 体检正常完成，合法配置计入 config 探测计数，隔离条目以警告列出并提示 `senv config repair`，不因索引加载失败而中断

#### Scenario: 隔离条目不计入失败清单

- **WHEN** 上述项目存在其他真实脱节文件时运行 `senv doctor`
- **THEN** 报告的 failed 清表仅包含真实脱节文件；隔离条目只出现在警告区，不被计为 NOT OK 探测
