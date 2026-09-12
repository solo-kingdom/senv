# SSH 资产档案随同步通道分发，私钥随档案跨机

ADR-0019 把 `ssh_host`/`ssh_keypair` 留给独立评审，理由是密钥资产的同步半径未决。本次裁决：两个集合整体接入同步通道（白名单扩到九 kind），身份=别名（`hosts/`、`keypairs/` 下的文件名，grp 为空），档案 blob **含私钥本体**原样走既有加密 blob 通道——任何持有 vault 口令的机器都能取得全部私钥明文，server 仍只见密文（零知识不变式不变）。这个扩散半径与 env/text 中既有密钥的半径相同，没有变差；不拆分公私钥（新机器「能看不能用」是半残状态），不设 per-kind 同步开关（九个 kind 语义统一，真有需求时后加开关是向后兼容的）。同时修正 ADR-0019 的措辞口径：「凭据本体不出机」只对档案 blob 成立——`llm-keys` 凭据与被 `{{text:...}}`/`{{env:...}}` 引用的条目本就随 text/env 通道同步，真正的边界是「档案 blob 不内嵌凭据」；`ssh_keypair` 的私钥是档案的组成部分，随 blob 走，与该口径一致。与 ADR-0001 的边界不变：`~/.ssh/senv/` 下的落盘文件是导出产物（本机状态），不同步、不被同步触碰，新机器同步到 keypair 档案后需显式 materialize。`host export` 对 `IdentityKey` 引用不在本机 vault 的 keypair 逐条 warning 但照常写出（与 ADR-0019 中 `mcp export` 的宽松写入同模式）；冲突报告对两个 kind 只渲染元数据对照（别名/revision/删除状态/大小），不解码明文——`KeyPairEntry` 含私钥，任何渲染文本不得出现私钥明文（与 ADR-0005 的凭据渲染红线同源）。

## Considered Options

- **只同步 hosts，keypair 留本机**：rejected——新机器仍要逐个重导私钥，host 的 `IdentityFile` 引用悬空，「SSH 资产多机可用」只完成一半。
- **keypair 拆分：公钥/指纹同步、私钥不出机**：rejected——新机器能看不能用，语义最拧巴；真要不跨机的私钥就不该放进 vault。
- **per-kind 同步开关**：rejected——目前没有任何 kind 有开关，SSH 不该特殊化；后加开关是向后兼容的。

## Consequences

- 新机器凭 vault 口令即可取用全量 SSH 资产：host/keypair 档案自动落地，materialize 后即可用；`~/.ssh/senv/` 的既有落盘文件不被同步触碰。
- 发布顺序：server 必须先升级。白名单由 client 与 server 共享的 syncschema 强制（`internal/server/store/store.go` 在事务前验证完整批次），新 client 向旧 server push 携带新 kind 的批次会被**整批拒绝**，连带 env/text 一起同步失败；反向（旧 client + 新 server）无影响，旧 client 只是收集不到新 kind、不损坏本地状态。senv-server 无代码改动，重建镜像即随新白名单生效（构建发布流程见 `docs/senv-server.md`，优先 iship）。
- host 先到、keypair 未到是常态时序：export 的逐条 warning 把悬空引用提前到导出时可见；materialize 的 fail-closed 报错带 keypair 名（现状已如此）。
- 冲突裁决所需信息（哪个别名、两侧 revision/删除状态）元数据已足够；要看明文内容用 `senv ssh host get` / `keypair list` 自行完成。
- TUI 无需改动：ssh tab 与 ai/mcp tab 同走通用 reload，同步落地后自动刷新。
