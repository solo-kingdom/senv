## 1. 参数表面

- [x] 1.1 `--models`（逗号分隔/可重复、省略即全集、保序解析）与 `--default-model` 接入 `aiSwitchCmd`；验证：`go test ./cmd -run AISwitch` 通过
- [x] 1.2 `--model` 出现即报错并提示改用 `--models`/`--default-model`，校验发生在解锁之前；验证：单测断言非 0 退出且无文件写入
- [x] 1.3 配套测试：省略/显式/空集/越界/无默认模型/`--model` 六类参数用例；验证：单测全绿

## 2. 输出与审计

- [x] 2.1 成功输出改为 provider + Agent 模型集条数 + 默认模型 + 实际接入地址；验证：单测断言输出字段
- [x] 2.2 审计 detail 改为 `default:<model> models:<count>`，不含凭据材料；验证：单测断言 detail 格式

## 3. status 展示

- [x] 3.1 `senv ai status` 显示 `provider / 默认模型（N 个模型）` 与时间；验证：单测断言格式
- [x] 3.2 漂移提示：指针模型集与档案不一致时附提示，档案不可得时只省略提示不报错；验证：单测覆盖一致/漂移/档案缺失三类

## 4. 文档与收尾

- [x] 4.1 更新 `.agents/skills/senv-cli/SKILL.md` 的 switch/status 说明与示例（含 `--model` 已移除）；验证：`go run . ai switch --help` 与文档一致
- [x] 4.2 `make check` 全绿；`openspec validate agent-model-set-cli --strict` 通过；回填 proposal 验证记录
