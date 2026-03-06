# Senv 项目实施计划

## 项目概述

**Senv** 是一个提供私密配置、环境变量加密存储和解密的程序。

### 核心需求

1. **多功能支持**：环境变量管理、配置文件管理
2. **加密存储**：数据保存在指定路径并加密
   - 每个路径下的文件有单独的密码，由用户指定
   - 用户在输入密码后，将密码加密后存储作为每次使用时的解密密钥
3. **项目初始化**：提供项目初始化功能，指定路径、密码，默认路径 `~/.config/senv/data`
4. **环境变量管理**：
   - 环境变量分 group，哪些 group 生效由配置文件决定
   - 提供 get/set/list 功能
   - 可以在 shell rc 文件快速解密、导入环境变量
5. **配置文件管理**：
   - 支持创建文件，可指定恢复后的文件保存路径
   - 提供 editor 功能：解密后用 vim 编辑；保存后自动加密并放到对应位置
   - 支持导出，指定配置名，导出到配置的路径，也可以重新指定
   - 支持 list

---

## 技术选型

### 1. 编程语言

**选择：Go**

**理由**：
- ✅ 编译为单一二进制文件，部署简单
- ✅ 标准库强大，加密支持完善
- ✅ 跨平台支持（Linux, macOS, Windows）
- ✅ 性能优秀，适合 CLI 工具
- ✅ Cobra 框架成熟，社区活跃

**备选方案**：
- Rust：性能更好但学习曲线陡峭
- Python：开发快速但需要环境和依赖

### 2. 加密方案

**选择：AES-256-GCM + PBKDF2**

**理由**：
- ✅ AES-256-GCM：行业标准、性能好、提供认证加密
- ✅ PBKDF2：100,000 次迭代，防止暴力破解
- ✅ Go 标准库原生支持
- ✅ 安全性经过广泛验证

**备选方案**：
- ChaCha20-Poly1305：在移动设备上性能更好

### 3. CLI 框架

**选择：Cobra**

**理由**：
- ✅ 功能强大，支持子命令
- ✅ Kubernetes、Docker 等知名项目使用
- ✅ 自动生成帮助文档
- ✅ 支持 shell 自动补全
- ✅ 参数验证和类型转换

**备选方案**：
- urfave/cli：轻量级但功能较少

### 4. 配置文件格式

**选择：JSON**

**理由**：
- ✅ 通用性强，工具支持好
- ✅ Go 标准库原生支持
- ✅ 易于调试和手动编辑
- ✅ 无需额外依赖

**备选方案**：
- YAML：易读性好但需要额外库
- TOML：类型明确但工具支持较少

---

## 核心设计

### 1. 数据结构设计

#### 项目元数据 (`metadata.json`)
```json
{
  "version": "1.0",
  "encrypted_master_key": "base64编码的加密主密钥",
  "salt": "base64编码的盐值",
  "created_at": "2024-01-01T00:00:00Z",
  "env_groups": ["default", "production", "development"],
  "config_files": ["database.json", "api_keys.json"]
}
```

#### 用户设置 (`settings.json`)
```json
{
  "active_groups": ["prod", "staging"],
  "default_group": "default",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

#### 环境变量存储 (`env_<group>.json.enc`)
```json
{
  "group": "production",
  "variables": {
    "DATABASE_URL": "postgresql://...",
    "API_KEY": "sk-..."
  },
  "created_at": "...",
  "updated_at": "..."
}
```

#### 配置文件索引 (`config_index.json`)
```json
{
  "configs": {
    "database": {
      "encrypted_file": "database.json.enc",
      "target_path": "~/.config/myapp/database.json",
      "created_at": "...",
      "updated_at": "..."
    }
  }
}
```

### 2. 加密流程设计

#### 密码处理流程：
```
1. 用户输入密码
   ↓
2. PBKDF2 派生密钥 (100,000 迭代, SHA-256)
   ↓
