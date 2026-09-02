## 1. 迁移核心

- [x] 1.1 密文条目枚举与双向搬运（metadata + env/text/config，幂等写入）
- [x] 1.2 目标非空检测与显式覆盖确认

## 2. CLI

- [x] 2.1 `senv migrate to-server` / `from-server` 命令与进度、计数输出
- [x] 2.2 失败条目报告与重试语义

## 3. 验证

- [x] 3.1 双向迁移集成测试（含中断重试、非空目标保护、口令不变验证）
- [x] 3.2 `make check` 全绿；`openspec validate --strict --type change senv-server-provider-migrate` 通过
