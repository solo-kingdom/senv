# senv-server 部署

## 公网加固

单人/小团队自用部署到公网时的建议基线。四项能力已随代码落地：DB 双角色、admin 审计、webhook 告警、token pepper——本节是它们的部署操作手册。

### 数据库双角色（日志不可篡改）

serve 与 admin CLI 不要共用同一个数据库角色，否则拿到 serve DSN 即可 `UPDATE/DELETE access_log` 抹除访问痕迹。

- 角色授权模板（单一事实源）：`senv-server/sql/roles.sql`——serve 角色 `senv_server` 对 `access_log` 仅 `INSERT`+`SELECT`；admin 角色 `senv_admin` 持有全量权限（`admin logs-prune` 与自动清理用）
- 部署步骤：以超户/库属主创建两角色并执行模板 → serve 用 `senv_server` DSN → admin CLI / cron 用 `senv_admin` DSN
- 注意：受限角色下 serve 的 `--logs-retain-days` 自动清理会权限失败（best-effort 记日志，不影响服务）。生产应设 `--logs-retain-days 0`，由 cron 跑清理：

```bash
# 每天凌晨清理 90 天前的访问日志（admin 角色 DSN）
0 3 * * * SENV_SERVER_DSN='postgres://senv_admin:****@db/senv'   /usr/local/bin/senv-server-bin admin logs-prune --before $(date -d '90 days ago' +%F)
```

### admin 操作审计

`admin create-user / revoke-token / create-registration / block-client / unblock-client` 成功后各写一条 `outcome=ADMIN` 事件（reason 记操作类型与目标）。查询：

```bash
./senv-server-bin admin logs --outcome ADMIN
```

### 告警 webhook

serve 支持通用 webhook（不内置任何第三方 provider，自行接 n8n/飞书机器人/Telegram gateway）：

```bash
./senv-server-bin serve --alert-webhook https://example.com/senv-alerts   --alert-auth-fail-threshold 10 --alert-debounce 5m
# 或环境变量 SENV_SERVER_ALERT_WEBHOOK
```

触发场景：同一来源连续 `AUTH-FAILED` 超阈值、`client_blocked`、新 client 注册成功、client 换 IP 首次访问。payload 为 JSON，只含时间/IP/client/user 名等元数据。网关侧建议加 secret 头校验防伪造。未配置时告警完全关闭、零开销。

### token pepper

serve 读 `SENV_SERVER_TOKEN_PEPPER`（可选）。配置后 token 存 `HMAC-SHA256(pepper, token)`——数据库整库泄露单独不足以离线验证 token。pepper 只驻留进程内存，**须与 DSN 凭证同级备份；丢失即全部 token 失效**。

启用后存量 token 自动走回退比对（进程内正缓存 1 分钟），服务不中断；回退命中记 `legacy sha256 token hash used` 慢日志。请尽快轮换：

```bash
# 逐设备：吊销旧 token，重新签发注册码，客户端重新 register
./senv-server-bin admin revoke-token <old-token>
./senv-server-bin admin create-registration <user>
# 客户端：senv server register --address <server> --code <注册码> --name <设备名>
```

### systemd 加固

生产建议以专用动态用户运行（单元文件模板，按需调整路径）：

```ini
[Unit]
Description=senv-server
After=network.target postgresql.service

[Service]
ExecStart=/usr/local/bin/senv-server-bin serve --addr 127.0.0.1:8080 --logs-retain-days 0
Restart=on-failure
DynamicUser=yes
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
```

### 反代推荐配置

server 本身跑明文 HTTP，TLS 由反向代理终结（回环或内网监听）：

- TLS 1.2+ only，现代 cipher；开 HSTS（`Strict-Transport-Security: max-age=63072000`）
- 认证端点限速（nginx 示例）：`limit_req_zone $binary_remote_addr zone=senv_auth:10m rate=10r/m;` 作用于 `/v1/`
- client_max_body_size 与 server `--max-body-bytes` 对齐（默认 64MB）

### 单实例边界

进程内认证失败限速器、告警去抖与旧哈希回退正缓存都是内存态：**不支持多实例部署**（多副本会出现限速/去抖各自为政、回退窗口重复查询）。需要高可用请先讨论架构变更。

### 元数据泄露边界

零知识架构保证 server 与 DB 持有者看不到条目内容，但以下元数据对「拿到数据库的人」可见：vault 名、条目数量、revision 变化频率、访问时间与来源 IP（access_log）。内容机密性不受影响；若连这些模式都不愿暴露，需要另一量级的工程（条目填充、固定频率同步），当前不做。


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
#      -trust-proxy-headers   反代时按 X-Real-IP/X-Forwarded-For 识别真实来源 IP
#                             （默认关闭；仅当直连对端是 loopback 或私网地址才采信，
#                             覆盖同机与 docker 网桥/内网反代拓扑）
#      -logs-retain-days N    访问日志保留天数（默认 90，0 关闭自动清理）
./senv-server-bin serve --addr 127.0.0.1:8080

