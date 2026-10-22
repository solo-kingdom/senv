# Tasks

## 1. 同步身份与 server

- [x] 1.1 syncschema 注册 `skill` / `skill_index`，补身份矩阵与路径攻击测试
- [x] 1.2 server store 按 kind 的密文上限（skill 1.5 MiB，其它 512 KiB），含经真实 store 的推送测试

## 2. 存储

- [x] 2.1 `internal/storage/skill.go`：索引类型与规范化、`SaveSkillWithKey`/`LoadSkillWithKey`/`DeleteSkill`/`ListSkillFiles`，明文 1 MiB 上限
- [x] 2.2 provider：`entryLocation` 与 `collectEntriesDiff` 覆盖 skills 与 `skill_index.json`，增量收集一致、pull 落点正确
- [x] 2.3 rekey（classify、manifest kind、结果计数）、`CheckConsistency`、`HasOrphanedData` 认识 skills

## 3. 归档与管理

- [x] 3.1 `internal/skill`：Pack / Inspect / ParseName / Validate，白名单与路径/重复/冲突校验
- [x] 3.2 `skill.Manager`：Add（含 `--force`）/ List / Get / Show / Remove

## 4. CLI 与周边

- [x] 4.1 `senv skill add|list|show|remove`（无 apply），审计 `op_skill`，doctor 输出 skills 探测
- [x] 4.2 冲突渲染与 `senv history` 对 skill / skill_index 的安全展示

## 5. 验证

- [x] 5.1 `go build ./... && go vet` 相关包，`go test ./...` 全部通过
