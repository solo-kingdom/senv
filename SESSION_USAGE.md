# Session Cache 功能使用指南

## 概述

Session Cache 功能允许你在一段时间内缓存会话密钥，避免每次运行 `senv` 命令都输入密码。

会话按 **vault 分槽**：每个 data path 一份缓存，切换 vault 不会覆盖另一个 vault 的会话。业务命令在复用缓存 key 时会**滑动续期**，但不会超过 `session.max_lifetime` 这个绝对上限。

## 安全性

- ✅ **只缓存派生密钥**，不缓存原始密码
- ✅ 缓存文件权限为 `0600`（仅所有者可读写）
- ✅ 使用 UID 隔离不同用户的缓存
- ✅ **按 vault 分槽**，一个 vault 的会话不会被另一个 vault 覆盖
- ✅ 包含数据路径哈希验证，防止缓存误用
- ✅ 完整的审计日志记录（含 `session_expire` / `session_invalidated` / `session_unverifiable`）
- ✅ 支持灵活的过期策略与滑动续期上限
- ✅ 无法判定是否可用时**保留缓存**，不擅自删除可能是唯一解密钥匙的残留

## 使用方法

### 1. 查看会话状态

```bash
senv session status
```

状态有四态，并会给出原因与下一步：

| 状态 | 含义 | 下一步 |
|------|------|--------|
| `Active` | 可用 | 直接使用；业务命令会滑动续期，同时显示距绝对上限（`session cap`）的剩余时间 |
| `Expired` | duration 到期 | `senv session start` |
| `Invalidated` | 重启后 `restart` 会话失效，或缓存属于别的 vault | 重启：`senv session start`；属于别的 vault：`senv session clear --all` 后再 `senv session start` |
| `Unverifiable` | boot ID 读不到、缓存损坏 | 排查原因后重试；确要丢弃用 `senv session clear --all` |

**只有 `Expired` 会被自动清理。** `Invalidated` 与 `Unverifiable` 的缓存一律保留，因为它们可能是另一个 vault 的唯一恢复钥匙；需要丢弃时显式执行 `senv session clear` / `--all`。

同一 vault 槽位若同时存在平台安全存储与磁盘逃生舱两份缓存，系统按 `created_at` **新者优先**完成校验并复用，保留另一份；只有两份时间戳完全相同时才报错并提示 `senv session clear --all`。被选中的缓存仍须通过 salt 与 key 校验，不会被跳过。

需要重新输入口令时，命令错误会给出**根因 + 一条确定的下一步动作**（`expired` / `restarted` / `vault-changed` / `multiple-cache` / `unreadable` / `metadata-replaced`）。

### 2. 启动或续期会话

#### 使用默认超时（8小时）
```bash
senv session start
```

已有有效会话时，`session start` 直接用缓存 key 续期、**不提示密码**，并保留原 timeout 策略；没有会话时才提示一次密码写入新会话。

#### 指定超时时间
```bash
# 30 分钟
senv session start --timeout 30m

# 12 小时
senv session start --timeout 12h

# 1 天
senv session start --timeout 1d

# 7 天
senv session start --timeout 7d

# 1 年
senv session start --timeout 1y
```

#### 特殊超时类型
```bash
# 直到系统重启（推荐用于服务器）
senv session start --timeout restart
```

#### 免密续期（不弹密码）
```bash
senv session refresh
senv session refresh --timeout 12h
```

`session refresh` 只用缓存 key 延长，**从不提示密码**；会话已过期/失效/不可判定时报出原因与下一步，既不新建会话也不删除缓存。

### 3. 使用缓存的会话

功能命令（`env` / `text` / `config` / `tui` / `interactive`）在有有效 session 时复用 derived key；`start` / `refresh` 写入或延长缓存。`duration` 会话在业务命令复用时滑动续期（上限 `session.max_lifetime`）；只读命令（`session status`、`doctor`）不触发续期。

无 session 时，**交互式**终端可提示一次密码作本次临时认证（同进程内复用，**不会**自动开 session）；`eval $(senv env export)` 因 stdout 被捕获**不会**弹密码，而是提示先 `senv session start`。Shell rc 请使用 `--if-session`。

```bash
# 显式启动会话
senv session start
# Enter password: ****
# ✓ Session started (expires in 8h0m0s)

# 后续任意入口免密
senv env list
senv env get DATABASE_URL
senv config list
senv tui

# 未 start 时：交互式命令用一次要一次密码（同进程内只问一次），且不留下 session
senv env get FOO          # 要密码
senv session status       # 仍无 active session
```

