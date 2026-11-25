# 分发说明改为 --force 保留未传入的 description

- target: .agents/skills/senv-cli
- mode: update
- patch: 20261125-234802-skill-add-force-keeps-description
- risk: low
- status: proposed

## Intent

文首分发约定仍写「`--force` 会清空 description，必须显式带」。当前 `skill add --force` 在未传 `--description` 时保留原说明，只有显式传入（含空串）才覆盖。把这句改成与 `internal/skill/manager.go` 和后文 `skill add` 一致，避免按文首去做时误以为必须靠重传说明才能保住索引说明，或反过来以为不传就会被清空。

非目标：不改 `skill add` 行为，不改后文已经写对的保留规则。

## Conflict check

none。后文「`--force` 替换归档时，未传入的元数据 flag 默认保留原值」与代码一致；文首是旧行为残留。

## Rationale

`AddOptions.DescriptionSet == false` 且 description 为空时沿用已有说明。文首与实现相反，分发时会误导。改动可验证：文首不再出现「会清空 description」。

## Files

- `.agents/skills/senv-cli/SKILL.md` — 文首分发 blockquote 中 `--force` 与 description 的那半句。

## Validation

- 应用前：`git apply --check --recount` 通过。
- 应用后：`git diff --check` 无空白错误；文首含「未传 `--description` 时保留原说明」；不再含「会清空 description」。