3. AES-256-GCM 加密数据 (随机 Nonce)
   ↓
4. 存储加密数据
```

#### 数据结构：
```
[12字节Nonce] + [加密数据] + [16字节AuthTag]
```

### 3. 分组激活机制

#### 核心概念：
- **Group（分组）**：环境变量按 group 分类
- **Default Group**：默认分组，始终激活
- **Active Groups**：通过 settings.json 管理激活的分组
- **Export**：只导出激活分组的环境变量

#### 激活流程：
```
1. 用户创建分组：senv env group add prod
2. 激活分组：senv env group activate prod
3. settings.json 更新：active_groups: ["prod"]
4. 导出时：eval $(senv env export)
   → 包含 default + prod 的所有变量
```

---

## 项目结构

```
senv/
├── cmd/                        # CLI 命令层
│   ├── root.go                # 根命令 & 全局配置
│   ├── init.go                # senv init - 初始化项目
│   ├── env.go                 # 环境变量命令组
│   ├── config.go              # 配置文件命令组
├── internal/                   # 内部实现
│   ├── crypto/                # 加密模块
│   │   ├── crypto.go          # AES-256-GCM 加密/解密
│   │   └── keyderive.go       # PBKDF2 密钥派生
│   ├── storage/               # 存储模块
│   │   ├── manager.go         # 存储管理器
│   │   ├── metadata.go        # 元数据管理（密码存储）
│   │   └── types.go           # 数据结构定义
│   ├── env/                   # 环境变量模块
│   │   ├── manager.go         # 环境变量管理器
│   │   └── types.go           # Group/EnvVar 结构
│   └── config/                # 配置文件模块
│       ├── manager.go         # 配置文件管理器
│       └── types.go           # ConfigFile 结构
├── docs/                       # 文档目录
│   ├── PLAN.md                # 本文档
│   └── SUMMARY.md             # 项目总结
├── main.go                     # 程序入口
├── go.mod
├── go.sum
├── README.md                   # 项目文档
├── EXAMPLES.md                 # 使用示例
└── .gitignore
```

---

## 实施计划

### 阶段 1：基础框架搭建 ✅

**目标**：初始化项目结构和依赖

**任务**：
- [x] 初始化 Go 模块
- [x] 创建项目目录结构
- [x] 安装依赖（cobra, golang.org/x/crypto, golang.org/x/term）
- [x] 创建 main.go 入口
- [x] 实现 root 命令

**预计时间**：30 分钟

---

### 阶段 2：加密模块实现 ✅

**目标**：实现核心加密功能

**任务**：
- [x] 实现 AES-256-GCM 加密/解密
- [x] 实现 PBKDF2 密钥派生
- [x] 实现随机数生成（盐值、Nonce）
- [x] 编写单元测试

**关键代码**：
```go
// 加密
func Encrypt(key []byte, plaintext []byte) (string, error)

// 解密
func Decrypt(key []byte, ciphertextBase64 string) ([]byte, error)

