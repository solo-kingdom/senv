# ADR-0038: Skill 多版本快照与切换/回滚（Skill Versioning）

`senv skill` 此前每个 skill 仅保存单份无版本密文 `skills/<group>/<id>.enc`，`add --force` 原地覆盖归档，本地 `apply` 仅写入空 `.senv-owned` 文件，导致无法保留历史版本、无法回滚或按指定版本安装，也无法直接识别本地已安装的版本号。本 ADR 决定为 `senv skill` 引入多版本快照、切换/回滚与本地版本追踪能力。

## 决策

1. **双路径密文布局（保留活跃版 + 不可变快照）**
   - 当前活跃版继续写入 `skills/<group>/<id>.enc`（`EncryptedFile` 路径与 AAD `(KindSkill, group, id)` 不变）。
   - 每个版本快照写入 `skills/<group>/<id>@<version>.enc`（不可变快照，AAD `(KindSkill, group, id+"@"+version)`）。
   - `@` 属于 `securefs.ValidateSegment` 允许字符，可直接复用现有 `syncschema.KindSkill` 3 段路径、`senv-server` 1.5 MiB 密文上限、`server_state` 同步、`rekey`、`consistency`/`doctor` 探测，**服务端与同步协议零改动**。
   - **兼容边界**：旧版 v0.1.75 客户端仍可正常同步、只读 list/show/apply 当前活跃版 `<id>.enc`、执行 `doctor` 与 `rekey`；仅当旧版客户端写回 `skill_index.json` 时会剥离新增的 `current_version` / `versions` 字段。
   - `ValidateSkillID` 禁止在 skill id 中使用 `@`；`listSkillFilesFromRoot` 改用 `ValidateSkillStorageKey` 同时接受 `<id>` 与 `<id>@<version>`。

2. **索引扩展与 fail-closed 校验**
   - `SkillEntry` 新增 `current_version`（字符串）与 `versions`（`[]SkillVersion`，含 `version`、`sha256`、`size`、`note`、`created_at`）。
   - `normalizeSkillIndex` 强校验：`versions` 非空时 `current_version` 必非空且命中其中一项、版本号合法且不重复；`versions` 为空时 `current_version` 必为空（存量未版本化条目合法）。

3. **版本号解析、发布与存量升级保护**
   - `ParseVersion` 支持从 `SKILL.md` YAML frontmatter 顶层 `version:` 或 `metadata.version:` 提取版本；优先级为 `--version` > frontmatter > 自动策略（新建默认 `v1`，`--force` 内容变化递增 `v<N>`）。
   - 已有 skill 同组传入显式新版本时无需 `--force` 即可发布并激活；未传版本且无 `--force` 仍报错 `already exists` 防止误操作。
   - 存量无版本 skill 首次版本化升级时，自动把旧 `<id>.enc` 留存为不可变快照（优先取旧 frontmatter 版本，缺省 `v0`），不丢失存量内容。

4. **本地 `.senv-owned` 版本元数据**
   - 带版本 `apply` 时在 `.senv-owned` 写入 JSON（`id`、`version`、`sha256`、`applied_at`）；无版本或低层 `Apply` 保持空文件，读取侧对空文件视为已托管但版本未知。
   - `PlanWithOptions` 据此区分同版本本地漂移与版本升级/切换（含内容相同但版本号变更）。

5. **CLI 与 TUI 访问面**
   - CLI：`skill add --version/--note`、`skill list` 展示 `@<ver>`、`skill show <id>[@<ver>]`、`skill apply <id>[@<ver>]`、`skill version list|use|remove`。
   - TUI Skill Tab：中栏展示 `@<ver>`、右栏展示已装版本与漂移方向、详情展示历史版本、新增 `v` 键版本弹层支持切换活跃版 / 按版本 apply / 删除非活跃版。

## Status

accepted（2026-11-26）
