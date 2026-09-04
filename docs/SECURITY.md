# 安全性说明

本文档说明 senv 的加密机制及其安全性。

## 加密机制概述

senv 使用以下加密组件：

- **密钥派生**: PBKDF2 (Password-Based Key Derivation Function 2)
- **加密算法**: AES-256-GCM (Galois/Counter Mode)
- **哈希算法**: SHA-256
- **迭代次数**: 600,000 次（新建与 `senv passwd` 后；旧 vault 为 100,000 次，见下文「KDF 参数版本化」）
- **盐值长度**: 32 字节 (256 位)
- **密钥长度**: 32 字节 (256 位)

---

## 问题 1: 派生密钥可以轻松反解出原始密码吗？

### 答案: 不可以

### 原因

查看 `internal/crypto/keyderive.go` 中的实现：

```go
func DeriveKeyWithIterations(password string, salt []byte, iterations int) []byte {
    return pbkdf2.Key([]byte(password), salt, iterations, KeySize, sha256.New)
}
```

- 使用 **PBKDF2**（Password-Based Key Derivation Function 2）算法
- **600,000 次迭代**（新 vault）+ **SHA-256** 哈希
- 这是**单向哈希函数**，设计上就是**不可逆的**

即使攻击者获得了：
- 派生密钥
- 盐值

也无法反推出原始密码。唯一可行的方式是暴力破解或字典攻击，迭代次数决定了每次猜测的计算成本（600k 轮 ≈ OWASP 2026 对 PBKDF2-SHA256 的建议值）。

---

## 问题 2: data 数据可以用原始密码直接解码出来吗？能否跨机器保存？

### 答案: 可以解码，可以跨机器保存

### 解码流程

```
原始密码 + Salt → PBKDF2(kdf_iterations) → 派生密钥 → AES-256-GCM 解密 → 明文数据
```

Salt 与 KDF 参数存储在 `metadata.json` 中：

```json
{
  "salt": "base64编码的盐值",
  "password_key": "加密后的密码哈希（用于验证密码）",
  "kdf_iterations": 600000
}
```

旧版本创建的 metadata 没有 `kdf_iterations` 字段，按 100,000 次解释（向后兼容）。

### 跨机器迁移

**完全支持！** 只需要：

1. **复制整个 data 目录**（包含 metadata.json 和所有 .enc 文件）
2. **在新机器上使用相同的原始密码**即可解密所有数据

原因：
- Salt 随 metadata.json 一起保存
- 派生密钥可以由 `原始密码 + Salt` 重新生成
- 所有加密数据都用这个派生密钥加密

---

## 安全性总结

| 方面 | 说明 |
|------|------|
| 派生密钥 → 原始密码 | ❌ 不可逆，PBKDF2 是单向函数 |
| 原始密码 → 解密数据 | ✅ 可以，只要原始密码正确 |
| 跨机器迁移 | ✅ 支持，复制整个 data 目录即可 |
| 数据安全性 | ✅ AES-256-GCM 提供认证加密，防止篡改 |

---

## 核心安全要点

1. **保护原始密码是核心**：数据目录可以安全地跨机器迁移，只要密码不泄露即可
2. **密码无法从加密数据中恢复**：如果忘记密码，数据将无法解密
3. **建议使用强密码**：结合高迭代次数，可以有效抵御暴力破解攻击
4. **派生密钥不落持久盘**：session 缓存（含派生密钥）只写在经操作系统确认的 memory-backed 文件系统；Linux 仅接受 tmpfs/ramfs。`XDG_RUNTIME_DIR` 与 fallback 都会检查实际介质，未知或磁盘介质 fail closed。所有 timeout（含 `never`）均如此，`never` 仅表示不设时间过期，重启后仍失效
5. **敏感文件强制 0600/0700**：metadata、settings.json（含 server token）、各 `.enc` 在每次写入时都会检查并收紧权限，老版本创建的宽松权限文件会被自动收敛

---

## KDF 参数版本化与升级

