## 1. 事务能力

- [x] 1.1 `configTransaction` 支持新建文件与删除文件/条目，回滚时新建文件删除、删除内容恢复；验证：`go test ./internal/llm -run Transaction` 通过
- [x] 1.2 配套测试：新建+删除+写回混合场景下单点失败全量回滚；验证：单测全绿

## 2. 目录元数据

- [x] 2.1 从目录缓存读取指定 provider 的模型元数据（`name`/`description`/`limit.context`/`reasoning_options`），缓存缺失或字段缺失时返回「未知」而非报错；验证：`go test ./internal/llm -run Catalog` 通过
- [x] 2.2 配套测试：命中、缺缓存、缺字段三类用例；验证：单测全绿

## 3. 各 agent 投影

- [x] 3.1 claude-code：写 `modelPicker`（每模型一行、`replaceBuiltInOptions: true`）与默认模型；验证：tmp HOME 下回环 + 幂等（`go test ./internal/llm -run ClaudeCode`）
- [x] 3.2 codex：生成 `senv-<alias>.json`（合成必填元数据、写前自校验）并指向 `model_catalog_json`；验证：单测 + 本机 `codex debug models` 解析回环
- [x] 3.3 kimi：`default_model` + 每模型一条 `[models.*]`，`max_context_size` 取目录值/回退；验证：单测
- [x] 3.4 pi 与 opencode：`providers.<id>.models[]` / `provider.<id>.models{}` 写全部选中模型；验证：单测
- [x] 3.5 配套测试：五个 agent 的列表写回、默认模型、重复切换幂等；验证：单测全绿

## 4. 清理

- [x] 4.1 差集清理：缩小模型集与换 provider 两种场景删除旧 senv 条目与失效 catalog 文件；验证：单测覆盖「清理后配置无残留」
- [x] 4.2 配套测试：用户自有条目/自有 catalog 文件不被删除；目标不存在时幂等通过；验证：单测全绿

## 5. 收尾

- [x] 5.1 事务回滚测试：新建 catalog 与删除动作中途失败时全部回滚；验证：单测全绿
- [x] 5.2 `make check` 全绿；`openspec validate agent-model-set-agents --strict` 通过；回填 proposal 验证记录
