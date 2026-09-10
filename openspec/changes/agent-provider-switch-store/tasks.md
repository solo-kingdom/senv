## 1. 存储层

- [x] 1.1 `internal/storage`：泛化通用 entry helper（`entryKindForDir`），新增 `LLMProviderEntry` 与 `llm_providers/` 的 Save/Load/Delete/List；验证：`go build ./... && go test -race ./internal/storage`
- [x] 1.2 1.1 配对测试（读写回环、重复别名覆盖、list、delete、未知名校验）；验证：`go test -race ./internal/storage`

## 2. 领域管理器

- [x] 2.1 `internal/llm`：`ProviderModelIDs`（按 id 取目录模型集，排序）；验证：`go build ./...`
- [x] 2.2 2.1 配对测试（存在/不存在/无模型/坏缓存）；验证：`go test -race ./internal/llm`
- [x] 2.3 `internal/llm`：Provider 管理器 AddProvider（凭据二选一、`llm-keys` 写入、模型集装配、force 覆盖、mutate 原子写入）、List/Get/Remove（自有凭据连带删、外部引用保留）；验证：`go build ./...`
- [x] 2.4 2.3 配对测试（目录+自定义并集、缓存缺失报错、空模型集、default 校验、重复别名、remove 两种凭据归宿）；验证：`go test -race ./internal/llm`

## 3. CLI

- [x] 3.1 `cmd/ai_provider.go`：`senv ai provider add/list/show/remove`（resolveAuth、审计 `op_llm_provider`、输出不含凭据明文）；验证：`make build`
- [x] 3.2 3.1 配对测试（隔离 vault：add→list→show→remove 全链路、重复别名报错、缓存缺失报错）；验证：`go test -race ./cmd`

## 4. 收尾

- [x] 4.1 README 功能列表补 `senv ai provider` 一行；验证：人工过目
- [x] 4.2 `make check` 全绿，结果记入 proposal 验证记录；验证：`make check`