# 3. 创建用户并签发 token（明文只展示一次，库中只存 SHA-256 哈希）
./senv-server-bin admin create-user alice

# 新设备接入：为用户 alice 签发一次性注册码（默认有效期 30 分钟，明文只展示一次）
./senv-server-bin admin create-registration alice
# 客户端执行: senv server register --address <server> --code <注册码> --name <设备名>

# 吊销 token（不影响同用户其他 token）
./senv-server-bin admin revoke-token <token>
```

## 客户端接入

```bash
# 已有 git 模式本地 vault：一键搬迁
senv migrate to-server --server https://senv.example.com --token <token>

# 本机切到 server provider：编辑 ~/.config/senv/settings.json
#   "provider": {"type": "server", "address": "...", "vault": "main"}
# token 存独立的机器本地文件 ~/.config/senv/server-token.json（0600，
# 不入 git 同步；register/init 自动写入，旧版 settings 内嵌 token 自动迁移）
# 可选自动同步配置：
#   "auto_sync": false,     # 默认开启；false 时回到仅手动 senv sync
#   "sync_throttle": "30s"  # 自动 pull 节流窗口；空/非法值回退 30s

# 新机器接入已有 vault（vault 口令绝不发往 server）
senv init --server https://senv.example.com --token <token>

# 冲突查看与修复 / 手动全量同步（断网时本地读写不受影响，恢复后同步收敛）
senv sync
```

vault 名规则：仅限可移植路径段字符（禁止 `/` `\` `:`、`.`/`..` 等），最长 128 字节；
server 侧会拒绝非法 vault 名（400）。默认值 `main` 天然合规。

地址scheme：客户端默认只接受 `https://` 的 server 地址；可信内网要用明文 http 时，
显式设置环境变量 `SENV_ALLOW_INSECURE_HTTP=1`（构造 provider 时会向 stderr 打警告）。

## 自动同步行为

server provider 默认在命令生命周期内做 best-effort 同步，不启动常驻进程：

- 读取命令（如 `env get/list/export`、`text get/list`、`config get/list`、
  `session start`、`tui`）先按 `sync_throttle` 增量拉取；窗口内直接使用本地缓存。
  拉取预算约 2 秒，超时或不可达时静默退回本地数据。
- 写入命令先完成本地落盘，进程退出前推送待同步更改。推送失败不改变原命令退出码，
  本地更改保留并显示“待推送”警告；后续任意命令会自动重试。
- `passwd` 与初始化后的关键写入使用约 10 秒的阻塞确认推送；失败时本地更改仍生效，
  但会输出强警告并指引执行 `senv sync`。
- MCP `serve` 的读工具复用同一节流拉取路径，连续请求通常只有一次网络拉取。
- 同一数据目录的同步段通过 `.senv-sync.lock` 串行化；锁文件与数据目录权限分别为
  0600/0700。锁被其他进程持有时本次自动同步跳过。

强制绕过节流窗口：

```bash
senv env get API_KEY --refresh
senv env list --refresh
senv env export --refresh
senv text get TLS_CERT --refresh
senv text list --refresh
senv config get app.toml --refresh
senv config list --refresh
senv session start --refresh
senv tui --refresh
```

乐观锁冲突不会被自动覆盖。自动推送遇到冲突时仅列出冲突条目并提示 `senv sync`；
手动 `senv sync` 在 TTY 中会先显示脱敏摘要（本地 base revision、远端 revision、
删除状态、size、hash 与可用更新时间），再进入交互式解决器：

| 快捷键 | 作用 |
| --- | --- |
| `j` `k` / `↑` `↓` | 选择冲突 |
| `Enter` | 查看本地/远端详情 |
| `v` | 显式揭示 / 重新掩码内容 |
| `l` / `r` | 当前条目使用本地 / 远端 |
| `L` / `R` | 未逐条处理项全部使用本地 / 远端 |
| `m` | 对兼容条目打开 `VISUAL`/`EDITOR` 手动合并 |
| `y` | 预览并确认覆盖计划 |
| `q` | 退出且不修改任何一端 |

`config_index` 会显示按配置名归纳的 target/group/description 差异。`env` 值默认
掩码；text/config 只有按 `v` 后才显示明文。`vault metadata` 只显示安全摘要，必须
整体选择一边，不能 raw 编辑。远端 metadata 与本地 key 不兼容时，editor merge 会被
禁用，避免生成无法解锁的混合状态。

