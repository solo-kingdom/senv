# 路径重构文档

## 概述

本次重构将配置文件、数据文件和日志文件分离存储，以提高管理的灵活性和可维护性。

## 修改内容

### 1. 文件存储路径分离

#### 之前
所有文件都存储在同一个目录中：
```
~/.config/senv/data/
├── metadata.json
├── settings.json
├── config_index.json
├── env_*.json.enc
└── *.enc
```

#### 之后
文件按类型分离存储：

**配置文件** (`~/.config/senv/`):
- `metadata.json` - 项目元数据（盐值、加密的密码哈希）
- `settings.json` - 用户设置（激活的分组）
- `config_index.json` - 配置文件索引

**数据文件** (`~/.config/senv/data/` 或自定义路径):
- `env_*.json.enc` - 加密的环境变量
- `*.enc` - 加密的配置文件

**日志文件** (`~/.log/senv/`):
- `audit.log` - 审计日志

### 2. 代码修改

#### 2.1 Storage Manager (`internal/storage/manager.go`)
- 添加 `configPath` 字段，用于存储配置文件路径
- 保留 `dataPath` 字段，用于存储数据文件路径
- 修改 `NewManager` 函数签名：`NewManager(configPath, dataPath string)`
- 更新所有配置文件操作方法，使用 `configPath`：
  - `LoadMetadata()` / `SaveMetadata()`
  - `LoadSettings()` / `SaveSettings()`
  - `LoadConfigIndex()` / `SaveConfigIndex()`
- 数据文件操作继续使用 `dataPath`

#### 2.2 Root Command (`cmd/root.go`)
- 添加 `getConfigPath()` 函数，返回 `~/.config/senv`
- 保留 `getDataPath()` 函数，返回默认 `~/.config/senv/data` 或自定义路径
- 更新全局参数说明，明确 `--path` 只影响数据路径

#### 2.3 Init Command (`cmd/init.go`)
- 更新初始化逻辑，分别创建配置目录和数据目录
- 显示配置路径和数据路径信息

#### 2.4 其他命令文件
- `cmd/env.go`: 更新 `getEnvManager()` 使用分离的路径
- `cmd/config.go`: 更新 `getConfigManager()` 使用分离的路径
- `cmd/session.go`: 更新所有会话管理相关代码

#### 2.5 Session Manager (`internal/session/manager.go`)
- 更新 `NewManager` 函数签名：`NewManager(configPath, dataPath string)`
- 更新 `StartSession` 方法中的 storage manager 创建

#### 2.6 Audit Logger (`internal/session/audit.go`)
- 修改 `NewAuditLogger` 使用 `~/.log/senv` 作为日志目录
- 确保日志文件独立于配置和数据文件

### 3. 向后兼容性

本次修改**不兼容**之前的数据结构。用户需要：
1. 备份旧数据目录（`~/.config/senv/data/`）
2. 删除旧目录
3. 重新初始化项目（`senv init`）
4. 重新导入环境变量和配置文件

### 4. 优势

#### 4.1 更好的组织结构
- 配置文件和数据文件分离，职责清晰
- 日志文件独立存储，便于管理和审计

#### 4.2 更灵活的部署选项
- 可以将数据文件存储在任意位置（包括加密的云存储）
- 配置文件始终在固定位置，便于查找
- 日志文件遵循 Unix 标准目录结构

#### 4.3 更好的可移植性
- 数据路径可以配置，便于在不同机器间同步数据
- 配置文件和数据文件分离，支持部分迁移

#### 4.4 符合 Unix 标准
- 配置文件：`~/.config/<app>/`
- 数据文件：`~/.local/share/<app>/` 或自定义
- 日志文件：`~/.log/<app>/` 或 `/var/log/<app>/`

### 5. 使用示例

#### 5.1 默认配置
```bash
# 初始化（使用默认路径）
senv init

# 配置文件位置: ~/.config/senv/
# 数据文件位置: ~/.config/senv/data/
# 日志文件位置: ~/.log/senv/
```

#### 5.2 自定义数据路径
```bash
# 使用自定义数据路径
senv init --path /path/to/custom/data

# 配置文件位置: ~/.config/senv/
# 数据文件位置: /path/to/custom/data/
# 日志文件位置: ~/.log/senv/
```

#### 5.3 多项目管理
```bash
# 项目 A
senv init --path ~/projects/project-a/.senv-data

# 项目 B
senv init --path ~/projects/project-b/.senv-data

# 切换项目时使用 --path 参数
senv --path ~/projects/project-a/.senv-data env list
```

### 6. 测试建议

#### 6.1 单元测试
- 测试 `NewManager` 使用不同的路径组合
- 测试配置文件和数据文件的分离存储
- 测试日志文件的独立存储

#### 6.2 集成测试
- 测试完整的工作流程（初始化、添加数据、导出）
- 测试路径切换功能
- 测试日志文件的正确写入

#### 6.3 手动测试
```bash
# 1. 清理旧数据
rm -rf ~/.config/senv ~/.log/senv

# 2. 初始化项目
./senv init
# 输入密码: test123

# 3. 检查目录结构
ls -la ~/.config/senv/
ls -la ~/.config/senv/data/
ls -la ~/.log/senv/

# 4. 添加环境变量
./senv env set TEST_VAR "test value"

# 5. 检查文件位置
ls -la ~/.config/senv/          # 应该有 metadata.json, settings.json, config_index.json
ls -la ~/.config/senv/data/     # 应该有 env_default.json.enc

# 6. 测试自定义路径
./senv --path /tmp/senv-test init
ls -la ~/.config/senv/          # 配置文件仍在默认位置
ls -la /tmp/senv-test/          # 数据文件在自定义位置
```

### 7. 已知问题

- 无向后兼容性：旧数据需要手动迁移
- 密码提示在非交互式环境下可能有问题（需要进一步改进）

### 8. 未来改进

- [ ] 添加数据迁移工具，从旧版本导入数据
- [ ] 支持环境变量配置默认数据路径
- [ ] 添加配置文件版本管理
- [ ] 支持多个配置文件（不同的加密密钥）

## 总结

本次重构成功实现了配置文件、数据文件和日志文件的分离存储，提高了项目的灵活性和可维护性。新的目录结构更加清晰，符合 Unix 标准，并支持更灵活的部署选项。
