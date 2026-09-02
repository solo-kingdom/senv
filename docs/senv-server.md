# senv-server 部署

senv-server 是零知识密文托管服务端：独立二进制，与 `senv` CLI 同仓（`senv-server/` main 包），
不经过 cobra，因此 `senv --help` 里没有它。所有持久化内容都是客户端产物（密文/不透明 blob），
server 只存密文 + token 哈希。

## 依赖

- Postgres（多用户；schema 由内置迁移管理）
- 生产环境必须由反向代理终结 TLS（server 本身跑明文 HTTP，仅监听内网/回环）

## 构建

```bash
make build-server   # 产出 senv-server-bin
```

## 部署步骤

```bash
export SENV_SERVER_DSN="postgres://senv:****@db-host:5432/senv"

# 1. 应用 schema 迁移（serve 启动时会校验版本，不匹配则拒绝启动）
./senv-server-bin migrate

# 2. 启动服务（默认 :8080，可用 --addr 或 SENV_SERVER_ADDR 覆盖）
./senv-server-bin serve --addr 127.0.0.1:8080

# 3. 创建用户并签发 token（明文只展示一次，库中只存 SHA-256 哈希）
./senv-server-bin admin create-user alice

# 吊销 token（不影响同用户其他 token）
./senv-server-bin admin revoke-token <token>
```

## 客户端接入

```bash
# 已有 git 模式本地 vault：一键搬迁
senv migrate to-server --server https://senv.example.com --token <token>

# 本机切到 server provider：编辑 ~/.config/senv/settings.json
#   "provider": {"type": "server", "address": "...", "token": "...", "vault": "main"}

# 新机器接入已有 vault（vault 口令绝不发往 server）
senv init --server https://senv.example.com --token <token>

# 日常同步（断网时本地读写不受影响，恢复后同步收敛）
senv sync
```

## 运维要点

- 备份 = 备份 Postgres 库即可；用户可随时 `senv migrate from-server` 导回本地/git 仓，无锁定
- token 泄漏 → `admin revoke-token` 吊销；库中无明文 token
- DB 泄漏的残余风险：metadata blob 含加密后的 passwordKey，可被离线爆破 vault 口令，
  由 PBKDF2 100k 迭代缓解——要求强口令
- API 全部位于 `/v1/` 前缀；健康检查 `GET /healthz` 无需认证
