## 1. 接线

- [x] 1.1 抽 `applyTokenPepper(st pepperSetter)` + `pepperSetter` 最小接口（env `SENV_SERVER_TOKEN_PEPPER`，空则不配置）
- [x] 1.2 `runServe` 内联判断改为调用它（行为不变）；`withStore` 建 store 后同样调用

## 2. 收尾

- [x] 2.1 `senv-server/main_test.go`：配置 → 透传；置空 → 不调用
- [x] 2.2 `docs/senv-server.md` pepper 节补「admin CLI 同源读取，部署须给 admin 侧注入同一 pepper」
