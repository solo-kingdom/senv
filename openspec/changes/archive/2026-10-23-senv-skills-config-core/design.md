## Context

冻结方案见台账 `~/code/tasks/ongoing/senv-skills-config/design/`。现状：同步白名单在 `internal/syncschema`；server 与 client 共用 `ValidateIdentity`；收集层按 kind 硬编码路径；rekey 对 data 树里任何 `.enc` 都要求可分类，否则失败关闭；`MaxEntryCiphertext=512KiB` 全 kind 通用。

## Decisions

1. **身份**：`skill` 要求 grp+key（与 backup 同形），`skill_index` 要求两者皆空（与 config_index 同形）。
2. **存储布局**：`skills/<group>/<id>.enc`；`skill_index.json` 放数据目录顶层（随 data/git 走）。顶层收集循环里该文件名先于 `.enc` 判断，避免被当成 config。
3. **索引**：`{"skills": {id: {id, group, description, encrypted_file, created_at, updated_at}}}`。`encrypted_file` 必须等于规范路径，空 group 视为 default；读取时任何身份异常整体失败关闭，不静默丢条目。id 由 map key 保证全局唯一。
4. **归档**：`Pack` 以源目录内容为 tar 根，条目按路径排序，uid/gid=0、mtime=epoch，普通文件权限归一为 0644/0755，目录 0755，输出确定。ustar 对路径有限制（ASCII、prefix+name ≤255），超限给出明确错误。`Inspect` 同时服务入库与后续 apply：白名单类型、拒绝 pax/GNU 头、路径校验、重复与文件/目录冲突检测、SKILL.md 必须在根。
5. **frontmatter**：按行扫描顶层 `name:`，不用完整 YAML——真实 skill 的 description 常含未加引号的冒号，会让 YAML 解析失败。不支持块标量写法。
6. **大小**：明文 tar ≤1 MiB（`storage.MaxSkillPlaintext`），写入前与解密后各检查一次；1 MiB 明文加密后（AEAD+base64）≈1.4 MiB，落在 server 1.5 MiB 上限内（有测试）。
7. **server**：`maxCiphertextFor(kind)` 仅对 `skill` 返回 1.5 MiB。
8. **rekey**：新增 `rekeyEntrySkill` / manifest kind `skill`；`classifyRekeyEntry` 接受 `skills/<group>/<id>.enc`。索引是明文，不参与 rekey。
9. **doctor/orphan**：`CheckConsistency` 枚举 `skills/` 磁盘文件（不依赖 index）；`HasOrphanedData` 对 `skills/` 下任何 `.enc` 报 orphan。
10. **CLI**：`add` 默认拒绝同 id（`--force` 整条替换，保留 CreatedAt；未给 `--group` 时沿用原分组）；`show` 只列文件清单，不打印内容；审计 target 只含 kind/group/id。
11. **冲突/历史**：skill 正文冲突与历史只显示元数据/大小；skill_index 按明文 JSON 处理。

## Risks / Trade-offs

- 旧 server 拒未知 kind：需先升级 server 再 push（文档片说明）。
- 非 ASCII 文件名无法写入 ustar：入库时报错，不做静默改名。
- 多机各自改索引会产生 `skill_index` 冲突，沿用 sync 的整体选边策略，本片不做语义合并。

## Migration Plan

纯新增；无旧数据迁移。回滚：删除 skill 代码与已同步条目。
