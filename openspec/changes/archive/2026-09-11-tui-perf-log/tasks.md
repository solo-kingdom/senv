## 1. 耗时日志核心

- [x] 1.1 新增耗时日志包：slog JSON sink、进程内单例 writer（句柄复用、追加写、0600）、`Stage`/`Note` API、阈值与 `SENV_PERF`/`SENV_PERF_THRESHOLD` 解析（非法回退默认）、关闭时 no-op、写失败静默降级
- [x] 1.2 单元测试：阈值过滤、开关零写放大、非法阈值回退、写失败不报错、日志不含传入值参数

## 2. 埋点接线

- [x] 2.1 启动阶段埋点：TUI 认证（含会话缓存命中）、manager/provider 构造、各 Tab 首次装载；CLI 命令主路径
- [x] 2.2 vault 全量加载埋点：env/text/host/config 各域，附组数/条目数维度
- [x] 2.3 网络同步埋点：auto pull / auto push / History 查询，附结果与 `conns_new` 维度（连接计数先置 0，由 `tui-perf-net` 回填）
- [x] 2.4 同步扫描埋点：`LocalSyncSnapshot`/collect，附扫描条目数

## 3. 收尾

- [x] 3.1 TUI 全屏下零 stderr 输出验证；`senv env list` 实测 `perf.log` 产出符合 spec 场景
- [x] 3.2 同步 `.agents/skills/senv-cli/SKILL.md`：perf.log 位置与 `SENV_PERF*` 环境变量；`go run . --help` 语法验证
- [x] 3.3 `openspec validate --strict --type change tui-perf-log` 通过，全量 make check 通过
