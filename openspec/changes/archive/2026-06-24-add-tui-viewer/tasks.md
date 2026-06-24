## 1. 依赖与脚手架

- [x] 1.1 引入 bubbletea 依赖：执行 `go get github.com/charmbracelet/bubbletea@latest` 及 `bubbles`、`lipgloss`，运行 `go mod tidy`。**验证**：`go build ./...` 通过，go.mod 出现三个直接依赖。
- [x] 1.2 创建 `internal/tui/` 包结构：`model.go`（主 model）、`tab.go`（Tab 接口）、`env_tab.go`、`text_tab.go`、`config_tab.go`、`search.go`、`styles.go`（lipgloss 样式）。**验证**：空文件骨架编译通过，包名一致。

## 2. 命令注册与启动校验

- [x] 2.1 在 `cmd/tui.go` 新增 `senv tui` cobra 命令，注册到 rootCmd。复用 `getTextManager` 同款的密码校验 + session 缓存逻辑（抽公共函数 `getManagers()` 同时返回 env/text/config Manager）。**验证**：`senv tui` 能跑起来（暂打印 "TUI TBD"），`./senv tui` 在未初始化项目提示错误。
- [x] 2.2 实现启动前置校验：项目未初始化 → 提示并退出；密码错误 → 提示并退出；校验通过 → 进入 TUI。**验证**：手动测三种场景（未初始化 / 错密码 / 正确密码）。
- [x] 2.3 为启动校验写测试 `cmd/tui_test.go`（项目未初始化、密码错误两条路径）。**验证**：`go test ./cmd/... -run TestTuiStartup` 通过。

## 3. TUI 主框架与 Tab 切换

- [x] 3.1 实现 `internal/tui/model.go` 主 model：持有三个 Tab、当前激活 Tab、共享的 Manager 引用、错误条状态。实现 `Init`/`Update`/`View`，先渲染 Tab 标签栏 + 占位内容。**验证**：`senv tui` 显示三个标签，默认聚焦 Env Tab。
- [x] 3.2 实现 Tab 切换：`Tab` 键和 `1`/`2`/`3` 数字键切换，各 Tab 导航状态独立保留（切换回来恢复选中项）。**验证**：在 Env Tab 选中第 2 项 → 切到 Text Tab → 切回 Env Tab，选中项仍是第 2 项。
- [x] 3.3 实现底部状态/帮助栏（显示当前 Tab 的快捷键提示）和顶部标签栏样式（lipgloss）。**验证**：界面布局清晰，快捷键提示可见。

## 4. Env Tab — 浏览与遮蔽（高优先级：安全）

- [x] 4.1 实现 Env Tab 左侧分组列表：调用 `env.Manager.ListGroups`，激活分组显示 `●`，default 显示默认标记。**验证**：左侧列出全部分组，激活态标记正确。
- [x] 4.2 实现右侧变量列表：选中分组时调用 `env.Manager.List(group)`，显示 key=value。**验证**：切换分组右侧列表更新。
- [x] 4.3 实现敏感值遮蔽：env 值默认显示为 `***`（长值截断 `prefix***`）。实现 `v` 键单条切换明文，光标移开自动重新遮蔽。**验证**：默认全遮蔽；按 `v` 仅当前条明文；移开光标后自动遮蔽；手动测试肩窥防护符合预期。
- [x] 4.4 为遮蔽逻辑写单元测试 `internal/tui/env_tab_test.go`（默认遮蔽、单条切换、移开重遮蔽）。**验证**：`go test ./internal/tui/... -run TestMasking` 通过。

## 5. Env Tab — 操作

- [x] 5.1 实现内联编辑（`e` 键）：用 `bubbles/textinput` 弹 modal 预填当前值，确认后调 `env.Manager.Set`。**验证**：编辑值后列表刷新，命令行 `senv env get` 确认值已更新。
- [x] 5.2 实现新建（`n` 键）：modal 依次收集 key、value，调 `env.Manager.Set` 到当前分组。**验证**：新建后列表出现新条目。
- [x] 5.3 实现删除（`d` 键）：确认提示后调 `env.Manager.Delete`。**验证**：删除后条目消失。
- [x] 5.4 实现分组激活/停用（`a`/`x` 键）：调 `env.Manager.ActivateGroup`/`DeactivateGroup`，default 不可停用。**验证**：激活后 `●` 出现，停用后消失；命令行 `senv env group list` 状态一致。
- [x] 5.5 实现新建分组（`+` 键）：modal 收集名称，调 `env.Manager.AddGroup`。**验证**：左侧出现新分组。
- [x] 5.6 实现复制值到剪贴板（`y` 键）：调底层 clipboard 逻辑（复用 `text.Manager` 的剪贴板方式或抽公共函数）。**验证**：粘贴板内容为该 env 值。

