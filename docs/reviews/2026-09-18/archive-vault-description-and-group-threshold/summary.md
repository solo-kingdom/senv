# Summary: archive-vault-description-and-group-threshold

**Commit:** `c22de23` on `chore/archive-vault-description-and-group-threshold` (already pushed)
**Scope:** 1 commit, 97 files, +2127 / -300
**Review method:** ⚠️ OCR CLI 与 MiniMax 工具调用不兼容（file_read 反向行范围死循环），回退为直接调用 MiniMax-M3 对核心 9 个生产文件做结构化 review，findings 经人工 grep 复核后定级。
**Output:** `review.md`（完整版）/ `ocr-raw.json`（原始 LLM JSON + 复核注记）

## P0 — 无

## P1（应尽快修；2 条）
1. **`internal/env/manager.go` `GetWithMeta`**：非 `os.IsNotExist` 错误被 `loadEnvGroup` fallback 吞掉，可能用旧格式数据掩盖解密/解析失败。
2. **`internal/storage/manager.go` `SaveEnvGroupWithKey`**：用 group 级 `CreatedAt/UpdatedAt` 覆盖每条 entry 的真实时间戳，破坏审计保真度。

## P2（5 条）
- `text.EnsureGroup` TOCTOU（检查与创建不同锁）
- `text.AddGroup` 失败留下无 `.meta.enc` 目录
- `storage.Initialize` 顺序失败无 rollback
- `storage.Initialize` 每次覆盖 `llm-keys` 描述（设计应改为 Ensure 风格）
- `env.SaveEnvGroupMetaWithKey` 失败留下无 meta 目录
- `env.RenameGroup` 丢失 legacy group 的 description

## P3（1 条）
- `llm.ensureLLMKeysGroup` 在 password-mode 每次重复 PBKDF2

## 已过滤（人工复核后失实 / 降级）
- F8 KeyPair 缺 description 校验：失实（Import:120 / Edit:347 都有）
- F9 `entry.ValidateLLMProvider` 缺 description 校验：误报（Save 路径已校验）
- F0/F2 PBKDF2 重复派生：降级（`m.key` 缓存下 O(1)）

**建议先做 P1 两条再发版；P2 可集中一提交修。**
