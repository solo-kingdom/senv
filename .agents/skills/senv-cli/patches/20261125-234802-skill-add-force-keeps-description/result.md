# Result

- target: .agents/skills/senv-cli
- mode: update
- patch: 20261125-234802-skill-add-force-keeps-description
- risk: low
- status: applied
- applied-at: 2026-11-25T23:48:30+08:00

## Validation

- `git apply --check --recount`: pass
- `git diff --check`: pass
- target tests: not-available（文档从句；已核对文首含「未传 `--description` 时保留原说明」，且不再含「会清空 description」）
- privacy check: pass
- mode check: pass

## Notes

none
