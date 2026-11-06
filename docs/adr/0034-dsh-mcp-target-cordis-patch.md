# DSH（DeepSeek Harness）MCP 目标：home 级 cordis patch 的受管块 + insert 行

DSH 的 MCP server 不是 `mcpServers` 对象，而是 cordis loader 的 patch 行：`$DSH_HOME/cordis.patch.yml` 里的一条 `- insert: [...]` 才会新增一个 `@deepseek-ai/dsh-mcp-client` 实例；裸 `- id/name/config` 行只能覆盖已由 bundle 挂载的条目，找不到 id 时**只打一行 stderr warning，退出码仍是 0，条目被静默丢弃**。senv 新增 `dsh` 目标时要按这个语义写，且不能破坏用户手写的 patch 文件。

## 决策

1. **`dsh` 作为独立目标注册**，id `dsh`、Name `DeepSeek Harness`、别名 `deepseek-harness` 仅查找接受，展示面只显示 `dsh`（沿用 ADR-0030/0032 的别名规则）。`mcp install dsh` 与 `mcp export --agent dsh` 共用同一写路径；**不扩展 `ai switch`**——LLM provider 在 DSH 里是 `llm-*` bundle 的 config 重写，字段面与 `ai switch` 现有适配器不同，属另一件事。
2. **写 `<DSH_HOME>/cordis.patch.yml`（home 级用户 patch 层），不写 profile 级 `cordis.patch.yml`、不做 config-only bundle。**
   - 层序（app-boot `readProfilePatches`）：bundle 层 → profile `cordis.patch.yml` → home `cordis.patch.yml` → `--patch`。home 层在所有 profile 之后应用，因此一处写入对 Desktop、web、headless、ACP 全部生效，符合 senv `--scope user` 的全局语义。
   - `dsh-hmr` 由 `dsh-base` 提供并 watch 两个用户 patch 文件（home 文件即使尚不存在也在 watch 目标内），写盘后热重载。
   - profile 级 patch 被 DSH 的原生 fatal-recovery（`sanitizeProfile`）改名备份；home 层明确不在恢复范围内。
   - profile 名对 senv 无确定性来源：DSH 不读 `DSH_PROFILE`（本机环境变量只是 harness 自己标注）；CLI 用 `--profile`，Desktop 硬编码 `desktop` 且拒绝 CLI 的 boot/dump。写 home 层不需要猜 profile。
   - 代价：所有 profile 都会加载这些 MCP server（包括 headless/sdk 的自动化会话）。这与「全局 MCP 配置」的语义一致（Claude Code 的 `~/.claude.json`、Cursor 的全局 `mcp.json` 同理），在 ADR 与 skill 文档里写明。
3. **所有受管 server 共用一个 `- insert: [...]` patch 行，每个 server 是该行 `insert` 列表里的一个条目，`id` 固定为 `senv-mcp-<serverName>`。**
   - 一个 insert 行携带全部受管条目，按 id 排序渲染，重复导出字节稳定；DSH 的 `applyPatches` 对 insert 列表逐项挂载，语义与「每 server 一行」等价。
   - loader 对缺 id 的 insert 条目会生成随机 id（`ensureInsertIds`），无法稳定回写；显式 id 是幂等与 drift 检测的前提。
   - 只写 `insert` 行；裸 id 行被静默丢弃，因此单元测试显式断言受管块里不出现顶层 `- id:` 行。
4. **只重写文件尾部一个标记块，块外字节逐字节保留。**
   - DSH 用 js-yaml `JSON_SCHEMA` + `!!js` 标量解析 patch；条目顺序有语义（同 id 的后写覆盖前写，`config` 是整体替换不是深合并）。整文件 parse→重排→重写会丢注释与 `!!js`/块标量风格，还可能改变语义。
   - 标记块（`# >>> senv-managed MCP servers ... >>>` / `# <<< ... <<<`）是 senv 唯一读写的区域；块外内容不做归一、不重排。
   - 受管块内容丢失/夹带非 senv 条目时 fail closed（报错、不写盘）；块外出现 senv id 前缀（无论是否已有受管块）同样在读取时报错，避免重复 id 或把不属于 senv 的行当成受管内容。
   - 删空最后一个条目时把文件收尾成显式 `[]`：DSH 对空文件/纯注释文件是 boot error。
   - 追加前只替换**解析到的顶层空列表节点**（`[]` / `[] # note`），不做文本匹配：多行标量或嵌套 flow 序列里恰好写成 `[]` 的行是用户内容，必须原样保留。
