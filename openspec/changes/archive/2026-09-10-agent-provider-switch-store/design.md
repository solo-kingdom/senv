## Context

SSH 资产（`internal/storage/ssh.go` + `internal/ssh` + `cmd/ssh.go`）确立了「专用加密目录 + 类型化 entry + 域管理器 + vault mutation 锁」范式，本 change 全程照搬。模型目录缓存由 catalog 子 change 提供（`internal/llm.Load`）。D4 已定：凭据存 vault、档案只存引用。

## Goals / Non-Goals

**Goals:** 档案与凭据分离存储；add 时从目录缓存装配模型集并允许自定义；提供完整 CRUD CLI
**Non-Goals:** agent 配置写入（agents 子 change）；凭据解密代取；定时刷新

## Decisions

1. **存储层**：`internal/storage/llm_provider.go`，目录 `llm_providers/`，`LLMProviderEntry`（alias、base_url、credential_ref、catalog_provider、models、default_model、时间戳）。通用 entry helper（`saveSSHEntry` 等）本就是 dir 参数化的，把 `sshKindForDir` 泛化为 `entryKindForDir` 并去掉错误文案里的硬编码「SSH」前缀后直接复用。
2. **凭据存 text 保留组 `llm-keys`**（D4）：`--api-key` 写入 `text:llm-keys/<alias>`，档案存引用；`--key-ref` 接受 `env:<g>/<k>` / `text:<g>/<k>` 指向既有条目，沿用 ref-system 语法，agents 子 change 的解析器零新增格式。备选「凭据内嵌档案」被 D4 否决（档案元数据会被 list/show/MCP 触达，密文不入口）。
3. **模型集装配**：并集 = `--catalog-provider` 读缓存（`internal/llm.Load` → `ProviderModelIDs`）∪ `--model`（去重排序）；缓存缺失/无该 provider → 报错提示 refresh，不写入。新鲜度 > 7 天打警告但继续（不阻塞离线使用）。
4. **原子性**：add 的凭据写入与档案保存在同一 `WithVaultMutation` 内；先写凭据后写档案，档案失败时尽力回删刚写的凭据（vault mutation 是锁非事务，孤儿风险窗口极小）。
5. **CLI**：`senv ai provider add/list/show/remove`，认证走 `resolveAuth`（同 `getSSHManager`）；审计新增 `op_llm_provider`。使用示例：
   ```bash
   senv ai provider add myprovider --base-url https://api.example.com \
       --api-key sk-xxx --catalog-provider anthropic --model custom-1
   senv ai provider add other --base-url https://... --key-ref env/OPENAI/KEY
   senv ai provider list | show myprovider | remove myprovider
   ```

## 数据流

```
add: 参数校验 ─▶ [目录缓存] 模型集装配 ─▶ vault mutation ─┬─▶ text 写凭据(llm-keys)
                                                      └─▶ llm_providers 写档案
list/show: llm_providers 读档案 ─▶ 渲染（不含凭据明文）
remove: 读档案 ─▶ mutation ─┬─▶ 删档案
                            └─▶ 引用属 llm-keys 时删凭据
```

## 错误处理策略

- 参数非法 / 别名重复 / 模型集为空 / default 不在集内：写入前失败，vault 零变更
- 缓存缺失或 provider 不存在：明确报错 + 提示 `senv ai refresh`
- 档案写入失败：尽力回删本次写入的凭据；若回删失败，错误信息附带孤儿凭据位置
- remove 不存在的别名：报错；外部凭据引用：保留并在输出中说明

## Risks / Trade-offs

- [`llm-keys` 组内的凭据条目会出现在通用 `senv text` 视图中] → 可接受：它本来就是 vault 用户数据；MCP 只读查询不解析该组
- [`--api-key` 走 argv 有进程列表暴露面] → 与 `senv env set` 现状一致；后续可加 `--api-key-stdin`
- [通用 helper 改名触及 ssh.go] → 同文件小重构，消息文案由 ssh 测试兜底
