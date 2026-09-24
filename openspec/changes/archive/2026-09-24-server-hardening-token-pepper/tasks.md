## 1. 哈希与认证路径

- [x] 1.1 store：哈希函数可注入（构造参数 pepper，空=SHA-256 旧行为）；签发路径用当前函数
- [x] 1.2 AuthenticateWithClient：HMAC 先查；未命中且 pepper 非空时回退一次 SHA-256 比对（per-token 窗口去抖防放大）；回退命中 slog 慢日志提示轮换
- [x] 1.3 认证缓存回归：键派生、TTL、LISTEN/NOTIFY 失效广播不受影响（server-auth spec 场景复测）

## 2. 接入

- [x] 2.1 main.go：env SENV_SERVER_TOKEN_PEPPER 解析并注入 store；help/文档字符串更新
- [x] 2.2 测试：双环境（有/无 pepper）签发+认证；存量兼容回退；吊销语义；失败语义无泄露回归

## 3. 收尾

- [x] 3.1 `.agents/skills/senv-cli/SKILL.md` 同步 env 说明
- [x] 3.2 `go test ./internal/server/...` 全绿；help 验证
