## Context

`senv tui` 是 bubbletea 全屏多 Tab 架构：`tui.Managers` 按类型注入管理器，nil 即不注册对应 Tab（History/Audit 先例）；sshTab 提供双栏浏览的现成范式（左列表右详情、focus 切换、filter）。AI Tab 是首个「浏览 + 写操作」组合 Tab，但写路径完全复用 agents 子 change 的 SwitchManager（原子写、指针、回滚已在 `internal/llm` 测试覆盖），Tab 自身只做选择交互与结果展示。

## Goals / Non-goals

**Goals:**

- 零新概念：键位、焦点、错误横幅全部沿用既有 Tab 交互语言
- 切换后指针区即时刷新，与 CLI `senv ai status` 语义一致
- 凭据明文只在 SwitchManager 内部流动

**Non-goals:**

- provider 档案编辑（CLI 职责）
- MCP 查询（mcp 子 change）

## Decisions

### D-A Managers 注入扩展

```go
type Managers struct {
    ...
    LLM        *llm.ProviderManager
    LLMPointer string            // 指针文件路径（测试注入）
    LLMHome    string            // agent 配置根（测试注入）
}
```

`cmd/tui.go` 在既有认证流程后构造：`llm.NewSwitchManager(mgr, LLMPointer, LLMHome)`。两个输入为 string 而非 SwitchManager 指针，避免 tui 包反向依赖时序问题并便于测试。

### D-B aiTab 结构

沿用 sshTab 双栏骨架：左栏 provider list（bubbles list），右栏上部为详情（base_url/模型集/凭据引用），右栏下部为指针区（StatusRow 渲染）。`s` 键进入三段选择流：agent → model → 确认（`y/n`）。选择流实现为 tab 内轻量状态机（selectAgent/selectModel/confirm），不新建顶层 modal 组件。

### D-C 切换执行

Switch 调用包在 `tea.Cmd` 中异步执行，结果消息携带 `SwitchOutput` 或 error：成功 → 重载指针 + 状态栏提示（codex 场景附加 env 变量名）；失败 → 既有错误横幅。TUI 不直接改配置文件，一切写路径走 SwitchManager。

### D-D 凭据安全

Tab 状态只保存 `LLMProviderEntry`（凭据引用非明文）与 `StatusRow`；SwitchOutput 不含凭据明文（agents 子 change 已保证 CredentialEnv 只是变量名）。渲染函数禁止输出 entry 之外的 vault 数据。

## 数据流

```
senv tui (解锁)
  → Managers.LLM = ProviderManager
  → aiTab.load: ListProviders() + Status()
  → [s] SwitchManager.Switch(agent, provider, model)  // 异步 tea.Cmd
      成功 → 指针重载 + 提示（codex 附加 env 名）
      失败 → 错误横幅
```

## 错误处理策略

- load 失败（vault 异常）：Tab 内错误横幅，不 panic
- 切换失败：横幅展示原因；指针区数据不变

## Risks / Trade-offs

- Tab 内写操作是 TUI 首例 → 复用已测 SwitchManager，Tab 测试覆盖成功/失败路径
- 模型集可达 10000 条 → 选择器用既有 list 虚拟化（ssh/env Tab 同款），不一次性渲染全量

## Migration Plan

纯新增 Tab；Managers 新字段零值安全（nil 跳过）。

## Open Questions

（无）
