# Senv 使用示例

本文档展示 Senv 的完整使用流程和功能演示。

## 1. 项目初始化

```bash
# 使用默认路径初始化
./senv init

# 输入密码: your-strong-password
# 确认密码: your-strong-password
```

输出：
```
Initializing senv project at /Users/you/.config/senv/data...
✓ Project initialized successfully!

Quick start:
  senv env set DATABASE_URL "postgres://localhost/db"
  senv env set --group prod API_KEY "sk-xxx"
  senv env list
  eval $(senv env export)
```

## 2. 环境变量管理

### 2.1 设置环境变量

```bash
# 设置到默认分组
./senv env set DATABASE_URL "postgresql://localhost:5432/mydb"
./senv env set API_KEY "sk-1234567890abcdef"
./senv env set REDIS_URL "redis://localhost:6379"

# 创建并设置到生产环境分组
./senv env group add prod
./senv env set --group prod DATABASE_URL "postgresql://prod.example.com:5432/proddb"
./senv env set --group prod API_KEY "sk-production-key-xxxx"

# 创建并设置到开发环境分组
./senv env group add dev
./senv env set --group dev DATABASE_URL "postgresql://localhost:5432/devdb"
./senv env set --group dev DEBUG "true"
```

### 2.2 查看环境变量

```bash
# 获取单个变量
./senv env get DATABASE_URL
# 输出: postgresql://localhost:5432/mydb

# 获取其他分组的变量
./senv env get --group prod DATABASE_URL
# 输出: postgresql://prod.example.com:5432/proddb

# 列出所有变量
./senv env list
# 输出:
# [default]
#   API_KEY=sk-1234567890abcdef
#   DATABASE_URL=postgresql://localhost:5432/mydb
#   REDIS_URL=redis://localhost:6379
#
# [dev]
#   DATABASE_URL=postgresql://localhost:5432/devdb
#   DEBUG=true
#
# [prod]
#   API_KEY=sk-production-key-xxxx
#   DATABASE_URL=postgresql://prod.example.com:5432/proddb

# 只列出特定分组
./senv env list --group prod
# 输出:
# [prod]
#   API_KEY=sk-production-key-xxxx
#   DATABASE_URL=postgresql://prod.example.com:5432/proddb
```

### 2.3 删除环境变量

```bash
./senv env delete DEBUG
# 输出: ✓ Deleted DEBUG from group default
```

## 3. 分组管理

### 3.1 查看分组状态

```bash
./senv env group list
# 输出:
# Environment variable groups:
#   default (default) - active - 3 variables
#   dev - inactive - 2 variables
#   prod - inactive - 2 variables
```

### 3.2 激活/停用分组

```bash
# 激活生产环境分组
./senv env group activate prod
# 输出: ✓ Activated group prod

# 查看分组状态
./senv env group list
# 输出:
# Environment variable groups:
#   default (default) - active - 3 variables
#   dev - inactive - 2 variables
#   prod - active - 2 variables

# 停用分组
./senv env group deactivate prod
# 输出: ✓ Deactivated group prod
```

### 3.3 导出环境变量

```bash
# 导出激活分组的环境变量（default 始终激活）
./senv env export
# 输出:
# export API_KEY='sk-1234567890abcdef'
# export DATABASE_URL='postgresql://localhost:5432/mydb'
# export REDIS_URL='redis://localhost:6379'

# 激活 prod 分组后导出
./senv env group activate prod
./senv env export
# 输出:
# export API_KEY='sk-production-key-xxxx'
# export DATABASE_URL='postgresql://prod.example.com:5432/proddb'
# export REDIS_URL='redis://localhost:6379'
```

### 3.4 在 Shell 中使用

```bash
# 方法 1: 直接 eval
eval $(./senv env export)

# 方法 2: 添加到 shell 配置文件
echo 'eval $(senv env export)' >> ~/.bashrc
source ~/.bashrc

# 验证
echo $DATABASE_URL
# 输出: postgresql://localhost:5432/mydb
```

## 4. 配置文件管理

### 4.1 创建配置文件

```bash
# 准备一个配置文件
cat > /tmp/database.json <<EOF
{
  "host": "localhost",
  "port": 5432,
  "database": "myapp",
  "username": "admin",
  "password": "secret123"
}
EOF

# 加密并存储配置文件
./senv config create database \
  --source /tmp/database.json \
  --target ~/.config/myapp/database.json

# 输出:
# ✓ Created config database
#   Source: /tmp/database.json
#   Target: /Users/you/.config/myapp/database.json
```

### 4.2 查看配置文件列表

```bash
./senv config list
# 输出:
# Configuration files:
#   database
#     Target: /Users/you/.config/myapp/database.json
#     Updated: 2026-03-06T14:00:00Z
```

### 4.3 编辑配置文件

```bash
# 使用默认编辑器编辑（会自动解密、编辑、重新加密）
./senv config edit database

# 如果没有设置 $EDITOR，默认使用 vim
# export EDITOR=code  # 可以设置为你喜欢的编辑器
```

### 4.4 导出配置文件

```bash
# 导出到默认目标路径
./senv config export database
# 输出: Config database exported to /Users/you/.config/myapp/database.json

# 导出到自定义路径
./senv config export database --path /tmp/database-backup.json
# 输出: Config database exported to /tmp/database-backup.json

# 验证导出的文件
cat ~/.config/myapp/database.json
# 输出:
# {
#   "host": "localhost",
#   "port": 5432,
#   "database": "myapp",
#   "username": "admin",
#   "password": "secret123"
# }
```