### 4. 清除会话

```bash
# 只清当前 vault（其他 vault 的会话保留）
senv session clear
# ✓ Session cache cleared (current vault)

# 清所有 vault 槽位与旧单槽残留
senv session clear --all
# ✓ All session caches cleared
```

## 配置

### 默认会话超时

在 `~/.config/senv/data/settings.json` 中配置：

```json
{
  "session": {
    "enabled": true,
    "timeout": "8h",
    "max_lifetime": "24h",
    "auto_start": false
  }
}
```

- `timeout`：`session start` 未传 `--timeout` 时的默认值。
- `max_lifetime`：滑动续期的绝对上限，默认 `24h`；会话生命周期不会超过 `created_at + max_lifetime`。
- `auto_start`：默认 `false`。设 `true` 后，带密码验证的业务命令在成功后会自动重建持久会话；默认关闭时临时密码用完即弃、不落盘。

### 禁用会话缓存

功能命令本身不会自动创建 session。若不需要免密，只需不要运行 `senv session start`（或用 `senv session clear` 清除已有 cache）。

也可用命令行临时禁用本次 start：

```bash
senv session start --timeout false
```

## 超时格式

支持以下格式：

| 格式 | 说明 | 示例 |
|------|------|------|
| `Nm` | N 分钟 | `30m` |
| `Nh` | N 小时 | `8h` |
| `Nd` | N 天 | `1d`, `7d` |
| `Ny` | N 年（365天） | `1y` |
| `restart` | 直到系统重启 | `restart` |
| `false` | 禁用缓存 | `false` |

> 注：`restart` 会话只看 boot ID，不看时间；`duration` 会话可跨重启存活到到期。旧版本写入的 `timeout_type: "never"` 缓存会被按 `restart` 收养（行为本就相同）；新配置不再接受 `never`，请改用 `restart`。

## 审计日志

所有会话操作都会记录到审计日志：

```bash
# 查看审计日志
cat ~/.log/senv/audit.log | jq
```

除 `session_start` / `session_clear` 外，失效路径也会分别落盘 `session_expire`（duration 到期）、`session_invalidated`（重启/ vault 不匹配）、`session_unverifiable`（环境或缓存无法判定）。日志只记录 session ID、timeout 类型与原因文本，不含 key、salt 或明文。

日志示例：
```json
{
  "timestamp": "2026-03-06T22:00:00Z",
  "event_type": "session_start",
  "session_id": "sess-abc123",
  "timeout_type": "duration",
  "success": true,
  "message": "Session started with timeout: 8h0m0s",
  "hostname": "MacBook-Pro",
  "username": "wii"
}
```

## 使用场景

### 场景 1: 日常开发

```bash
# 早上 / 登录后启动会话
senv session start --timeout 8h
# 或：senv session start -t restart

# 工作期间无需重复输入密码
eval "$(senv env export --if-session)"
senv env list
senv env get DATABASE_URL

# 需要更长时间时免密延长
senv session refresh --timeout 8h
```

推荐写入 `~/.zshrc`（无 session 时静默跳过，不弹密码）：

```bash
# ~/.zshrc
# senv session start -t restart   # 登录后手动执行一次
eval "$(senv env export --if-session)"
```

### 场景 2: 演示/会议

```bash
# 短期会话
senv session start --timeout 1h
```

### 场景 3: 服务器

```bash
# 直到重启自动清除
senv session start --timeout restart
```

### 场景 4: 长期项目

```bash
# 一周的会话
senv session start --timeout 7d
```

## 安全建议

| 超时类型 | 适用场景 | 安全等级 | 建议 |
|---------|---------|---------|------|
| `30m` - `8h` | 日常工作 | ⭐⭐⭐⭐⭐ | **推荐** |
| `1d` - `7d` | 长期项目 | ⭐⭐⭐⭐ | 可接受 |
| `30d` - `1y` | 个人设备 | ⭐⭐ | ⚠️ 谨慎使用 |
| `restart` | 服务器 | ⭐⭐⭐⭐ | **推荐** |

## 故障排查

### 会话无效

```bash
senv session status
# Session: Expired
# Next: senv session start

# 或会话仍有效、只想延长
senv session refresh

# 失效时（缓存保留，不会被自动删除）
# Session: Invalidated
# Cache: retained
# Next: senv session start

# 不可判定时
# Session: Unverifiable
# Cache: retained (not deleted)
# Next: resolve the environment issue above, then retry (`senv session clear --all` discards the cache)
```

### 缓存文件权限错误