// 密钥派生
func DeriveKey(password string, salt []byte) []byte
```

**预计时间**：45 分钟

---

### 阶段 3：存储与初始化 ✅

**目标**：实现存储管理器

**任务**：
- [x] 实现存储管理器（Manager）
- [x] 实现元数据管理
- [x] 实现设置管理
- [x] 实现 `init` 命令
- [x] 实现密码验证

**关键功能**：
- 初始化项目目录
- 生成盐值和加密密钥
- 存储加密的密码哈希
- 创建默认配置

**预计时间**：1 小时

---

### 阶段 4：环境变量管理 ✅

**目标**：实现环境变量管理功能

**任务**：
- [x] 实现环境变量管理器
- [x] 实现 `env set/get/delete` 命令
- [x] 实现 `env list` 命令
- [x] 实现分组管理（add/activate/deactivate/list）
- [x] 实现 `env export` 命令（根据激活分组导出）

**关键特性**：
- 分组机制
- 默认分组始终激活
- 通过配置文件控制激活状态
- Shell 集成支持

**预计时间**：1.5 小时

---

### 阶段 5：配置文件管理 ✅

**目标**：实现配置文件管理功能

**任务**：
- [x] 实现配置文件管理器
- [x] 实现 `config create` 命令
- [x] 实现 `config edit` 命令（编辑器集成）
- [x] 实现 `config export` 命令
- [x] 实现 `config list/get/delete` 命令

**关键特性**：
- 加密存储配置文件
- 目标路径映射
- 编辑器集成（$EDITOR）
- 临时文件自动清理

**预计时间**：1.5 小时

---

### 阶段 6：文档与测试 ✅

**目标**：完善文档和测试

**任务**：
- [x] 编写 README.md
- [x] 编写 EXAMPLES.md
- [x] 编写命令行帮助文档
- [x] 功能测试和验证
- [ ] 编写单元测试（后续优化）
- [ ] 编写集成测试（后续优化）

**预计时间**：1 小时

---

## 技术要点

### 1. 依赖库

```go
require (
    github.com/spf13/cobra v1.10.2        // CLI 框架
    golang.org/x/crypto v0.48.0           // PBKDF2
    golang.org/x/term v0.40.0             // 终端密码输入
)
```

### 2. 安全考虑

- ✅ 使用加密安全的随机数生成器（`crypto/rand`）
- ✅ 每次加密使用新的 Nonce
- ✅ 密码不直接存储，使用 PBKDF2 派生
- ✅ 内存中及时清理敏感数据
- ✅ 临时文件使用后立即删除
- ✅ 文件权限设置（700/600）

### 3. 错误处理

- 统一错误格式
- 友好的错误提示
- 详细的错误日志（调试模式）

### 4. 用户体验

- 交互式密码输入（隐藏输入）
- 进度提示
- 彩色输出（可选）
- 自动补全支持

---

## 命令设计

### 全局选项
```
--path string   数据存储路径（默认 ~/.config/senv/data）
```

### 命令列表

#### 初始化
```
senv init                                  初始化项目
```

#### 环境变量
```
senv env set <key> <value>                 设置环境变量（default 分组）
senv env set -g <group> <key> <value>      设置到指定分组
senv env get <key>                         获取环境变量
senv env get -g <group> <key>              从指定分组获取
senv env delete <key>                      删除环境变量
senv env list [group]                      列出环境变量
senv env export                            导出激活分组的环境变量
```

#### 分组管理
```
senv env group list                        列出所有分组
senv env group add <name>                  创建分组
senv env group activate <name>             激活分组
senv env group deactivate <name>           停用分组
```

#### 配置文件
```
senv config create <name> --source <file> --target <path>
senv config edit <name>
senv config export <name> [--path <target>]
senv config list
senv config get <name>
senv config delete <name>
```

---

## 数据存储结构

```
~/.config/senv/data/
├── metadata.json          # 项目元数据（盐值、加密的密码哈希）
├── settings.json          # 用户设置（激活的分组）
├── config_index.json      # 配置文件索引
├── env_default.json.enc   # default 分组的环境变量（加密）
├── env_prod.json.enc      # prod 分组的环境变量（加密）
├── env_dev.json.enc       # dev 分组的环境变量（加密）
├── database.enc           # 配置文件（加密）
└── api_keys.enc           # 配置文件（加密）
```

---

## 使用场景

### 场景 1：多环境管理
```bash
# 开发环境
senv env set DATABASE_URL "postgres://localhost/dev"

# 生产环境
senv env group add prod
senv env set --group prod DATABASE_URL "postgres://prod/db"
senv env group activate prod

# 切换环境
eval $(senv env export)
```

### 场景 2：敏感配置管理
```bash
# 加密 AWS 凭证
senv config create aws-credentials \
  --source ~/.aws/credentials \
  --target ~/.aws/credentials

