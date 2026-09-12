## 1. ai switch 诊断增强

- [x] 1.1 `internal/llm/switch.go` `resolveCredential`：区分"引用条目不存在"（storage 哨兵错误判定）与"解密失败"，前者错误文案指明完整引用名与修复指引（`senv text add llm-keys <alias>` 或跨机同步）
- [x] 1.2 单测：text 条目缺失时错误含 `text:llm-keys/<alias>` 全名与指引、且 Switch 零写入；条目存在但密文损坏时保留既有 decrypt 包装
- 验证：`go test ./internal/llm/ -race` 全绿

## 2. mcp export 宽松模式

- [x] 2.1 `internal/mcp/export.go`：`ExportItem` 增 `Warnings []string`；`resolveEntry` 走 loose 回调（保留模板原文）并收集 warning，不再返回错误；`Plan` 对含未解析引用的条目正常写入并标注，不再置 `ActionError`
- [x] 2.2 `cmd/mcp_export.go`：resolve 回调切换 loose；新增 warnings 统一打印（stderr，含缺失的 env/text 名）；`--dry-run` / `--print` 展示含未解析引用标注与 warnings
- [x] 2.3 单测/集成：引用缺失 → 文件写入、值含模板原文、stderr warning 列出缺失名；引用齐全 → 输出与既有行为一致（快照对照）；漂移 + `--force` 组合行为不变
- [x] 2.4 spec 场景逐条落测试（`specs/mcp-server-export` delta 三个场景）
- 验证：`go test ./internal/mcp/ ./cmd/... -race` 全绿

## 3. 收尾

- [x] 3.1 `make check` 通过
