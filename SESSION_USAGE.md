# Session Cache 功能使用指南

## 概述

Session Cache 功能允许你在一段时间内缓存会话密钥，避免每次运行 `senv` 命令都输入密码。

## 安全性

- ✅ **只缓存派生密钥**，不缓存原始密码
- ✅ 缓存文件权限为 `0600`（仅所有者可读写）
- ✅ 使用 UID 隔离不同用户的缓存
- ✅ 包含数据路径哈希验证，防止缓存误用
- ✅ 完整的审计日志记录
- ✅ 支持灵活的过期策略

## 使用方法

### 1. 查看会话状态

```bash
senv session status
```

### 2. 启动会话

#### 使用默认超时（8小时）
```bash
senv session start
```

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

# 永不过期（不推荐）
senv session start --timeout never
```

### 3. 使用缓存的会话

**仅** `senv session start` 会写入或刷新 session cache。功能命令（`env` / `text` / `config` / `tui` / `interactive`）在有有效 session 时复用 derived key；无 session 时提示密码仅作本次临时认证，**不会**自动开 session。

```bash
# 显式启动会话（唯一落盘方式）
senv session start
# Enter password: ****
# ✓ Session started (expires in 8h0m0s)

# 后续任意入口免密
senv env list
senv env get DATABASE_URL
senv config list
senv tui

# 未 start 时：用一次要一次密码，且不留下 session
senv env get FOO          # 要密码
senv session status       # 仍无 active session
```

### 4. 清除会话

```bash
senv session clear
# ✓ Session cache cleared
```

## 配置

### 默认会话超时

在 `~/.config/senv/data/settings.json` 中配置：

```json
{
  "session": {
    "enabled": true,
    "timeout": "8h"
  }
}
```

### 禁用会话缓存

功能命令本身不会自动创建 session。若不需要免密，只需不要运行 `senv session start`（或用 `senv session clear` 清除已有 cache）。

settings 中的 `session.timeout` 仅作为 `senv session start`（未传 `--timeout`）的默认值：

```json
{
  "session": {
    "enabled": true,
    "timeout": "8h"
  }
}
```

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
| `never` | 永不过期 | `never`（不推荐） |
| `false` | 禁用缓存 | `false` |

## 审计日志

所有会话操作都会记录到审计日志：

```bash
# 查看审计日志
cat ~/.config/senv/data/logs/audit.log | jq
```

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
# 早上启动会话
senv session start --timeout 8h

# 工作期间无需重复输入密码
eval $(senv env export)
senv env list
senv env get DATABASE_URL
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
| `never` | 不推荐 | ⭐ | ⚠️ **强烈不推荐** |

## 故障排查

### 会话无效

```bash
senv session status
# Session: Expired

# 重新启动会话
senv session start
```

### 缓存文件权限错误

```bash
# 检查缓存文件权限
ls -la /tmp/senv-session-*

# 应该显示：-rw------- (0600)
```

### 审计日志不更新

```bash
# 检查日志目录权限
ls -la ~/.config/senv/data/logs/

# 应该是 0700 (目录) 和 0600 (文件)
```

## 技术细节

### 缓存文件位置

- **duration / restart**（临时）:
  - `$XDG_RUNTIME_DIR/senv/session-<uid>`（优先）
  - 后备: `/tmp/senv-session-<uid>`
- **never**（持久，重启后仍在）:
  - `~/.local/share/senv/session/session-<uid>`

### 缓存文件结构

```json
{
  "key": "base64-encoded-derived-key",
  "salt": "base64-encoded-salt",
  "created_at": "2026-03-06T22:00:00Z",
  "expires_at": "2026-03-07T06:00:00Z",
  "timeout_type": "duration",
  "boot_id": "",
  "data_path_hash": "sha256-hash",
  "session_id": "sess-abc123"
}
```

### 系统启动 ID 检测

- **Linux**: `/proc/sys/kernel/random/boot_id`
- **macOS**: `sysctl -n kern.boottime`
- **其他**: `uptime -s`

## 常见问题

### Q: 会话缓存会自动续期吗？

A: 不会。会话到期后需要重新启动。

### Q: 可以在多个终端窗口中同时使用同一个会话吗？

A: 可以。会话缓存在文件中，所有终端窗口共享同一个会话。

### Q: 如何在脚本中使用会话？

A: 先启动会话，然后在脚本中运行命令：

```bash
#!/bin/bash
# 启动会话（假设已设置密码）
senv session start --timeout 1h

# 运行命令（无需密码）
senv env export > .env
```

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
$ senv env export
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

# 5. 清除会话
$ senv session clear
✓ Session cache cleared

# 6. 验证清除
$ senv session status
Session: No active session
```