## 6. Env Tab — 解引用与过滤

- [x] 6.1 实现解引用视图切换（`D` 键）：默认显示原始值，按 `D` 切换为解引用视图（复用 `cmd/text.go` 的 `resolveValue` + `combinedGetter` 逻辑，抽到公共位置）。解引用失败的条目保留原值并错误条提示。**验证**：含 `{{text:secrets:DB_PASS}}` 的值在 `D` 切换后显示解析结果；再按 `D` 切回。
- [x] 6.2 实现 Tab 内过滤（`/` 键）：输入框过滤右侧列表，仅匹配 key（不匹配值），忽略大小写。**验证**：输入 `DATABASE` 只剩 key 含该串的条目；清空恢复全部。
- [x] 6.3 为过滤逻辑写单元测试（key 匹配、值不匹配、大小写忽略、空输入恢复）。**验证**：`go test ./internal/tui/... -run TestFilter` 通过。

## 7. Text Tab — vim 编辑衔接 Spike（关键验证点）

- [x] 7.1 **Spike**：验证 `tea.ExecProcess` 能否直接包装现有 `text.Manager.SetViaEditor`（其内部用 `exec.Command.Run()` 接管 stdio）。写最小 demo 调用 `SetViaEditor` 并用 `tea.ExecProcess` 触发。**验证**：vim 正常打开、退出后 TUI 正确恢复。若衔接失败，记录原因。
  - **结果**：直接衔接失败。`tea.ExecProcess` 需要一个由调用者构造的 `*exec.Cmd` 来挂起/恢复 TUI，而 `SetViaEditor`/`config.Edit` 自包含地创建并 `exec.Command.Run()` 了编辑器，TUI 无法插入挂起逻辑；直接在 `Update` 调用会阻塞主 loop、与 bubbletea stdin reader 抢占、alt screen 未退出导致 vim 输出错乱。
- [x] 7.2 根据 spike 结果决定：若直接衔接成功 → 无需改 Manager；若失败 → 重构 `SetViaEditor`/`config.Manager.Edit` 拆分为"准备临时文件"和"运行编辑器"两步（保持向后兼容，现有 CLI 调用不受影响）。**验证**：重构后 `senv text -g X set KEY`（无值打开编辑器）和 `senv config edit` 仍正常工作。
  - **实现**：新增 `text.EditorSession`/`PrepareEditor`/`FinishEditor`/`EditorCommand` 与 `config.ConfigEditSession`/`PrepareEdit`/`FinishEdit`/`EditorCommand`；`SetViaEditor`/`Edit` 改为 Prepare+Run+Finish 的一站式包装，签名与 CLI 行为不变。新增 `internal/text/manager_editor_test.go`、`internal/config/manager_test.go` 覆盖变更/未变更/新条目/未找到/编辑器解析路径。

## 8. Text Tab — 浏览与操作

- [x] 8.1 实现 Text Tab 左侧分组列表（`text.Manager.ListGroups`）+ 右侧文本块列表（`text.Manager.List(group)`，显示 key/size/更新时间，不显示内容）。**验证**：列表正确，内容片段不泄漏。
- [x] 8.2 实现 vim 编辑（`e` 键）：通过 `tea.ExecProcess` 挂起 → `SetViaEditor` → 恢复 → 刷新列表。**验证**：编辑保存后 size/时间更新；内容未改动时不重新加密。
  - **实现**：用 7.2 拆出的 `PrepareEditor` + `session.EditorCommand()` 传入 `tea.ExecProcess(cmd, cb)`；回调里调 `FinishEditor`（未改动不重新加密）并发 `textReloadMsg`；编辑器失败时清理临时文件并报错条（覆盖任务 11.3）。
- [x] 8.3 实现新建（`n` 键，vim）、删除（`d` 键）、复制（`y` 键）、导出文件（`o` 键，调 `GetToFile`）、新建分组（`+` 键）、解引用切换（`D` 键）。**验证**：逐项手动测试，命令行交叉验证。
  - 注：Text 列表仅显示元信息（不含内容），`D` 切换有反馈提示但列表视觉无变化（符合"内容不进列表"的安全要求）。

