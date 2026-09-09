## Context

vault 已有 env/text/config 条目模型，存储层按 `dataPath/<集合>/<键>.enc` 逐条加密，`internal/storage` 提供原子写入、一致性检查与 rekey 基础设施。`golang.org/x/crypto/ssh` 已在 go.mod（`ParseRawPrivateKey` 等），公钥派生零新增依赖。动机见 proposal.md，术语见 CONTEXT.md「SSH 资产」，materialize 路径约定见 docs/adr/0001。

## Goals / Non-Goals

**Goals:**
- `hosts` / `keypairs` 两个新顶级集合，与 env/text 同级的加密与同步待遇
- 核心结构化字段 + 自由 KV 的 host 模型，及 host→keypair 结构化引用
- 引用一致性：写路径校验、删除保护、导出兜底校验
- export/materialize 联动与 TUI/MCP 集成

**Non-Goals:**
- 不生成密钥对；不内建 ssh 客户端；不解析 ProxyCommand；MCP 不提供私钥明文（与 proposal 非目标一致）
- 不做 materialize 文件的自动清理与状态跟踪

## Decisions

### D1 顶级集合与存储布局

`dataPath/hosts/<alias>.enc` 与 `dataPath/keypairs/<name>.enc`，逐条加密文件，与 text 同模式，复用 secure_write 与文件锁。`internal/storage/types.go` 新增 `HostEntry`、`KeyPairEntry`。
备选：挂入现有 group/key 命名空间——否决，会污染 env/text 语义并迫使 ref 解析与 TUI 加特判。

### D2 KeyPairEntry 结构

字段：name、privateKey（OpenSSH/PEM 文本整体）、publicKey（派生，可空）、fingerprint、comment、importedAt。不存源文件路径（导入后即失效）。
公钥派生：`ssh.ParseRawPrivateKey` → `ssh.MarshalAuthorizedKey`；passphrase 加密私钥不做解密尝试（无 passphrase 输入），派生失败公钥留空。

### D3 HostEntry 结构

字段：alias、hostname、user、port、proxyJump、identityKey、tags、extra（任意 KV）、updatedAt。
写路径校验：`identityKey` 存在性、`proxyJump` 指向的 alias 存在性；`extra` 只要求键非空、不含空白，不做 OpenSSH 关键字白名单。

### D4 删除与一致性策略

删除 keypair 时线性扫描 hosts 求引用集：非空即拒绝并列出；`--force` 先清空引用再删除。悬空引用主防线在写路径 + force 清空；git 合并冲突仍可能造成悬空，由 `doctor` 与 `host export` 兜底校验。

### D5 export / materialize 渲染

export 每个 host 渲染为：`Host <alias>` → `HostName`/`User`/`Port`/`ProxyJump` → `IdentityFile ~/.ssh/senv/<keypair name>` → extra KV 直传（键按 OpenSSH 惯例保持用户输入的大小写，OpenSSH 关键字大小写不敏感）。
materialize：目录不存在则创建（0700），文件写 0600，已存在默认拒绝、`--force` 覆盖（见 ADR-0001）。

### D6 CLI 形态

```
senv keypair import web-key --file ~/.ssh/id_ed25519
senv keypair list
senv keypair materialize web-key [--force]
senv keypair delete web-key [--force]

senv host add web --hostname 10.0.0.1 --user deploy --port 2222 \
  [--keypair web-key | --key-file ~/.ssh/id_ed25519 --keypair-name web-key] \
  [--proxy-jump jump1] [--attr forwardAgent=yes]...
senv host list
senv host get web
senv host edit web            # 编辑器编辑结构化字段 + extra
senv host delete web
senv host export [--host web]
```

交互式 keypair 选择复用现有 interactive_* 提示组件；`host edit` 复用编辑器流程（与 `text set`、`config edit` 同模式）。

### D7 TUI / MCP

TUI 新增 hosts/keypairs 板块，沿用 tui-viewer 的浏览与遮蔽模式；keypair 仅展示指纹、公钥摘要与元信息。MCP 注册只读工具 `ssh_host_list` / `ssh_host_get`，返回字段白名单（不含 identityKey 指向的内容、不含任何私钥材料）；不注册 keypair 内容类工具。

## 数据流

```
导入:   私钥文件 --读取--> 格式解析校验 --派生--> 公钥/指纹
             └───────────── AES-GCM 加密 ────> dataPath/keypairs/<name>.enc

建 host: CLI 参数/交互 --校验 identityKey、proxyJump--> 加密 --> dataPath/hosts/<alias>.enc

导出:   hosts/*.enc --解密--> 引用兜底校验 --> 渲染 OpenSSH config 片段 --> stdout
落地:   keypairs/<name>.enc --解密--> ~/.ssh/senv/<name>（0700/0600）

同步:   hosts/ keypairs/ --现有 vault 同步通道（密文）--> git remote / senv-server
```

## 错误处理策略

- 导入：文件不存在、格式非法 → 带路径与原因报错，不产生部分写入（先全部校验再落盘）
- 引用：`identityKey` / `proxyJump` 目标不存在 → 拒绝写入；export 遇悬空引用 → 报错并指出 alias
- 落盘：目录创建失败、权限不足 → 明确报错；目标已存在默认拒绝
- 一致性：`doctor` 报告悬空 `identityKey` / `proxyJump`（git 合并冲突兜底）

## 向后兼容

纯新增顶级目录，不改既有条目格式，无迁移、可随时回滚（删除新目录即回到旧状态）。唯一兼容风险：旧版本二进制读取新 vault 时，storage 一致性检查/repair 对未知目录的容忍度需显式验证；若误报则参照既有 `config_index_quarantine` 策略处理。

## Risks / Trade-offs

- [materialize 后私钥常驻磁盘，删除 keypair 不自动清理落盘文件] → 文档明示该行为；后续版本再评估清理策略
- [passphrase 加密私钥派生不出公钥] → list 标注 `pubkey: none`；使用时由 ssh 自行提示 passphrase
- [ProxyJump 环引用（A→B→A）] → v1 不检测，导出成功但 ssh 连接失败；后续可在 export 加环检测
- [额外 KV 可写入任意 OpenSSH 关键字（如 LocalCommand）] → 直传是有意的逃逸舱，不做黑名单；README 提示风险
- [旧版本 client 对新目录的兼容] → tasks 中加入 doctor/repair 兼容验证项

## Open Questions

（无）
