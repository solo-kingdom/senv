## Why
config 当前是扁平模型（`ConfigIndex.Configs`，每条仅 name/target_path），无分组、无描述，与 env/text 的分组管理模型不对齐。私密配置需要按分组管理，并携带描述与保存位置等 meta 信息。

## What Changes
- `storage.ConfigFile` 增加 `Group`（缺省 `default`）与 `Description` 字段；旧索引数据读入时落 `default` 分组，向后兼容
- 保存位置（target path）支持 `~` 与环境变量展开：存储原始写法，使用时统一展开
- `internal/config` Manager 增加分组感知能力：按组列出、按组过滤、创建/更新时指定 group 与 description
- CLI：`config create` 增加 `--group`/`--description`；`config list/get` 展示分组与 meta；name 保持全局唯一

## Non-goals
- 不改变加密算法、密钥派生与加密文件命名（仍为 `<name>.enc`）
- 不实现 install/uninstall（属 `config-encrypt-install` / `config-encrypt-uninstall`）
- 不改 TUI（属 `config-encrypt-tui`）

## Capabilities
- `config-grouped-storage`: config 的分组与 meta 存储模型及路径展开

## 验收标准
- [ ] 配置可指定分组与描述，list/get 可见
- [ ] 旧格式索引无需手工迁移即可读写
- [ ] target path 中 `~`、`$VAR`、`${VAR}` 在使用时正确展开
