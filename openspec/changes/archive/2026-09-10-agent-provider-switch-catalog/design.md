## Context

driver 已定切片：本 change 是第一个子 change，只做模型目录，不碰 vault 与 provider 档案。models.dev `api.json` 结构为 `{providerId: {id, name, api, env, doc, models: {modelId: {id, name, description, reasoning, tool_call, ...}}}}`（已实测）。缓存是机器本地公开数据，放 config 目录（`getConfigPath()` 下的 `cache/`），不放 vault data 目录；命令不需要解锁 vault。

## Goals / Non-Goals

**Goals:** 一次 refresh 拉取+校验+原子落盘；离线可读缓存元信息；为后续 store 子 change 提供可编程的缓存读取入口
**Non-Goals:** 定时/自动刷新；provider 档案存储；模型字段裁剪或转换

## Decisions

1. **新增 `internal/llm` 包**：`Catalog` envelope（`{version: 1, fetched_at, source, providers}`）+ `Fetch`（30s 超时）+ `Parse/Validate`（≥1 provider、≥1 model、provider id 非空）+ `Save/Load`。后续 store 子 change 复用 Load 做新鲜度检查。
2. **透传原始 providers JSON，不做字段裁剪**：envelope 只加元信息，`providers` 保持上游原样。备选的「只保留 id/name/api/models 白名单」会在后续 slice 需要新字段时改格式，得不偿失。
3. **缓存布局**：`~/.config/senv/cache/models-dev.json`，目录 0700 / 文件 0600（沿用仓库惯例），写入为 temp+rename 原子替换，失败不触碰旧文件。
4. **CLI 形态**：`senv ai` 根命令仅作分组（无 vault 依赖）；`senv ai refresh [--source URL]`；`senv ai catalog status`。使用示例：
   ```bash
   senv ai refresh
   senv ai refresh --source https://mirror.example/api.json
   senv ai catalog status
   ```
5. **`--source` 保留**：便于测试注入与自建镜像；目录是无凭据公开数据，任意 URL 无敏感泄露面。

## 数据流

```
refresh: source URL ─▶ GET(30s) ─▶ Parse/Validate ─▶ envelope(version/fetched_at) ─▶ temp+rename 原子写 ─▶ 缓存
status:  缓存 ─▶ 读 envelope ─▶ 渲染元信息（无网络）
```

## 错误处理策略

- 网络失败/超时：非 0 退出，stderr 明确报错，旧缓存不动
- 内容非法：同上，报「目录内容非法」并保留旧缓存
- status 无缓存：非 0 退出，提示 `senv ai refresh`
- status 缓存损坏（version 不识别/JSON 损坏）：报损坏并建议 refresh，不做静默降级

## Risks / Trade-offs

- [透传原始 JSON 缓存体积偏大（MB 级）] → 本地磁盘可接受，换取后续 slice 免改格式
- [上游目录结构变化] → envelope version + 入口校验兜底，解析失败保留旧缓存
- [仓库无 httpmock 依赖] → `Fetch` 接受 `*http.Client`/URL 注入，用 `httptest.Server` 测试
