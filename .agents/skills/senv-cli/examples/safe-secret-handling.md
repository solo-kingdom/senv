# 安全取密钥：可执行配方

背景：`senv env list` 与 MCP `senv_env_list` 的输出**默认含明文值**（list 无只列键名开关）。
以下配方对应 SKILL.md「输出默认含明文值」三条硬规则，可直接复制改组名/key 使用。

## ① 只要键名，不碰值

```bash
# 列有哪些 env 组
senv env group list

# 列某组内有哪些 key（只留行首键名，值被 grep 截掉，永不进会话）
senv env list -g prod | grep -oE '^[A-Z0-9_]+'

# text / backup 的 list 本身不含值，可直接看
senv text list -g notes
senv backup list notes
```

## ② 取值：留在变量内，同一命令消费完

```bash
# 取值 + 立即消费，不回显
TOKEN="$(senv env get ai:OPENAI_API_KEY)" && curl -sS -H "Authorization: Bearer $TOKEN" https://api.openai.com/v1/models

# 需要判断格式时，只打印派生信息
echo "len=${#TOKEN}"
printf '%s\n' "$TOKEN" | grep -oE '^[A-Za-z0-9_-]+' | head -c 4   # 掩码后前缀：只露形态
```

掩码必须同时覆盖两种值块形态（URI 内嵌凭据 + 多行「一行一个字段」）：

```bash
# 形态 A：URI 内嵌凭据  postgres://user:pass@db.internal:5432/app
# 形态 B：多行块        username: alice\npassword: s3cret…
printf '%s\n' "$VALUE" | sed -E \
  -e 's#(://[^:/@]+:)[^@[:space:]]+@#\1***@#g' \
  -e 's#^((user(name)?|pass(word)?|token|secret|api[_-]?key|secret[_-]?key|access[_-]?key|authorization|credential)s?[[:space:]]*[:=][[:space:]]*).*$#\1***#i'
```

**掩码前先确认值块是哪种形态**；不确定就只打印 `len` 与字段名（`grep -oE '^[A-Za-z_]+:'`），不打印值。

## ③ 疑似凭据已进入输出时

1. 立即停止继续打印相关输出；
2. 本轮结束前向用户报告：哪个命令、哪个分组/key 疑似外泄；
3. 建议轮换该凭据，并在轮换前把该 key 视为已泄露（不再次回显验证）。