editor merge 是 LOCAL/REMOTE 两方手动合并，不是自动 three-way merge。退出 editor 后
必须移除全部 `SENV_LOCAL` / `SENV_REMOTE` 标记并通过类型校验；senv 使用一次性私有
目录保存缓冲区，退出后递归清理。远端在编辑期间再次变化时会重新进入冲突流程。

脚本或不想进入 UI 时：

```bash
senv sync --no-interactive      # 输出增强脱敏报告与解决指引
senv sync --accept-remote       # 放弃本地冲突版本，采用远端
senv sync --force-push          # 放弃远端冲突版本，采用本地
```

旧 server 未返回新增时间/大小描述符时，CLI 会显示 `N/A` 并保留 revision 冲突语义；
冲突 409 响应本身不携带 ciphertext。

保留的非交互策略如下：

```bash
senv sync --accept-remote   # 放弃本地，采用远端
senv sync --force-push      # 放弃远端，采用本地
```

## 构建与发布（踩坑记录）

生产镜像 `registry.wii.pub/senv/senv-server:<tag>` 的 Dockerfile 在部署机上
（`tcbj:/home/ubuntu/.agent-deploy/tcbj/senv-server/Dockerfile`，senv 仓内没有），
构建上下文 = senv 源码仓根。

**构建优先在 iship 上做**（192.168.6.3，`wii`）：其上已有 senv 仓 checkout
（`~/code/repos/github/solo-kingdom/senv`）和 docker，且 `proxy.golang.org` 可达，
`go mod download` 开箱即用。流程：在 iship 的仓里 `docker build`（可同时打
`<tag>` 与 `sha-<short>` 标签）→ `docker push` → tcbj 上 `docker compose pull && up -d`。

已踩过的坑：

- **扩容同步 kind 白名单必须先发 server 镜像**：`internal/syncschema` 由 client 与 senv-server 共享。新 kind（如 `backup`/`backup_meta`）未进运行中镜像时，新 client 的整批 push 会被拒绝。发布顺序：iship 构建并推送 senv-server 镜像 → tcbj `docker compose pull && up -d` → 再发 client。
- **不要在 tcbj 上直接 docker build**：大陆云机访问不了默认 Go 模块代理
  `proxy.golang.org`，`go mod download` 必失败（2026-09-08 实测）。
- **`--trust-proxy-headers` 旧语义（<0.1.23）只认 loopback 对端**：docker 网桥反代
  （caddy 容器 → senv-server 容器）的对端是 172.18.0.0/16 网桥地址，只加 flag 不生效——
  访问日志全记成网桥 IP、按 IP 限速退化为全局共享。0.1.23 起扩展为 loopback 或私网地址。
- **查线上访问日志直接查 PG**：`docker exec tcbj-casdoor-pg psql -U casdoor -d senv`
  查 `access_log` 表（ts/ip/method/path/outcome/reason；outcome 取值
  OK/AUTH-FAILED/BLOCKED/RATE-LIMITED），或用 `senv-server admin logs` 子命令。
  日志只从 0.1.22（2026-09-08 部署）起才有。
- **Caddyfile 是单文件 bind mount**：不能 `docker cp` 覆盖（报 device or resource busy）。
  宿主机 `/home/ubuntu/.agent-deploy/tcbj/configs/caddy/Caddyfile` 就是挂载源，
  原地编辑后 `caddy validate` + `caddy reload` 优雅生效，无需重启容器。

## 运维要点

- 备份 = 备份 Postgres 库即可；用户可随时 `senv migrate from-server` 导回本地/git 仓，无锁定
- token 泄漏 → `admin revoke-token` 吊销（`revoke-token -` 可从 stdin 读 token，
  避免明文进进程列表与 Shell 历史）；库中无明文 token
- DB 泄漏的残余风险：metadata blob 含加密后的 passwordKey，可被离线爆破 vault 口令，
  由 PBKDF2 迭代次数缓解（新 vault 600k；旧 vault 经 `senv passwd` 升级）——要求强口令
- API 全部位于 `/v1/` 前缀；健康检查 `GET /healthz` 无需认证
- server 自身带读写/空闲超时、请求体上限、认证失败限速与访问日志自动保留
  （`--logs-retain-days`，默认 90 天；path 等变长字段截断入库，防超长 URL 灌爆日志表）；
  内部错误细节只写服务端日志，客户端只收到通用 `internal error`
- **单实例部署约束**：认证失败限速窗口、认证结果缓存与 last_seen 节流缓冲均为
  单实例内存态（见 ADR-0018）。serve 进程经 LISTEN/NOTIFY 接收 admin 进程的
  缓存失效广播——该机制不跨实例，横向扩多副本前必须重新设计失效（广播语义
  或多级 TTL），否则屏蔽/吊销在其他副本上会延迟到缓存 TTL（≤30s）才生效

## 加固清单（tcbj 部署 runbook）