- metadata 的 `kdf_iterations` 记录本 vault 的 PBKDF2 迭代次数；缺省（旧格式）= 100,000
- **新建 vault** 直接使用 600,000 次
- **存量 vault** 通过 `senv passwd` 升级（可保持原口令：提示输入新口令时输入两次相同口令），升级会换盐、全量重加密并重写 metadata
- ⚠️ **降级限制**：升级到 600,000 次后，旧版本 senv 二进制（硬编码 100,000 次）将无法解锁该 vault（passwordKey 校验必然失败）。升级前请确认所有在用机器都已升级客户端
- KDF 参数是公开成本参数，明文存于 metadata 不降低安全性；加密算法本体（AES-256-GCM）、盐与密钥长度不变
- 当前 metadata 仅接受显式 100,000–1,000,000 次；负数、过低、超上限或无法表示的值会在 PBKDF2 前作为 metadata/KDF 错误拒绝，不会误报密码错误

---

## Session 介质限制

- `session start` 会验证 runtime 路径的实际 backing filesystem，而不是信任路径名。无法确认 memory-backed 时命令非零退出，derived key 不会写盘
- 无安全 runtime 时仍可在交互式命令中输入一次密码；该路径不创建 session cache。也可将 `XDG_RUNTIME_DIR` 指向由用户提供并确认的内存挂载
- MCP 只保留非秘密授权指纹，并在每个工具请求前重新校验 session；expiry、`session clear`、session 替换、boot 变化或 rekey 都会撤销旧 MCP 进程

## Rekey 恢复

`senv passwd` 先完整预检，再通过私有 journal 与同目录 generations 切换密文和 metadata。普通 vault 访问会先检测未完成事务，并在状态可证明时自动回滚或 roll forward。无法验证 journal、hash 或文件状态时系统 fail closed：保留 `.senv-rekey-*` recovery 材料、拒绝普通读写并提示使用新版 `senv doctor`；不要手工删除这些文件。

## 明文导出权限

`senv text get -o`、`senv config export` 与 `senv config install` 新建明文文件默认使用 `0600`，新建父目录为 `0700`，且拒绝目标/父路径符号链接。确需共享非秘密内容时，必须在该次 CLI 调用显式传入严格四位八进制 mode，例如 `--mode 0644`；该选择不会持久化，覆盖既有更严格文件也不会放宽权限。TUI text 导出始终使用 `0600`。

---

## 文件结构

```
data/
├── metadata.json       # 元数据（包含 Salt 和密码验证密钥）
├── settings.json       # 设置（明文）
├── config_index.json   # 配置文件索引（明文）
├── env_default.json.enc  # 加密的环境变量组
└── env_xxx.json.enc    # 其他加密的环境变量组
```

---

## 加密流程详解

### 初始化时

1. 生成随机 32 字节 Salt
2. 使用 PBKDF2 从密码派生 32 字节密钥（600,000 次迭代，记入 metadata）
3. 计算密码的 SHA-256 哈希
4. 用派生密钥加密密码哈希（用于后续验证）
5. 保存 Salt、加密后的密码哈希与 kdf_iterations 到 metadata.json

### 保存数据时

1. 从 metadata.json 读取 Salt 与 kdf_iterations
2. 使用 PBKDF2 从密码 + Salt 按记录的迭代次数派生密钥
3. 使用 AES-256-GCM 加密数据
4. 保存加密数据到文件

### 读取数据时

1. 从 metadata.json 读取 Salt 与 kdf_iterations
2. 使用 PBKDF2 从密码 + Salt 按记录的迭代次数派生密钥
3. 验证密码（解密 password_key 并比对哈希）
4. 使用 AES-256-GCM 解密数据

---

## 参考文档

- [PBKDF2 RFC 8018](https://tools.ietf.org/html/rfc8018)
- [AES-GCM RFC 5116](https://tools.ietf.org/html/rfc5116)
- [NIST Cryptographic Standards](https://csrc.nist.gov/publications/)
