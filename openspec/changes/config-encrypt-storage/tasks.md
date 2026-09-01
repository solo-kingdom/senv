## 1. 存储模型
- [x] 1.1 `storage.ConfigFile` 增加 `Group`/`Description`（omitempty），补充类型注释
- [x] 1.2 索引读写兼容测试：旧 JSON（无新字段）读入 → default 组、空描述；新格式往返一致

## 2. 路径展开
- [x] 2.1 实现 `ResolveTargetPath`：`~` 展开 + `os.ExpandEnv` + 未定义变量/空结果报错
- [x] 2.2 `Export` 与既有 `expandHome` 调用点切换到 `ResolveTargetPath`
- [x] 2.3 单测：`~`、`$VAR`、`${VAR}`、未定义变量、混合写法

## 3. Manager 分组能力
- [x] 3.1 `Create` 增加 group/description 参数，group 空值落 default；name 唯一性校验不变
- [x] 3.2 `List`/`Get` 支持分组过滤并返回 meta；`SetMeta` 更新分组与描述
- [x] 3.3 单测：创建/列表/过滤/SetMeta/重复 name

## 4. CLI
- [x] 4.1 `config create` 增加 `--group`/`--description` flag
- [x] 4.2 `config list` 增加 `--group` 过滤并展示 description；`config get` 展示完整 meta
- [x] 4.3 `make check` 通过
