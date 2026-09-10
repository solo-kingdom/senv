## 1. 勘察与指针存储

- [x] 1.1 核实 claude-code、zcode、kimi 的 provider 配置真实格式（路径/结构/凭据字段），结论回填 design.md 表格；无法核实的 agent 按 D-E 降级 unsupported
- [x] 1.2 实现 `internal/llm/pointer.go`：`~/.config/senv/agent-pointers.json` 读写（0600、JSON schema、损坏时报错）、`Get/Set/List`
- [x] 1.3 指针存储单元测试（tmp HOME：落盘、权限、损坏文件、未初始化读取）

## 2. 适配器与切换管理器

- [x] 2.1 定义 `AgentAdapter`/`SwitchRequest`/`CredentialMode` 与 agent 注册表（含 cursor unsupported 列表）
- [x] 2.2 实现 JSON merge 写回（保留无关键、临时文件+rename、`.senv-bak` 备份、0600）与 TOML 写回（codex，凭据不落盘）
- [x] 2.3 按勘察结论实现各 agent 适配器：claude-code、codex、zcode、kimi、pi（`~/.pi/agent/models.json`）、opencode（`~/.config/opencode/opencode.json`）
- [x] 2.4 实现 `SwitchManager.Switch`：校验 agent/provider/model → 解密凭据（text/env manager）→ adapter.Apply → 指针更新 → 失败回滚
- [x] 2.5 适配器与管理器测试：merge 保留既有键、原子写、备份/回滚、codex 不落密钥、inline agent 权限 0600

## 3. CLI 与收尾

- [x] 3.1 实现 `cmd/ai_switch.go`：`senv ai switch`（vault 解锁流程、错误文案）与 `senv ai status`（无 vault 依赖、cursor/未切换展示、配置路径）
- [x] 3.2 CLI 测试：切换成功全链路、agent/provider/model 校验失败、status 混合状态与无指针文件
- [x] 3.3 `make check` 全绿；`openspec validate agent-provider-switch-agents --strict` 通过；回填 proposal 验证记录
