## 1. 多选模型集

- [x] 1.1 切换状态机扩展：`s` 进入多选（space 勾选、进入默认全选）、再选默认模型、再确认；验证：`go test ./internal/tui -run AITab` 通过
- [x] 1.2 空模型集在提交前拦截并提示；验证：单测断言未调用 SwitchManager
- [x] 1.3 配套测试：默认全选、勾选子集、空集拦截、Help 文案；验证：单测全绿

## 2. 仅换默认模型

- [x] 2.1 `m` 候选限定为指针中的 Agent 模型集，只更新默认模型；验证：单测断言提交参数中模型集不变
- [x] 2.2 配套测试：未指向时提示先切换、集合内选择、档案默认模型不在集合内时游标落首项；验证：单测全绿

## 3. 展示与收尾

- [x] 3.1 agent 行展示默认模型 + 条数与漂移提示（与 status 口径一致）；验证：单测断言渲染文本不含凭据且含条数
- [x] 3.2 更新 README「TUI mode」键位表与 `.agents/skills/senv-cli/SKILL.md` 的 AI Tab 说明；验证：文档与 Help() 一致
- [x] 3.3 `make check` 全绿；`openspec validate agent-model-set-tui --strict` 通过；回填 proposal 验证记录