5. **`serverName` 必须满足 mcp-client 契约 `^[A-Za-z0-9_-]{1,32}$`（同 scope 唯一）。**
   - 合规别名原样使用（保留 `mcp__<serverName>__<tool>` 的可读命名空间）；不合规别名按字节归一为 `-`，再追加 6 位 alias sha256 后缀，保证确定性且不同别名不塌缩到同一 namespace。
   - 写盘前校验：受管 id 与 serverName 在最终文件里各出现一次；与文件内用户自建条目的 serverName 撞名时拒绝写入（DSH 会让后加载的实例失败）；两个别名归一后落到同一 id 时 fail closed，不静默覆盖。
   - 受管条目被手工改坏（如删掉 `transport`、写成 schema 拒绝的值）时读取为**不可比对的 transport**，指纹必然失配 → 计划判 drift，`--force` 可修复，不会当成 up-to-date 跳过。
6. **transport 映射：stdio → `stdio`，http → `streamable-http`；`sse` plan-error。**
   mcp-client 的 Config schema 是 `z.union([z.const("stdio"), z.const("streamable-http")])`，没有 sse 值；把 sse 档案写成 streamable-http 是静默改写（沿用 codex/omo 先例，计划阶段报错）。
7. **明文取舍沿用 ADR-0008**：导出的 env/url/header 为解析后的明文，写 0600 并备份 `<file>.bak`，计划里标 `[明文]`。
   DSH 的 mcp-client 支持 `!!js process.env.X` 引用，但那要求变量存在于 **DSH 进程**的启动环境；senv export 不负责给 DSH 注入环境，写入 `!!js` 只会让条目在变量缺失时拿到非字符串或空值。用户可以把受管条目手工改成 `!!js`，senv 会按「本地修改」判 drift，需要 `--force` 才覆盖。
8. **`--scope project` 与 `user` 解析到同一文件**：DSH 没有项目级 patch 层，不做假装区分的第二条路径（同 claude-code 等不区分 scope 的目标）。
9. **不声明前置依赖**：`@deepseek-ai/dsh-mcp-client` 是 `@deepseek-ai/dsh` 安装包的直接依赖，安装作用域解析可直接 import，无需 `dsh plugin add`。

## Considered Options

- **写 profile 级 `profiles/<name>/cordis.patch.yml`（调研报告的初始建议）**：更贴近 DSH 单 profile 的表面，但要猜 profile 名（DSH 不读 `DSH_PROFILE`，Desktop 硬编码且拒绝 CLI dump），profile 被 `sanitizeProfile` 恢复时会整文件改名，且只作用于一个表面。若将来用户要求「只给 desktop 写」，把 `ConfigPath` 换成 profile 路径即可，格式与合并逻辑不变。
- **config-only bundle（`dsh.bundle.patch` + plugin_manager/pnpm 安装）**：能分发共享，但引入包管理器副作用与 profile 清单改写；senv 管理的是本机用户档案，不需要。
- **整文件 parse → 改数组 → dump**：见决策 4；注释、`!!js` 风格与条目顺序都会受影响。
- **`!!js process.env.X` 引用代替明文**：见决策 7；注入依赖 DSH 启动环境，不是 senv 的契约。
- **sse 档案悄悄改写成 streamable-http**：生成用户没要求的配置，且语义不等价。

## Consequences

- 新增 `FormatCordisPatch` 与 `internal/agentcfg/cordis_patch.go`（标记块定位/解析/渲染/冲突校验）；不引入新依赖（`gopkg.in/yaml.v3` 已是直接依赖，omp YAML 写回在用）。
- `internal/mcp/export.go` 的 `agentFile` 增加文本+受管块分支；`cmd/mcp_install.go` 增加 `installCordisPatch`；`cmd/mcp_export.go` 的 `--print` 增加 DSH 片段。TUI 由注册表驱动，无需改 UI 代码。
- spec `mcp-server-export` 新增 dsh 目标要求；`.agents/skills/senv-cli/SKILL.md` 与 `README.md` 的 agent 枚举同步。
- 写盘权限：新建 DSH home 目录 0700、文件 0600；覆盖已有宽松权限的文件时由 `WriteWithBackup` 收紧到 0600。
- 冒烟锚点：DSH Desktop 0.2.2-rc.1（app-boot `readProfilePatches`/`applyPatches`、dsh-home-paths `resolveDshHome`、dsh-hmr watch 列表、mcp-client Config）。上游漂移（文件名、层序、Config 字段或 transport 取值变化）时需要跟改；skill 文档记录锚点。
- 已知盲区：senv 只校验 home 文件内的 id/serverName 唯一性；用户在 profile 级 patch 或 bundle 里手写同名 serverName 时检测不到，由 DSH 在运行时对后加载的实例报错。
- 测试隔离：所有涉及 `--all`/dsh 的测试必须钉住 `DSH_HOME`，否则开发者的真实 `~/.dsh` 可能被写（单测已加）。

## Status

accepted（2026-11-06 随 change `add-dsh-mcp-target` 实现落地；本机 DSH Desktop 0.2.2-rc.1 只读调研 + 隔离目录测试验证）
