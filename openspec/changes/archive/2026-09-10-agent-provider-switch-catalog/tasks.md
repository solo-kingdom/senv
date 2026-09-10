## 1. internal/llm 目录包

- [x] 1.1 实现目录类型与 `Fetch/Parse/Validate`（URL 与 `*http.Client` 注入，30s 超时）；验证：`go build ./...`
- [x] 1.2 1.1 配对测试（httptest：合法 payload、非法 payload、网络失败）；验证：`go test -race ./internal/llm`
- [x] 1.3 实现缓存 `Save/Load`（envelope version/fetched_at/source，temp+rename 原子写，目录 0700 / 文件 0600）；验证：`go build ./...`
- [x] 1.4 1.3 配对测试（写读回环、替换保留旧文件、损坏缓存报错）；验证：`go test -race ./internal/llm`

## 2. CLI 命令

- [x] 2.1 新增 `senv ai` 根命令与 `senv ai refresh [--source URL]`（失败不动旧缓存、输出 provider/model 数摘要）；验证：`make build`
- [x] 2.2 2.1 配对测试（configPathFn 注入临时目录：成功刷新、失败保留旧缓存）；验证：`go test -race ./cmd`
- [x] 2.3 新增 `senv ai catalog status`（离线展示拉取时间/来源/数量；无缓存提示 refresh；损坏报错）；验证：`make build`
- [x] 2.4 2.3 配对测试（有缓存、无缓存、损坏三种场景）；验证：`go test -race ./cmd`

## 3. 收尾

- [x] 3.1 README 功能列表补充 `senv ai refresh` 一行；验证：人工过目
- [x] 3.2 全量检查 `make check` 通过，结果记入 proposal 验证记录；验证：`make check`
