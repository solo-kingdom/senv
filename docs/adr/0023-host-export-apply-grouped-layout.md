# host export 应用化：分组布局 + 自动落盘 + Include 注册

`senv host export` 从「纯渲染到 stdout、接线全靠手工」（README 教 `>> ~/.ssh/config.d/senv` + 手写 `Include`）改为默认**应用模式**：按分组整树维护 `~/.ssh/senv/`（`groups/<组>.conf` 组片段 + `keys/<组>/<名>` 落盘私钥，未分组入 `_ungrouped`），渲染后自动落盘被引用密钥（已存在跳过；keypair 不在本机 vault 只 warning 不阻断，ADR-0020 D4 不变），并幂等注册 `~/.ssh/config` 顶部一行 `Include ~/.ssh/senv/groups/*.conf`（OpenSSH 7.3+ 支持 glob）。`--output -` 保留纯渲染预览。落盘布局取代 ADR-0001 的扁平约定；「留在 `~/.ssh/` 下贴近 OpenSSH 权限模型」的理由不变。

## 决策

1. **默认形态**：应用为默认，`--output -` 显式回退纯渲染。对依赖 stdout 的既有脚本是有意破坏——个人 CLI 的脚本面由用户自担。
2. **按组导出**：apply 模式以组片段为写入单元：`--group prod` 只重建 `prod.conf`（整文件从 vault 重渲染，不回读合并，同 ADR-0007 单向模型）；`--host web` 重建 web 所在组整文件；无过滤 = 全量重建。stdout 模式保持过滤渲染预览。
3. **跨组 ProxyJump**：jump 目标在组外是合法配置，闭包自动并入**发起导出的同一 fragment**；目标所属组另有片段时，跨文件同别名块无害（OpenSSH 首匹配获胜，内容同源自 vault）。
4. **自动落盘**：被引用 keypair 的落盘文件缺失即补写，存在即跳过（不静默覆盖，与 `materialize --force` 语义一致）。
5. **Include 注册**：单行 glob 置顶；缺失才追加，用户其余内容一字不动；文件不存在则建 0600；写入事务留 `~/.ssh/config.senv-bak`（ADR-0012 同款）。置顶理由：重复参数首匹配，senv 是事实源应在先；同时压过 README 旧教程留下的手写 `Include ~/.ssh/config.d/senv`（旧行不代删，文档提示）。一组一行 Include 的方案无收益（组增删要改写多行）。
6. **陈旧产物**：senv 完全拥有的 `groups/*.conf` 在组于 vault 中消失（改名/删空）时由 export 自动清理，否则 glob 继续提供幽灵别名；落盘私钥**只警告**（export 列出未引用文件）+ 显式 `senv keypair prune` 清理——私钥可能被你手写 ssh config（senv 不可见）引用，不自动删。
7. **撤回**：`senv host unexport` 幂等移除注册行并删除组片段；不碰 vault 档案、不删落盘私钥（与 MCP 撤回同构）。
8. **分组名校验**：host/keypair 写入与编辑时禁止 `/`（进文件系统布局）；真实组名撞 `_ungrouped` 在导出时报错而非静默混合。

## Considered Options

- **片段单一文件 vs 一组一文件**：单一文件下「按组导出」要么过滤掉其他组条目（破坏），要么合并渲染（`--group` 退化为预览参数），与诉求拧；组文件使组增删对 `~/.ssh/config` 零扰动（glob 行不变）。
- **扁平 key 名加组前缀 vs 分组目录**：前者组改名即路径变且不可读，无收益。
- **自动落盘默认 vs `--with-keys` 显式**：诉求是「导出后直接可用」；纯渲染由 `--output -`、TUI 预览与 MCP 只读路径承接，副作用默认化不伤及它们。
- **keypair 不在 vault 严格失败 vs warning**：违背 ADR-0020 D4 定案（悬空引用不阻断导出），维持 warning。

## Consequences

- **导出须解密被引用 keypair** 取分组（`IdentityFile` 路径依赖之），放弃 `internal/ssh/host.go` 只列名单的优化；vault 已解锁，成本忽略。
- **升级残留**：老机器的扁平落盘文件（`~/.ssh/senv/<名>`）与手写 `config.d/senv` Include 不迁移、不删除；前者落入「未引用警告」覆盖，后者被顶部 glob 压过，文档提示自行清理。
- **TUI 最小对齐**：`m` 落盘与导出预览自动跟随 manager 新路径，不加新交互；MCP 维持只读。
- **OpenSSH ≥ 7.3 依赖**：glob Include 需要之（2016 年起）；更老版本注册行不生效，导出时输出警告。
- **审计**：`export` / `unexport` / `prune` 均记入操作审计（含落盘与清理的条目名清单）。

## Status

accepted（2026-09-14 随 ssh-host-export-apply 实现落地）
