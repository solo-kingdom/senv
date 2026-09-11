## 1. 准备

- [x] 1.1 把涉及面里角色为必须的仓切到任务分支（工作树存在无关未提交改动时按 Driver 协议列出路径并确认；用户确认继续：CONTEXT.md + 5 个 LLM 相关文件随 switch 带到 `tui-perf` 分支）

## 2. 实施

- [x] 2.1 完成子 change `tui-perf-log`：apply 至全部 checkbox 勾选且 `validate --strict` 通过
- [x] 2.2 完成子 change `tui-perf-net`：apply 至全部 checkbox 勾选且 `validate --strict` 通过
- [x] 2.3 完成子 change `tui-perf-load`：apply 至全部 checkbox 勾选且 `validate --strict` 通过

## 3. 收尾

- [x] 3.1 全仓回归与静态检查（`make check`），命令与结果写入 proposal 验证记录
- [x] 3.2 回填 proposal 验收标准（以 `tui-perf-log` 埋点回测 D8 数字）
- [x] 3.3 提交交付仓改动（e7631ae；工作树中另一 WIP（llm/ai_provider 等）未纳入，ai_tab.go/ai_tab_test.go 以「HEAD+本任务改动」入提交、混合版留工作区）
- [x] 3.4 归档全部子 change（先子后 driver）
