# 安全性说明

本文档说明 senv 的加密机制及其安全性。

## 加密机制概述

senv 使用以下加密组件：

- **密钥派生**: PBKDF2 (Password-Based Key Derivation Function 2)
- **加密算法**: AES-256-GCM (Galois/Counter Mode)
- **哈希算法**: SHA-256
- **迭代次数**: 100,000 次
- **盐值长度**: 32 字节 (256 位)
- **密钥长度**: 32 字节 (256 位)

---

## 问题 1: 派生密钥可以轻松反解出原始密码吗？

### 答案: 不可以

### 原因

查看 `internal/crypto/keyderive.go` 中的实现：

```go
func DeriveKey(password string, salt []byte) []byte {
    return pbkdf2.Key([]byte(password), salt, Iterations, KeySize, sha256.New)
}
```

- 使用 **PBKDF2**（Password-Based Key Derivation Function 2）算法
- **100,000 次迭代** + **SHA-256** 哈希
- 这是**单向哈希函数**，设计上就是**不可逆的**

即使攻击者获得了：
- 派生密钥
- 盐值

也无法反推出原始密码。唯一可行的方式是暴力破解或字典攻击，但由于 10 万次迭代的计算成本，这会非常困难。

---

## 问题 2: data 数据可以用原始密码直接解码出来吗？能否跨机器保存？

### 答案: 可以解码，可以跨机器保存

### 解码流程

```
原始密码 + Salt → PBKDF2 → 派生密钥 → AES-256-GCM 解密 → 明文数据
```

Salt 存储在 `metadata.json` 中：

```json
{
  "salt": "base64编码的盐值",
  "password_key": "加密后的密码哈希（用于验证密码）"
}
```

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
2. 使用 PBKDF2 从密码派生 32 字节密钥（100,000 次迭代）
3. 计算密码的 SHA-256 哈希
4. 用派生密钥加密密码哈希（用于后续验证）
5. 保存 Salt 和加密后的密码哈希到 metadata.json

### 保存数据时

1. 从 metadata.json 读取 Salt
2. 使用 PBKDF2 从密码 + Salt 派生密钥
3. 使用 AES-256-GCM 加密数据
4. 保存加密数据到文件

### 读取数据时

1. 从 metadata.json 读取 Salt
2. 使用 PBKDF2 从密码 + Salt 派生密钥
3. 验证密码（解密 password_key 并比对哈希）
4. 使用 AES-256-GCM 解密数据

---

## 参考文档

- [PBKDF2 RFC 8018](https://tools.ietf.org/html/rfc8018)
- [AES-GCM RFC 5116](https://tools.ietf.org/html/rfc5116)
- [NIST Cryptographic Standards](https://csrc.nist.gov/publications/)