# 需要时导出
senv config export aws-credentials
```

### 场景 3：Shell 集成
```bash
# 添加到 shell 配置
echo 'eval $(senv env export)' >> ~/.bashrc
source ~/.bashrc
```

---

## 后续优化建议

### 1. 功能增强
- [ ] 密码更改功能
- [ ] 批量导入/导出
- [ ] 环境变量模板
- [ ] 配置文件版本控制
- [ ] 多用户支持

### 2. 安全增强
- [ ] 双因素认证
- [ ] 硬件密钥支持
- [ ] 审计日志
- [ ] 自动锁定

### 3. 用户体验
- [ ] GUI 客户端
- [ ] Web 界面
- [ ] 彩色输出
- [ ] 进度条
- [ ] 自动补全脚本

### 4. 开发工具
- [ ] 完整的单元测试
- [ ] 集成测试
- [ ] 性能测试
- [ ] CI/CD 流程
- [ ] 发布自动化

---

## 时间规划

| 阶段 | 内容 | 预计时间 | 实际时间 |
|------|------|---------|---------|
| 1️⃣ | 基础框架搭建 | 30分钟 | 30分钟 |
| 2️⃣ | 加密模块实现 | 45分钟 | 45分钟 |
| 3️⃣ | 存储与初始化 | 1小时 | 1小时 |
| 4️⃣ | 环境变量管理 | 1.5小时 | 1.5小时 |
| 5️⃣ | 配置文件管理 | 1.5小时 | 1.5小时 |
| 6️⃣ | 文档与测试 | 1小时 | 1小时 |
| **总计** | | **6小时** | **6小时** |

---

## 风险与挑战

### 1. 技术风险
- **密码学实现**：需要确保加密实现的正确性
  - 解决方案：使用标准库，参考成熟实现
  
- **跨平台兼容性**：不同平台的终端和文件系统差异
  - 解决方案：使用跨平台库，充分测试

### 2. 安全风险
- **密码管理**：用户忘记密码无法恢复
  - 解决方案：提供密码提示功能，建议使用密码管理器
  
- **内存安全**：敏感数据可能在内存中泄露
  - 解决方案：及时清理内存，使用安全的数据结构

### 3. 用户体验风险
- **命令复杂度**：命令太多可能难以记忆
  - 解决方案：提供清晰的文档和示例，自动补全
  
- **错误处理**：错误信息不够友好
  - 解决方案：统一错误格式，提供解决建议

---

## 成功标准

### 功能完整性
- ✅ 所有需求功能已实现
- ✅ 命令行接口完整
- ✅ 文档齐全

### 安全性
- ✅ 使用标准加密算法
- ✅ 文件权限正确
- ✅ 密码安全处理

### 可用性
- ✅ 编译成功
- ✅ 命令执行正常
- ✅ 文档清晰

### 代码质量
- ✅ 代码结构清晰
- ✅ 错误处理完善
- ✅ 注释充分

---

## 参考资料

### 加密算法
- [NIST Cryptographic Standards](https://csrc.nist.gov/publications/)
- [Go Crypto Package](https://pkg.go.dev/crypto)

### CLI 框架
- [Cobra Documentation](https://github.com/spf13/cobra)
- [Cobra CLI Generator](https://github.com/spf13/cobra-cli)

### 最佳实践
- [Go Project Layout](https://github.com/golang-standards/project-layout)
- [12 Factor App](https://12factor.net/)

---

## 总结

本实施计划详细描述了 Senv 项目的开发流程，从技术选型到功能实现，从数据结构到命令设计。通过分阶段实施，确保项目能够按时高质量完成。

核心亮点：
1. ✅ **分组激活机制** - 灵活的环境变量管理
2. ✅ **AES-256-GCM 加密** - 工业级安全保障
3. ✅ **Shell 集成** - 无缝的工作流集成
4. ✅ **编辑器集成** - 便捷的配置文件编辑

项目完成后，将为用户提供一个安全、易用的环境变量和配置文件管理工具。
