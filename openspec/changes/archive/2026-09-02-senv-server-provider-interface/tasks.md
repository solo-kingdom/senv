## 1. Provider 接口

- [x] 1.1 新建 `internal/provider` 包，定义窄接口（Push/Pull/Sync/Status）与配置结构
- [x] 1.2 实现配置解析与校验（git 默认；server 缺参报明确错误）

## 2. git 适配

- [x] 2.1 实现 git provider 适配层，委托现有 `internal/git.Manager`
- [x] 2.2 现有 git 命令/交互菜单改经适配层，行为不变

## 3. cmd 收敛

- [x] 3.1 统一构造入口替换 auth/doctor/git/init/session/interactive_main 等处的直接构造
- [x] 3.2 构造失败错误信息包含 provider 类型与原因

## 4. 验证

- [x] 4.1 `make check` 全绿，现有测试断言不变
- [x] 4.2 `openspec validate --strict --type change senv-server-provider-interface` 通过
