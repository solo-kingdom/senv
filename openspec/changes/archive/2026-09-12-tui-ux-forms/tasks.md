## 1. 迁移

- [x] 1.1 env 新建迁移结构化表单：key（必填 + 组内冲突校验）+ value（`formSecret` 遮蔽），提交调 `env.Manager.Set`；验证：n 填写后保存成功、value 不出现在渲染/提示文本，重名 key 内联报错不丢输入
- [x] 1.2 config 创建迁移结构化表单：name/源路径/target/分组（ref）/描述，校验 + reopen 回填；验证：必填缺失内联报错、`esc` 取消零副作用、成功创建含分组与描述
- [x] 1.3 删除手搓状态机：`configModeCreate*` 向导与 env 新建连续弹窗代码整段移除；验证：`go vet` 无残留引用，grep 无 `configModeCreate` 死代码

## 2. 文档与回归

- [x] 2.1 同步 `.agents/skills/senv-cli/SKILL.md`（创建流程描述如涉及）；验证：文档与实现一致
- [x] 2.2 `make check` + 表单契约回归（tab 导航/esc 隔离全局键/失败重开），结果写入 proposal 验证记录；验证：退出码 0
