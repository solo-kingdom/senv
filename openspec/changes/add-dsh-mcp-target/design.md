## Context

DSH（DeepSeek Harness）0.2.2-rc.1 的配置是 cordis loader 的 patch 树：bundle 层 → profile `cordis.patch.yml` → `$DSH_HOME/cordis.patch.yml`（home 用户层，优先级最高）→ CLI `--patch`。MCP 由 `@deepseek-ai/dsh-mcp-client` 提供，每个 server 是一个 loader 条目（`serverName`/`transport`/`command`/`args`/`env` 或 `url`/`headers`）。新增 server 只能通过 patch 行的 `insert` 列表；裸 id 行是「覆盖已有条目」，目标不存在时静默跳过。

事实证据（只读调研，报告 `/tmp/dsh-mcp-research.md`，不入库）：app-boot `readProfilePatches`/`applyPatches`/`parsePatchList`、dsh-home-paths `resolveDshHome`、dsh-hmr watch 列表、mcp-client Config schema（`transport` 仅 `stdio`/`streamable-http`，`serverName` 正则）。

## Goals / Non-Goals

**Goals:**

- `senv mcp export --agent dsh` / `senv mcp install dsh` 把档案写进 DSH 真正读取的层，且幂等、可撤回、能识别 drift。
- 不破坏用户手写 patch 文件（注释、`!!js`、其它条目、条目顺序）。
- 写出 DSH 一定接受的最小配置形状（insert 行、合法 serverName/transport）。

**Non-Goals:**

- 不写 profile 级 patch、不做 bundle（理由见决策 2）。
- 不支持 sse（schema 无该值）。
- 不改 `ai switch`。
- 不接管用户自建 mcp-client 条目。

## Decisions

1. **home patch 作为目标文件，而不是 profile patch 或 bundle。**
   home 层在 app-boot 里于所有 profile 层之后应用，`dsh-hmr` watch 它（即使文件尚不存在），且它不受 `sanitizeProfile` 的 fatal-recovery 改名影响。profile 名对 senv 无确定性来源：DSH 不读 `DSH_PROFILE`，Desktop 硬编码 `desktop` 且拒绝 CLI boot/dump；写 home 层不需要猜 profile，一处写入覆盖 Desktop/web/headless/ACP。代价（所有 profile 都加载这些 server）与 user scope 的全局语义一致，写进 ADR 与 skill 文档。

2. **标记块 + 文本级增删，不做整文件 YAML 重序列化。**
   DSH 用 js-yaml `JSON_SCHEMA` + `!!js` 标量；条目顺序有语义（同 id 后写覆盖前写，`config` 整体替换不为深合并）。整文件 parse→dump 会丢注释与标量风格、且重排有语义风险。senv 只重写块内文本；块外零改动。

3. **所有受管 server 共用一个 `- insert: [...]` 行，每个 server 是其中一个条目，id 确定。**
   patch 层新增条目只有 `insert` 可行；loader 会给缺 id 的 insert 项随机 id，所以 senv 固定写 `senv-mcp-<serverName>`，drift/unexport 才能定位。

4. **serverName 归一规则。**
   合规别名原样用（保留 `mcp__<name>__<tool>` 可读性）；不合规按字节替换非法字符为 `-`、trim、截断到 32-7，再追加 6 位 sha256(alias) 后缀，保证确定性与不塌缩。

5. **transport：http → streamable-http，sse plan-error。**
   `createTransport` 只有 stdio / streamable-http 两个分支；把 sse 改写成 http 是静默改写（codex/omo 先例）。

6. **明文导出沿用 ADR-0008。**
   DSH 支持 `!!js process.env.X`，但依赖 DSH 进程启动环境，senv export 不注入；写 `!!js` 会在变量缺失时给出非字符串值。因此写解析后明文（0600 + `.bak` + 计划 `[明文]`），用户手工改 `!!js` 会被判 drift。

7. **空层写 `[]`。**
   DSH 对空/纯注释 patch 文件 boot error；删掉最后一个受管条目后必须保留显式空列表。写前用同一套顶层结构校验（数组 + 每项 mapping）兜底。

8. **冲突 fail-closed。**
   最终文件里受管 id 与 serverName 必须各出现一次；撞上文件内用户条目时拒绝写入（DSH 会让后加载实例失败），而不是写一份运行时会坏的配置。

## Risks / Trade-offs

- **home patch 影响所有 profile**：headless/sdk 也会加载 MCP server。与全局 user scope 一致，但需在文档里写明；若要按 profile 隔离，后续把 `ConfigPath` 换成 profile 路径即可（格式/合并逻辑不变）。
- **上游格式漂移**：文件名、层序、mcp-client Config 字段或 transport 取值变化会让写入失效。锚定 0.2.2-rc.1，ADR/skill 记录验证锚点。
- **跨层 serverName 冲突检测不到**：senv 只校验 home 文件内部；用户在 profile patch/bundle 里手写同名 serverName 由 DSH 运行时对后加载实例报错。
- **明文落盘**：DSH 文档推荐 `!!js` 引用，但 senv 的导出契约是解析后明文；文件 0600，风险与其它 agent 目标一致。

## Migration Plan

纯增量：无存量数据迁移。首次 `mcp export --agent dsh` / `mcp install dsh` 才创建 `~/.dsh/cordis.patch.yml`；回滚 = revert 本 change，已写条目按既有 ledger 语义 `senv mcp unexport --agent dsh` 撤回（或手工删除标记块，DSH 会把它当普通用户 patch 内容保留）。

## Open Questions

无。落点、合并策略、transport/明文取舍均已由调研证据与仓库既有先例确定；如需改为 profile 级目标属后续小改动。
