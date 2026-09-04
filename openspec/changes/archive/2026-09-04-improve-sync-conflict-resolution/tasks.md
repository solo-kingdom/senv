## 1. Server 冲突描述符

- [x] 1.1 扩展 server `Entry` / `Conflict` wire 与 store 查询，使 pull 返回 `updated_at`、409 返回 `current_revision`、`deleted`、`size` 与 `updated_at`。验证：新增 store/handler 测试断言字段值，且 409 JSON 不含 ciphertext。
- [x] 1.2 保持 v1 API 向后兼容。验证：用旧 client JSON fixture 解码/忽略新增字段，现有 push/pull/metadata handler 测试全部通过。

## 2. Provider 冲突候选模型

- [x] 2.1 在 pull 结果中保留因本地 dirty 而跳过的远端候选 entry，不落盘、不改变现有 sync state。验证：构造两端修改冲突，断言本地文件未被覆盖且可取得远端候选 revision/hash/ciphertext。
- [x] 2.2 将本地 dirty 候选、远端候选、409 描述符与 metadata 诊断合并为结构化 conflict detail。验证：表驱动测试覆盖普通条目、删除、metadata 冲突与旧 server 零值字段。
- [x] 2.3 当 409 revision 与保留候选不一致时，用全量 pull 刷新远端候选。验证：模拟 pull 后远端再次更新，断言刷新候选且不应用旧 plan。

## 3. 非交互 CLI 报告

- [x] 3.1 为 `senv sync` 增加 `--no-interactive`，并保持 `--accept-remote` / `--force-push` 优先级与现有语义不变。验证：CLI 测试覆盖互斥策略、非 TTY 默认不交互、旧帮助示例仍可用。
- [x] 3.2 升级非交互冲突输出为安全摘要表，包含 identity、本地 base revision、远端 revision、删除状态、size、hash 与可用时间。验证：捕获 stdout/stderr 断言字段与解决指引，且输出无明文/密文。

## 4. 解密与类型化对比

- [x] 4.1 【高优先级】实现冲突专用按需认证：优先复用 session/auth memo，必要时 TTY 提示 vault 口令，失败保持脱敏。验证：测试有效 key、无 key 非 TTY、错误口令与 key 不兼容路径，确认不写日志。
- [x] 4.2 实现 `env` / `text` / `config` / `config_index` / `env_meta` / metadata 的安全摘要与明文渲染，env 默认掩码。验证：表驱动测试断言各类型字段、二进制降级、掩码与显式揭示。
- [x] 4.3 实现 `config_index` 语义摘要（added/deleted/target/group/description）。验证：用本地/远端 index fixture 断言差异文本与排序，不泄露无关条目明文。

## 5. 交互式冲突解决器

- [x] 5.1 创建 Bubbletea 冲突列表模型与键盘导航（j/k、Enter、q、?），仅在 TTY 且无显式策略时启动。验证：模型测试覆盖导航、帮助、退出和 `--no-interactive` 短路。
- [x] 5.2 添加明细视图与 `v` 显式揭示/掩码，按类型调用渲染器。验证：测试 keybinding、明文不默认出现、解密失败显示安全警告。
- [x] 5.3 添加本地/远端逐条选择与未处理项批量 `L/R` 默认策略，生成覆盖计划。验证：模型测试断言 plan 覆盖所有冲突与 metadata，确认前不调用写操作。
- [x] 5.4 添加最终确认页与应用结果输出。验证：模拟确认/取消/远端变化，断言用户必须显式确认且成功/失败文案准确。

## 6. Provider Resolution Plan

- [x] 6.1 【高优先级】实现 provider 级 resolution plan 与同步锁内预检：本地 hash、远端 revision、候选存在性、metadata 决策完整性。验证：测试本地变化、远端变化、缺失候选均拒绝应用且两端不被部分覆盖。
- [x] 6.2 实现本地 cache transaction 写入选定 remote/local/merged 条目，并批量推送 local/merged 决策。验证：fake server 测试断言 base_revision、状态更新顺序和本地写入原子性。
- [x] 6.3 实现网络失败后的本地 pending 恢复与再次 409 重入冲突。验证：模拟 push 网络失败与远端新变更，断言不回滚已确认合并、不静默覆盖远端。
- [x] 6.4 【高优先级】实现 metadata key 兼容门禁：不兼容时禁用逐条 merge，仅允许既有整体策略。验证：构造远端 passwordKey 不兼容，断言 merge 入口拒绝且错误不含密钥材料。

## 7. Editor 手动合并

- [x] 7.1 【高优先级】实现一次性 0700 merge 目录、0600 缓冲文件、editor 启动与递归清理。验证：测试 editor 成功、失败、校验失败和清理失败路径，断断明文不进入日志。
- [x] 7.2 生成 LOCAL/REMOTE 两方合并缓冲区，支持 `text`、UTF-8 `config`、`env` 与 pretty `config_index`。验证：fixture 测试断言 marker、UTF-8 内容、二进制/删除/env_meta/metadata 被拒绝。
- [x] 7.3 实现读回校验：未解决 marker、UTF-8、大小限制、条目 schema、`config_index` normalize。验证：表驱动测试覆盖合法合并、残留 marker、非法 JSON、超限与非 UTF-8。
- [x] 7.4 将合法 merged plaintext 加密为候选并加入 resolution plan，更新类型内业务时间且保留创建时间。验证：测试 env/text/config/index 合并后的密文可解密、元数据正确、等价内容识别正确。
- [x] 7.5 在 Bubbletea 中通过 `tea.ExecProcess` 打开 editor 并恢复冲突 UI。验证：模型测试断言 editor 命令使用 `VISUAL`/`EDITOR`、失败清理、取消不改变 plan。

## 8. 文档与总体验收

- [x] 8.1 更新 `docs/senv-server.md` 与 README/示例，说明交互快捷键、安全边界、editor merge、`--no-interactive` 与旧 server 兼容。验证：文档命令可复制执行，无明文示例泄漏。
- [x] 8.2 运行完整回归与安全审查。验证：`make test` 通过；重点审查 conflict 409、auth、临时文件、日志与 provider 竞态测试；`make check` 无新增问题。