安全审查（2026-09）后建议在运维窗口执行的加固项，按优先级排列。
第二轮 server 侧加固（2026-09）已随版本落地，无需运维操作：可信代理来源 IP
识别（`--trust-proxy-headers`）、访问日志截断与自动保留（`--logs-retain-days`，
默认 90 天）、vault 名与设备名 server 侧校验、优雅停机、`revoke-token -` stdin 传 token。

### 1. registry 加认证（供应链，P1，未完成）

`registry.wii.pub` 目前无认证（2026-09-13 复测匿名 `/v2/` 仍返回 200），wg/LAN 内任何被攻破的机器都可 push 恶意镜像。
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

### 2. PG 收紧跨库 CONNECT（信任边界，P2，已完成 2026-09-13）

senv 与 casdoor 共享 PG 实例。casdoor 角色是实例 superuser，应用层沦陷可拖走 senv
全部密文离线爆破；2026-09-13 已执行跨库 CONNECT 收紧（casdoor 因 superuser 仍可跨库，
降权/拆实例仍是远期项）：

```sql
REVOKE CONNECT ON DATABASE casdoor FROM PUBLIC;
REVOKE CONNECT ON DATABASE senv FROM PUBLIC;
GRANT  CONNECT ON DATABASE senv    TO senv;
GRANT  CONNECT ON DATABASE casdoor TO casdoor;
```

验证（已通过）：`has_database_privilege('senv','casdoor','CONNECT')` 为 false、
`('litellm','senv'/'casdoor')` 均为 false；senv 与 casdoor 应用连接正常。
回滚方式：`GRANT CONNECT ON DATABASE <db> TO PUBLIC;`

### 3. 存量文件权限一次性收紧（本机/各客户端节点）

老版本创建的 `~/.config/senv/settings.json`、部分 `.enc`、`data/`、`envs/` 目录可能是
0644/0755；新版本每次写入都会自动收紧，但存量文件在下次写入前仍宽松：

```bash
chmod -R go-rwx ~/.config/senv ~/.local/share/senv
```

### 4. 备份排除项更新（P1 配套）

- server token 现存 `~/.config/senv/server-token.json`（0600，机器本地；旧版在
  settings.json 内，升级后自动迁移），确认备份策略里它的保护级别与 vault 本体
  一致，或直接排除；`mcp-exports.json`（导出台账，含指纹）同理
- session 派生密钥缓存现在只存在于 tmpfs（`XDG_RUNTIME_DIR`），不再出现在
  `~/.local/share/senv`，备份天然不会带走；旧版本遗留的
  `~/.local/share/senv/session/` 已被新版本自动清理
  （例外：显式 `--insecure-cache` 或 Darwin 无 tmpfs 时的磁盘逃生舱会写
  `~/.cache/senv/session-*.json`，见 session-auth spec）

### 5. caddy 层配置（真实来源 IP 透传为必选项，X-Real-IP 已配置 2026-09-13）

- 真实来源 IP（**已配置并验证**）：tcbj 的 Caddyfile `senv.wii.pub` 块已含
  `header_up X-Real-IP {remote_host}`。没有这行时，caddy 不清洗客户端自带的
  X-Real-IP/XFF 头，而 server 对私网对端（caddy 容器网桥地址）采信这些头——
  公网攻击者伪造即可轮换限速窗口、污染审计 IP（修复前实测伪造 `9.9.9.9` 生效，
  修复后日志记录真实出口 IP）。部署形态变化时保持该配置：

  ```
  reverse_proxy senv-server:8080 {
      header_up X-Real-IP {remote_host}
  }
  ```

  caddy 自动附加 X-Forwarded-For，作为 X-Real-IP 缺失时的回落；两者都缺失或
  非法时 server 按代理自身 IP 计数（fail-safe）。server 的 `--trust-proxy-headers`
  仅在直连对端为 loopback 或私网地址（RFC1918/ULA，覆盖 docker 网桥反代）时
  才采信这些头，外网直连伪造无效。开启后同一私网内的其他主机也被视为可信
  代理（可借伪造头获得独立限速窗口），仅当私网对端全部可信时才应开启。
- 认证限速：server 内置按来源 IP 的失败限速（默认 30 次/分钟）；如需在边缘再加一层，
  可用 caddy 的 `rate_limit` 插件对 `/v1/` 路径限速
- 访问日志：如需排障/溯源，可在 caddy 开 access log（server 本身保持精简；
  server 自身的系统日志保留 90 天，由 `--logs-retain-days` 控制）

### 6. 客户端升级顺序

1. 所有在用机器先升级 senv 客户端（旧二进制硬编码 100k，无法解锁升级后的 vault）
2. 逐台执行 `senv passwd`（口令可不变，输两次相同新口令）完成 KDF 升级
3. 升级后首次运行会清理遗留的持久 session 缓存并要求重新解锁一次（属预期）
