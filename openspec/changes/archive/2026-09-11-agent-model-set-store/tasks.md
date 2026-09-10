## 1. 指针结构

- [x] 1.1 高优先级：`AgentPointer` 扩展为 `provider`/`models[]`/`default_model`/`switched_at`，更新 `Set` 与 JSON 编解码；验证：`go test ./internal/llm -run Pointer` 通过
- [x] 1.2 配套测试：新结构落盘与往返、文件权限 0600、模型集保序；验证：同上全绿

## 2. version 1 兼容

- [x] 2.1 `LoadPointers` 解码后归一：`models` 为空且 `model` 非空时视为 `models=[model]`、`default_model=model`；验证：单测用手工构造的 v1 文件读取成功
- [x] 2.2 配套测试：v1 文件不报错、视为单元素集、下一次写回后升级为新结构；验证：单测全绿

## 3. 切换内核

- [x] 3.1 `SwitchRequest` 改为 `Models []string` + `DefaultModel string`，`Switch` 入参同步；实现模型集非空、成员属于 Provider 模型集、默认模型属于 Agent 模型集的校验，非法时不写任何文件；验证：`go test ./internal/llm -run Switch` 通过
- [x] 3.2 配套测试：全选、显式子集、空集、含档案外模型、默认模型越界、无默认模型六类用例；验证：单测全绿
- [x] 3.3 `cmd/ai_switch.go` 调用点适配编译（暂不新增 flag）；验证：`go build ./...` 通过

## 4. 收尾

- [x] 4.1 `make check` 全绿；`openspec validate agent-model-set-store --strict` 通过；回填 proposal 验证记录
