## 1. 存储层扩展

- [x] 1.1 在 `internal/storage/types.go` 新增 `TextEntry` 类型（value, size, created_at, updated_at 字段），以及 `NewTextEntry(value string) *TextEntry` 构造函数
  - 验证: `go build ./...` 编译通过
- [x] 1.2 在 `internal/storage/manager.go` 新增 `SaveTextFile(group, key string, entry *TextEntry, password string)` 和 `SaveTextFileWithKey` 方法，加密 JSON 并写入 `dataPath/texts/{group}/{key}.enc`（自动创建目录）
- [x] 1.3 新增 `LoadTextFile` 和 `LoadTextFileWithKey` 方法，从 `texts/{group}/{key}.enc` 解密并反序列化为 `TextEntry`
- [x] 1.4 新增 `DeleteTextFile(group, key string)` 方法
- [x] 1.5 新增 `ListTextFiles(group string) ([]string, error)` 方法，列出目录下 `.enc` 文件对应的 key 名
- [x] 1.6 新增 `ListTextGroups() ([]string, error)` 方法，列出 `texts/` 下所有子目录
  - 验证: 单元测试创建多个 group 后 list 返回正确
- [x] 1.7 为存储层新增方法编写单元测试 `internal/storage/text_test.go`，覆盖 CRUD + 边界（key 不存在、group 不存在、512KB 限制校验）
  - 验证: `go test -v -race ./internal/storage/...` 全部通过

## 2. Text Manager 业务逻辑

- [x] 2.1 创建 `internal/text/manager.go`，实现 `Manager` 结构体（与 env.Manager 同构，持有 storage.Manager + password/key）
- [x] 2.2 实现 `Set(group, key, value string) error`：512KB 校验 → 构建 TextEntry → 调用 storage.SaveTextFile
- [x] 2.3 实现 `Get(group, key string) (string, error)`：调用 storage.LoadTextFile → 返回 value
- [x] 2.4 实现 `Delete(group, key string) error`
- [x] 2.5 实现 `List(group string) ([]TextInfo, error)`：列出 key + 从加密文件读 size/updated_at 元信息
- [x] 2.6 实现 `SetFromFile(group, key, filePath string) error` 和 `SetFromReader(group, key string, reader io.Reader) error`
- [x] 2.7 实现 `SetViaEditor(group, key string) error`：检查 key 是否存在 → 预填/空 → 打开编辑器 → 读取 → 存储
- [x] 2.8 实现 `GetToFile(group, key, outputPath string) error` 和 `GetToClipboard(group, key string) error`
- [x] 2.9 Group 管理：`AddGroup(name)`, `DeleteGroup(name)`, `ListGroups()`
- [x] 2.10 为 text manager 编写单元测试 `internal/text/manager_test.go`
  - 验证: `go test -v -race ./internal/text/...` 全部通过

## 3. 引用解析引擎

- [x] 3.1 创建 `internal/ref/resolver.go`，定义 `ValueGetter` 接口（`GetEnvValue`, `GetTextValue`）和 `ResolveOptions` 结构体（Loose, MaxDepth）
- [x] 3.2 实现引用正则解析：匹配 `{{(env|text):([^}]+)}}`，区分 `{{type:key}}` 和 `{{type:group:key}}`，处理 `\{{` 转义
- [x] 3.3 实现 `Resolve(value string, getter ValueGetter, opts ResolveOptions) (string, error)` 核心逻辑：正则扫描 → 逐个解析 → 获取实际值 → 递归调用 → 替换
- [x] 3.4 实现循环检测：维护 `visited map[string]bool`，检测到循环时报错并显示引用链
- [x] 3.5 实现最大深度检测（默认 10 层）
- [x] 3.6 实现严格/宽松模式：严格模式引用不存在报错；宽松模式保留原样 + stderr 警告
- [x] 3.7 为 resolver 编写单元测试 `internal/ref/resolver_test.go`，覆盖以上所有场景
  - 验证: `go test -v -race ./internal/ref/...` 全部通过

## 4. CLI 命令注册

- [x] 4.1 创建 `cmd/text.go`：注册 `textCmd` 到 rootCmd，添加 `-g` flag（默认 `default`），实现 `getTextManager()` 复用 session 缓存逻辑
- [x] 4.2 实现 `textSetCmd`：解析输入优先级（--file > stdin > args > editor），调用 text manager 对应方法
- [x] 4.3 实现 `textGetCmd`：支持 `-d`/`--decode`、`--loose`、`-o`/`--output`、`--copy` flags
- [x] 4.4 实现 `textDeleteCmd`、`textListCmd`
- [x] 4.5 实现 `textGroupCmd` 及子命令 `group list`、`group add`、`group delete`（带确认提示）
- [x] 4.6 修改 `cmd/env.go`：为 `envGetCmd` 和 `envListCmd` 添加 `-d`/`--decode` 和 `--loose` flags
- [x] 4.7 修改 `internal/env/manager.go`：让 `env.Manager` 实现 `ValueGetter` 接口，添加 `ResolveValue` 方法
  - 验证: `go test -v -race ./internal/env/...` 通过

## 5. 集成测试与文档

- [x] 5.1 编写集成测试：完整流程测试（init → text group add → text set → text get → text get -d → text delete → text group delete）
- [x] 5.2 编写引用集成测试：env 引用 text、text 引用 env、嵌套引用、循环检测
- [x] 5.3 更新 `README.md`：新增 text 功能使用说明和示例
- [x] 5.4 运行完整测试套件 `make check`（fmt + vet + lint + test）
  - 验证: `make check` 零错误
