# Design: tui-ux-list

## Context

ADR 候选 `adr-self-built-list-component`（driver design.md D1/D8）：自研薄层，不引入 `bubbles/list`。本 change 只建核心（游标+窗口+几何），filter/多选/侧栏由 ④⑤⑥ 在组件上叠加。

## Goals / Non-Goals

**Goals:** 一个可被全部 Tab 复用的列表状态与渲染组件；两个试点 Tab 迁移后行为逐像素等价。

**Non-Goals:** 不含选择集与过滤状态机；不迁移其余 Tab；不改按键语义。

## Decisions

- **组件只持有视图状态，不持有业务数据**：

  ```go
  type List struct {
      cursor int // 游标
      height int // 可视行预算
      // ④ 叠加 filter 词；⑤ 叠加 selection map（本 change 预留字段位，不实现）
  }
  func (l *List) Move(delta int)            // j/k，clamp 到 [0, n)
  func (l *List) Page(dir int)              // PgUp/PgDn，整页移动
  func (l *List) Home() / End()             // g/G
  func (l *List) VisibleRange(n int) (lo, hi int) // 跟随式窗口
  ```

  行渲染仍由 Tab 提供（`[]string` 或回调），组件只算「可见哪些行、游标在哪」。备选「组件托管条目接口 `Item interface`」被否：8 个 Tab 行结构差异大，接口化会逼出过度抽象。

- **history 居中窗口语义归入 `VisibleRange`**：现 `history_tab.go` 的 `visibleRows` 把游标居中，audit 是顶端跟随。组件实现跟随式（与 `windowedPane` 一致）为准，history 迁移后从居中变跟随——这是唯一可见差异，属可接受统一（两窗口内容集合相同，仅停留位置不同），在 tasks 验证里显式核对。
- **双栏几何收敛**：各 Tab `viewBaseAt` 里 `width/4`、`width*11/20` 等散算提取为 `paneBudgets(width int) (left, right int)`，各 Tab 传入自己的比例偏好，计算逻辑唯一。
- **helper 收敛以搬家为主不改名**：`modalBox`/`cursorLine` 等直接移入共享文件，调用点改引用，避免翻新命名引入无谓 diff。

## 数据流与错误处理

纯视图层重构；组件不产生错误路径（入参 clamp 化，无 error 返回）。数据装载、快照、审计语义零变化。

## Risks / Trade-offs

- [逐像素等价难验证] → 迁移前后对同一状态（同尺寸/同数据/同游标）截屏 diff；audit/history 各 3 组状态
- [组件 API 被 ④⑤⑥ 逼着改] → 本 change 只冻结最小面（Move/Page/Home/End/VisibleRange），filter/选择集字段留待对应子 change 评审时加入

## Open Questions

无。
