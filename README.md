# Senv

🔐 **Senv** 是一个安全的私密配置和环境变量加密存储管理工具。

## 功能特性

- ✅ **环境变量管理** - 按分组管理环境变量，支持 get/set/list/export
- ✅ **配置文件管理** - 加密存储配置文件，支持 create/edit/export
- ✅ **加密存储** - 使用 AES-256-GCM + PBKDF2 加密
- ✅ **分组管理** - 通过激活分组控制哪些环境变量生效
- ✅ **Shell 集成** - 支持 `eval $(senv env export)` 快速导入
- ✅ **编辑器集成** - 使用系统默认编辑器编辑配置文件

## 安装

### 从源码编译

```bash
git clone https://github.com/yourname/senv.git
cd senv
go build -o senv
sudo mv senv /usr/local/bin/
```

## 快速开始

### 1. 初始化项目

```bash
# 使用默认路径 ~/.config/senv/data
senv init

# 或指定自定义路径
senv init --path /path/to/data
```

程序会提示你输入加密密码，该密码用于加密所有数据。

### 2. 管理环境变量

```bash
# 设置环境变量到默认分组
senv env set DATABASE_URL "postgresql://localhost/mydb"
senv env set API_KEY "sk-1234567890"

# 创建并设置变量到指定分组
senv env set --group prod DATABASE_URL "postgresql://prod-server/db"
senv env set --group staging DATABASE_URL "postgresql://staging-server/db"

# 获取环境变量
senv env get DATABASE_URL              # 从 default 分组获取
senv env get --group prod DATABASE_URL # 从 prod 分组获取

# 列出所有环境变量
senv env list              # 列出所有分组
senv env list --group prod # 只列出 prod 分组

# 删除环境变量
senv env delete API_KEY
```

### 3. 管理分组

```bash
# 列出所有分组
senv env group list

# 创建新分组
senv env group add production

# 激活分组（使其变量在 export 时生效）
senv env group activate production

# 停用分组
senv env group deactivate production
```

### 4. 导出环境变量到 Shell

```bash
# 导出所有激活分组的环境变量
eval $(senv env export)

# 添加到 shell 配置文件（例如 ~/.bashrc 或 ~/.zshrc）
echo 'eval $(senv env export)' >> ~/.bashrc
```

**注意**：`default` 分组默认激活，无需手动激活。

### 5. 管理配置文件

```bash
# 创建配置文件（从现有文件导入）
senv config create database --path ~/.config/myapp/database.json

# 编辑配置文件（会自动解密、编辑、重新加密）
senv config edit database

# 导出配置文件到目标路径
senv config export database

# 或导出到自定义路径
senv config export database --path /tmp/database.json

# 列出所有配置文件
senv config list

# 查看配置文件信息
senv config get database

# 删除配置文件
senv config delete database
```

## 工作原理

### 加密方案

- **算法**: AES-256-GCM（认证加密）
- **密钥派生**: PBKDF2（100,000 次迭代，SHA-256）
- **盐值**: 每个项目使用 32 字节随机盐
- **Nonce**: 每次加密使用 12 字节随机 nonce

### 分组激活机制

1. `default` 分组始终激活
2. 其他分组需要通过 `senv env group activate` 激活
3. `senv env export` 只导出激活分组的环境变量
4. 激活状态保存在 `settings.json` 文件中

### 数据存储

```
~/.config/senv/data/
├── metadata.json          # 项目元数据（盐值、加密的密码哈希）
├── settings.json          # 用户设置（激活的分组）
├── config_index.json      # 配置文件索引
├── env_default.json.enc   # default 分组的环境变量（加密）
├── env_prod.json.enc      # prod 分组的环境变量（加密）
└── database.enc           # 配置文件（加密）
```

## 使用场景

### 场景 1: 开发环境管理

```bash
# 初始化
senv init

# 设置开发环境变量
senv env set DATABASE_URL "postgresql://localhost/dev"
senv env set REDIS_URL "redis://localhost:6379"

# 创建生产环境分组
senv env group add prod
senv env set --group prod DATABASE_URL "postgresql://prod-server/db"
senv env set --group prod REDIS_URL "redis://prod-server:6379"

# 添加到 shell 配置
echo 'eval $(senv env export)' >> ~/.bashrc
source ~/.bashrc
```

### 场景 2: 配置文件加密管理

```bash
# 加密敏感配置文件
senv config create aws_credentials \
  --path ~/.aws/credentials \
  --target ~/.aws/credentials

# 需要时导出
senv config export aws_credentials

# 或直接编辑加密文件
senv config edit aws_credentials
```

### 场景 3: 多项目环境管理

```bash
# 项目 A
cd project-a
senv init --path ./.senv-data
senv env set DATABASE_URL "postgres://localhost/project_a"

# 项目 B
cd ../project-b
senv init --path ./.senv-data
senv env set DATABASE_URL "postgres://localhost/project_b"

# 在各自项目中使用
eval $(senv env export)
```

## 安全建议

1. **使用强密码** - 选择复杂的密码，建议使用密码管理器生成
2. **定期备份** - 备份整个数据目录（`~/.config/senv/data/`）
3. **不要提交数据目录** - 将数据目录添加到 `.gitignore`
4. **限制文件权限** - 数据目录权限自动设置为 700，文件权限为 600
5. **安全传输** - 在不同机器间传输时使用加密通道

## 常见问题

### Q: 忘记密码怎么办？

A: 密码无法恢复。如果你忘记了密码，所有数据将无法解密。建议：
- 使用密码管理器存储密码
- 定期备份数据目录
- 记录密码提示

### Q: 如何在多台机器间同步？

A: 你可以：
1. 使用加密的云存储（如 Cryptomator、Syncthing）
2. 手动复制整个数据目录
3. 确保使用相同的密码

### Q: 数据存储在哪里？

A: 默认存储在 `~/.config/senv/data/`，可以通过 `--path` 参数指定其他位置。

### Q: 如何更改密码？

A: 目前不支持直接更改密码。你需要：
1. 导出所有环境变量和配置文件
2. 删除数据目录
3. 重新初始化并导入数据

## 命令参考

### 全局选项

```
--path string   数据存储路径（默认 ~/.config/senv/data）
```

### 命令列表

```
senv init                          初始化项目
senv env get <key>                 获取环境变量
senv env set <key> <value>         设置环境变量
senv env delete <key>              删除环境变量
senv env list [group]              列出环境变量
senv env export                    导出环境变量到 shell
senv env group list                列出所有分组
senv env group add <name>          创建分组
senv env group activate <name>     激活分组
senv env group deactivate <name>   停用分组
senv config create <name>          创建配置文件
senv config edit <name>            编辑配置文件
senv config export <name>          导出配置文件
senv config list                   列出所有配置文件
senv config get <name>             查看配置文件信息
senv config delete <name>          删除配置文件
```

## 开发

### 构建

```bash
go build -o senv
```

### 测试

```bash
go test ./...
```

### 依赖

- [github.com/spf13/cobra](https://github.com/spf13/cobra) - CLI 框架
- [golang.org/x/crypto](https://golang.org/x/crypto) - 加密算法（PBKDF2）
- [golang.org/x/term](https://golang.org/x/term) - 终端密码输入

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 更新日志

### v1.0.0 (2026-03-06)

- ✨ 初始版本发布
- ✅ 环境变量管理功能
- ✅ 配置文件管理功能
- ✅ AES-256-GCM 加密
- ✅ 分组激活机制
- ✅ Shell 集成