```bash
# 检查缓存文件权限
ls -la "$XDG_RUNTIME_DIR/senv/session-"*

# 应该显示：-rw------- (0600)
```

### 审计日志不更新

```bash
# 检查日志目录权限
ls -la ~/.log/senv/

# 应该是 0700 (目录) 和 0600 (文件)
```

## 技术细节

### 缓存文件位置

- **duration / restart**（能证明 tmpfs 时写 runtime；否则 Darwin 默认磁盘逃生舱。槽名是 data path 规范化后的 hash）:
  - `$XDG_RUNTIME_DIR/senv/session-<uid>-<slot>`（优先，须经确认的 tmpfs/ramfs）
  - 后备: `$TMPDIR/` 下随机命名的 `senv-<uid>-<slot>-<rand>` 0700 目录（同样须 memory-backed）
- **磁盘逃生舱** (`--insecure-cache`；stock Darwin 无 tmpfs 时的默认写目标): `${XDG_CACHE_HOME:-~/.cache}/senv/session-<slot>.json`
- 旧版登录钥匙串条目（`senv.session.<uid>` / `senv.v1*`）不再读取或删除；需要时可在钥匙串访问中手动删除

### 缓存文件结构

```json
{
  "key": "base64-encoded-derived-key",
  "salt": "base64-encoded-salt",
  "created_at": "2026-03-06T22:00:00Z",
  "expires_at": "2026-03-07T06:00:00Z",
  "timeout_type": "duration",
  "boot_id": "",
  "data_path_hash": "16-hex-vault-slot",
  "session_id": "sess-abc123",
  "timeout_seconds": 28800
}
```

`timeout_seconds` 是 duration 会话建立时的原始 timeout，用于滑动续期；旧缓存缺该字段时仍可复用，但不会被续期。

### 系统启动 ID 检测

- **Linux**: `/proc/sys/kernel/random/boot_id`
- **macOS**: `sysctl -n kern.boottime`
- **其他**: `uptime -s`

## 常见问题

### Q: 会话缓存会自动续期吗？

A: `duration` 会话在业务命令复用 key 时会滑动续期，但不会超过 `session.max_lifetime`（默认 24h）；也可以用 `senv session refresh` 手动免密延长。`restart` 会话不看时间、只按 boot ID 判定。只读命令（`session status`、`doctor`）不会续期。

### Q: 可以在多个终端窗口中同时使用同一个会话吗？

A: 可以。会话缓存在文件中，同一 vault 的所有终端窗口共享同一个会话；不同 vault 各有自己的槽位。

### Q: 重新 `session start` 会让正在运行的 MCP server 失效吗？

A: 不会。MCP 每个请求按 `keyHash + saltHash + dataPathHash` 绑定 vault 校验，session ID 只用于审计；同一 vault 重建会话后 MCP 继续可用。`session clear` 或 vault salt 变化（`passwd` / rekey）才会拒绝请求。

### Q: 如何在脚本 / shell rc 中使用会话？

A: 先启动会话，再用 `--if-session` 导出（无 session 时不会弹密码、也不失败）：

```bash
#!/bin/bash
senv session start --timeout 1h

# shell rc 推荐写法
eval "$(senv env export --if-session)"

# 或写入文件
senv env export > .env
```

无 session 时直接 `eval $(senv env export)` 会失败并提示 `senv session start`，不再交互要密码。

### Q: 会话缓存会占用多少磁盘空间？

A: 会话缓存文件约 500-1000 字节，可以忽略不计。

## 完整示例

```bash
# 1. 检查当前状态
$ senv session status
Session: No active session

# 2. 启动 8 小时会话
$ senv session start
Enter password: ****
✓ Session started (expires in 8h0m0s)

# 3. 使用会话
$ eval "$(senv env export --if-session)"
export DATABASE_URL='postgresql://localhost/mydb'
export API_KEY='sk-1234567890'

$ senv env list
[default]
  DATABASE_URL=postgresql://localhost/mydb
  API_KEY=sk-1234567890

# 4. 查看会话状态
$ senv session status
Session: Active
Session ID: sess-abc123
Created: 2026-03-06 22:00:00
Timeout: duration
Expires: 2026-03-07 06:00:00 (in 7h 32m)
Sliding window: 8h0m0s, session cap: 2026-03-07 22:00:00

# 5. 免密续期
$ senv session refresh --timeout 8h
✓ Session refreshed (expires in 8h0m0s)

# 6. 清除当前 vault 的会话
$ senv session clear
✓ Session cache cleared (current vault)

# 7. 验证清除
$ senv session status
Session: No active session
```
