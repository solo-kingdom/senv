# 本机默认密钥对：`_default.conf` + 随 host unexport 清除

本机默认密钥对（`Host *` + `IdentityFile`）只服务「未另行配置 Identity」的连接；个人机/工作机常需不同默认钥，故**只存本机、不进 vault、不同步**。与 host Apply 共用 `Include ~/.ssh/senv/groups/*.conf`：在 `groups/_default.conf` 写三四行即可（Host Apply 永不重渲染该文件，幽灵清理白名单跳过；Host 组名撞 `_default` 导出时报错，对称 `_ungrouped`）。真源即该文件。`host unexport` 删光 `groups/*` 时一并清掉默认——默认是本机 OpenSSH 应用状态，不另搞生命周期。未选第二条 Include，也未塞进普通组片段（会被整文件重渲染抹掉，ADR-0023）。设默认与普通落盘（用户面 `keypair export`）分开；不写 `IdentitiesOnly`。刻意保持轻量：无同步、无额外状态文件、无复杂状态机。
