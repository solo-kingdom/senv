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
#    可选参数：
#      -max-body-bytes N      单请求体上限（默认 64MB，覆盖 1000×512KB 理论最大值）
#      -auth-rate-limit N     每分钟每来源 IP 认证失败阈值（默认 30，负值关闭）
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

地址scheme：客户端默认只接受 `https://` 的 server 地址；可信内网要用明文 http 时，
显式设置环境变量 `SENV_ALLOW_INSECURE_HTTP=1`（构造 provider 时会向 stderr 打警告）。

## 运维要点

- 备份 = 备份 Postgres 库即可；用户可随时 `senv migrate from-server` 导回本地/git 仓，无锁定
- token 泄漏 → `admin revoke-token` 吊销；库中无明文 token
- DB 泄漏的残余风险：metadata blob 含加密后的 passwordKey，可被离线爆破 vault 口令，
  由 PBKDF2 迭代次数缓解（新 vault 600k；旧 vault 经 `senv passwd` 升级）——要求强口令
- API 全部位于 `/v1/` 前缀；健康检查 `GET /healthz` 无需认证
- server 自身带读写/空闲超时、请求体上限与认证失败限速；日志中的内部错误细节只写
  服务端日志，客户端只收到通用 `internal error`

## 加固清单（tcbj 部署 runbook）

安全审查（2026-09）后建议在运维窗口执行的加固项，按优先级排列：

### 1. registry 加认证（供应链，P1）

`registry.wii.pub` 目前无认证，wg/LAN 内任何被攻破的机器都可 push 恶意镜像。
给 registry 加 htpasswd：

```bash
# 生成凭据（需要 htpasswd，来自 apache2-utils 或 httpie）
htpasswd -Bbn registry-admin '<强口令>' > /etc/registry/htpasswd

# registry 配置（config.yml）增加：
# auth:
#   htpasswd:
#     realm: basic-realm
#     path: /etc/registry/htpasswd

# tcbj 各节点登录一次（~/.docker/config.json 会存 base64 凭据，注意该文件权限 0600）
docker login registry.wii.pub
```

### 2. PG 收紧跨库 CONNECT（信任边界，P2）

senv 与 casdoor 共享 PG 实例。casdoor 角色是实例 superuser，应用层沦陷可拖走 senv
全部密文离线爆破；反向也应收紧——默认 PUBLIC 可 CONNECT 任意库：

```sql
REVOKE CONNECT ON DATABASE casdoor FROM PUBLIC;
REVOKE CONNECT ON DATABASE senv FROM PUBLIC;
GRANT  CONNECT ON DATABASE senv    TO senv;
GRANT  CONNECT ON DATABASE casdoor TO casdoor;
```

验证：以无权限角色 `\c casdoor` / `\c senv` 应报 `permission denied for database "..."`。
远期项（本次不做）：拆分实例或给 casdoor 角色降权。

### 3. 存量文件权限一次性收紧（本机/各客户端节点）

老版本创建的 `~/.config/senv/settings.json`、部分 `.enc`、`data/`、`envs/` 目录可能是
0644/0755；新版本每次写入都会自动收紧，但存量文件在下次写入前仍宽松：

```bash
chmod -R go-rwx ~/.config/senv ~/.local/share/senv
```

### 4. 备份排除项更新（P1 配套）

- `~/.config/senv/settings.json` 含 provider token（server 模式），确认备份策略里它的
  保护级别与 vault 本体一致，或直接排除
- session 派生密钥缓存现在只存在于 tmpfs（`XDG_RUNTIME_DIR`），不再出现在
  `~/.local/share/senv`，备份天然不会带走；旧版本遗留的
  `~/.local/share/senv/session/` 已被新版本自动清理

### 5. caddy 层可选项

- 认证限速：server 内置按 IP 的失败限速（默认 30 次/分钟）；如需在边缘再加一层，
  可用 caddy 的 `rate_limit` 插件对 `/v1/` 路径限速
- 访问日志：如需排障/溯源，可在 caddy 开 access log（server 本身保持精简）

### 6. 客户端升级顺序

1. 所有在用机器先升级 senv 客户端（旧二进制硬编码 100k，无法解锁升级后的 vault）
2. 逐台执行 `senv passwd`（口令可不变，输两次相同新口令）完成 KDF 升级
3. 升级后首次运行会清理遗留的持久 session 缓存并要求重新解锁一次（属预期）