### 4.5 删除配置文件

```bash
./senv config delete database
# 输出: ✓ Deleted config database
```

## 5. 实际使用场景

### 场景 1: 多环境管理

```bash
# 初始化
./senv init

# 开发环境（默认）
./senv env set DATABASE_URL "postgresql://localhost/dev"
./senv env set REDIS_URL "redis://localhost:6379"
./senv env set DEBUG "true"

# 生产环境
./senv env group add prod
./senv env set --group prod DATABASE_URL "postgresql://prod-server/app"
./senv env set --group prod REDIS_URL "redis://prod-server:6379"
./senv env set --group prod DEBUG "false"

# 测试环境
./senv env group add test
./senv env set --group test DATABASE_URL "postgresql://test-server/test"
./senv env set --group test DEBUG "true"

# 切换环境
./senv env group activate prod
eval $(./senv env export)

# 现在使用的是生产环境配置
echo $DATABASE_URL  # postgresql://prod-server/app
echo $DEBUG         # false
```

### 场景 2: CI/CD 集成

```bash
# 在 CI/CD 脚本中
#!/bin/bash

# 从加密存储中导出环境变量
eval $(senv env export)

# 运行应用
./myapp
```

### 场景 3: 团队共享

```bash
# 1. 在安全的环境中初始化项目
senv init --path ./project-secrets

# 2. 设置共享的环境变量
senv env set --path ./project-secrets SHARED_API_KEY "team-key-xxx"

# 3. 提交加密数据到版本控制（需要密码）
git add project-secrets/
git commit -m "Add encrypted secrets"

# 4. 团队成员克隆后使用
git clone <repo>
cd <repo>
eval $(senv env export --path ./project-secrets)
```

### 场景 4: 敏感配置文件管理

```bash
# AWS 凭证
./senv config create aws-credentials \
  --source ~/.aws/credentials \
  --target ~/.aws/credentials

# SSH 密钥（不推荐，仅示例）
./senv config create deploy-key \
  --source ~/.ssh/deploy_key \
  --target ~/.ssh/deploy_key

# 应用配置
./senv config create app-config \
  --source ./config.production.json \
  --target ./config.json

# 需要时导出
./senv config export aws-credentials
./senv config export app-config
```

## 6. 数据存储位置

```bash
# 默认存储位置
~/.config/senv/data/
├── metadata.json          # 项目元数据
├── settings.json          # 用户设置（激活的分组）
├── config_index.json      # 配置文件索引
├── env_default.json.enc   # default 分组
├── env_prod.json.enc      # prod 分组
├── env_dev.json.enc       # dev 分组
├── aws-credentials.enc    # AWS 配置文件
└── app-config.enc         # 应用配置文件

# 自定义存储位置
./senv init --path /custom/path
./senv env set --path /custom/path KEY value
```

## 7. 安全最佳实践

### 7.1 密码管理

```bash
# 使用强密码（建议使用密码管理器生成）
# ✗ 弱密码: password123
# ✓ 强密码: Xk9#mP2$vL5@nQ8!

# 记录密码提示（不要记录密码本身）
# 在安全的地方记录密码提示
```

### 7.2 文件权限

```bash
# Senv 自动设置安全权限
# 数据目录: 700 (drwx------)
# 数据文件: 600 (-rw-------)

# 验证权限
ls -la ~/.config/senv/data/
# drwx------  7 user  group   224 Mar  6 14:00 .
# -rw-------  1 user  group  1234 Mar  6 14:00 metadata.json
# ...
```

### 7.3 备份

```bash
# 备份整个数据目录
tar -czf senv-backup-$(date +%Y%m%d).tar.gz ~/.config/senv/data/

# 加密备份（推荐）
gpg -c senv-backup-20260306.tar.gz

# 恢复
tar -xzf senv-backup-20260306.tar.gz -C ~/
```

### 7.4 Git 忽略

```bash
# .gitignore
.senv-data/
*.enc
secrets/

# 或者在项目根目录创建专用的数据目录
senv init --path ./.senv-data
echo ".senv-data/" >> .gitignore
```

## 8. 故障排除

### 8.1 忘记密码

```
错误: invalid password

解决方法:
1. 密码无法恢复，需要重新初始化
2. 如果有备份，可以恢复备份
3. 建议使用密码管理器存储密码
```

### 8.2 权限问题

```bash
# 修复权限
chmod 700 ~/.config/senv/data
chmod 600 ~/.config/senv/data/*
```

### 8.3 编辑器未设置

```bash
# 设置默认编辑器
export EDITOR=vim
# 或
export EDITOR=code
# 或
export EDITOR=nano
```

## 9. 命令速查表

```bash
# 初始化
senv init

# 环境变量
senv env set KEY VALUE              # 设置到 default
senv env set -g prod KEY VALUE      # 设置到 prod
senv env get KEY                    # 从 default 获取
senv env get -g prod KEY            # 从 prod 获取
senv env delete KEY                 # 删除
senv env list                       # 列出所有
senv env list -g prod               # 列出 prod
senv env export                     # 导出激活的变量

# 分组管理
senv env group list                 # 列出分组
senv env group add NAME             # 创建分组
senv env group activate NAME        # 激活分组
senv env group deactivate NAME      # 停用分组

# 配置文件
senv config create NAME --source FILE --target PATH
senv config edit NAME
senv config export NAME [--path PATH]
senv config list
senv config get NAME
senv config delete NAME
```