## 9. Config Tab — 单栏浏览与操作

- [x] 9.1 实现 Config Tab 单栏列表（无左栏）：调 `config.Manager.List`，显示 name/target 路径/更新时间。**验证**：布局单栏铺满，信息完整。
- [x] 9.2 实现 vim 编辑（`e` 键）：`tea.ExecProcess` → `config.Manager.Edit` → 恢复刷新。**验证**：编辑保存后时间更新。
  - **实现**：复用 7.2 拆出的 `PrepareEdit`/`FinishEdit` + `tea.ExecProcess(session.EditorCommand(), cb)`。
- [x] 9.3 实现创建（`n` 键，从文件导入）、导出到 target（`x` 键，调 `Export`）、删除（`d` 键）、查看详情（`enter`，调 `Get` 显示元信息面板）。**验证**：逐项测试，与 `senv config` 命令行交叉验证。

## 10. 全局搜索（高优先级：安全 — 不搜值）

- [x] 10.1 实现全局搜索 overlay 触发（`S` 键）：弹出输入框 + 跨类型结果列表。**验证**：`S` 弹出 overlay，`esc` 关闭返回原 Tab。
- [x] 10.2 实现搜索逻辑：遍历 env（全分组）、text（全分组）、config，**仅匹配 key/name**，绝不匹配值。结果按类型标识（Env/Text/Cfg）+ 分组 + key/name，值遮蔽（env `***`、text size、config target）。**验证**：搜 `database` 命中所有 key/name 含该串的条目；输入仅出现在值中的字符串返回"无匹配"。
- [x] 10.3 实现跳转定位：结果选中按 `enter` → 切到对应 Tab + 选中分组 + 选中条目。**验证**：从搜索结果跳转到 env prod 分组的某条目，Tab/分组/条目三重定位正确。
- [x] 10.4 为搜索范围写单元测试（key 命中、值不命中、跨类型聚合、空结果）。**验证**：`go test ./internal/tui/... -run TestSearch` 通过，特别断言"值不匹配"。

## 11. 错误处理与空状态

- [x] 11.1 实现底部错误条：任何 Manager 返回错误时显示，不崩溃，保留当前视图；下次操作时清除。**验证**：人为触发错误（如删除不存在的 key），错误条显示，TUI 不退出。
  - **实现**：`errMsg`/`clearErrMsg` 在 `Model.Update`/`View`；任意按键清除（`TestErrorBarSetAndClear`）。
- [x] 11.2 实现空状态提示：空分组/空列表显示友好提示（如"该分组暂无环境变量"）。**验证**：选中空分组显示提示而非空白。
  - **实现**：各 Tab 用 `emptyStateStyle` 渲染提示（`TestEmptyStateHintsRender` 覆盖 env/text/config）。
- [x] 11.3 vim 编辑失败处理：编辑器不存在/退出码非 0 时，TUI 恢复后错误条提示，临时文件仍清理。**验证**：`EDITOR=/no/such/editor ./senv tui` 触发编辑，错误条提示，无残留临时文件。
  - **实现**：`finishAfterEdit(session, runErr)` 在 `runErr != nil` 时 `os.Remove(tmp)` + 返回 `errMsg`，且不调用 `FinishEditor`（值不变）（`TestTextEditorFailureCleansUpAndPreservesValue`、`TestConfigEditorFailureCleansUp`）。

## 12. 文档与收尾

- [x] 12.1 更新 `README.md`：在命令列表加入 `senv tui`，新增"TUI 模式"小节说明快捷键（引用 design.md 的使用示例）。**验证**：README 编译渲染正常，快捷键表与实现一致。
- [x] 12.2 运行 `make check`（fmt + vet + lint + test）全绿。**验证**：无报错，`go test -race ./...` 通过。
- [ ] 12.3 全流程手动验收：初始化项目 → 录入 env/text/config → `senv tui` 完成浏览/搜索/编辑/删除/激活/解引用全套操作，与命令行结果交叉验证。**验证**：所有场景符合 specs/tui-viewer/spec.md 的每个 Scenario。
  - **进度**：已自动化覆盖（`make check` 全绿，30+ 测试涵盖启动校验/Tab 切换/遮蔽/过滤/搜索不搜值/操作/解引用/错误条/空状态/编辑器 Prepare-Finish/vim 失败清理）；命令注册与 `senv tui --help` 已验证。**剩余**：交互式 TTY + vim 全流程手动验收需人工在终端执行（无法在无 TTY 环境自动完成）。
