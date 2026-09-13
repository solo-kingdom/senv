# 0021-server-token-machine-local-file

server provider 的 Bearer token 存储 `<configPath>/server-token.json`（0600，
机器本地），不再写入 settings.json 的 provider 字段（该字段降级为兼容旧版的
只读迁移来源）。

背景（2026-09 数据安全 review）：git provider 的同步语义是仓库根
`git add .`，而默认布局下仓库根就是 configPath（`~/.config/senv`）——
settings.json 会被提交推送。旧版把 token 写进 settings.json，注释虽声称
"Machine-local, never synced"，但该性质只对 server provider 的条目同步成立，
对 git provider 完全不成立：git 模式机器上注册过 server（或从 server 迁回
git）后，任何一次 git 同步都会把 token 推到 git 远端，token 等价于 vault
密文的完整读取凭证。

决策与防线（三层）：

1. token 单独存放 `server-token.json`，settings.json 只保留 provider 的
   非敏感字段（type/address/vault）；register/init 写新位置，读取路径
   遇到旧版 settings 内嵌 token 时自动迁移（best-effort，失败回退旧值）。
2. `senv init` 与 token 写入自动生成/补写 `.gitignore`（覆盖
   `server-token.json` 与 `mcp-exports.json`，后者是导出台账指纹）。
3. `git add` 固定携带 `:(exclude,glob)**/server-token.json` 等排除路径
   规格——即使用户自建仓库、`.gitignore` 缺失也不暂存这些文件。

已知残余：旧 token 值可能已存在于 git 历史（无法从代码层清除）；曾在
git 模式下使用过 server 模式的机器应轮换 token（server 侧
`admin revoke-token` 后重新注册）。
