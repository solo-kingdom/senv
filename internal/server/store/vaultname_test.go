// vault 名 server 侧校验：可移植路径段规则 + 长度上限（纯函数，无需数据库）。
package store

import (
	"strings"
	"testing"
)

func TestValidateVaultName(t *testing.T) {
	cases := []struct {
		name    string
		vault   string
		wantErr bool
	}{
		{"默认名", "main", false},
		{"常规字符", "prod-eu_01", false},
		{"UTF-8 名称", "生产环境", false},
		{"空名", "", true},
		{"路径穿越", "..", true},
		{"当前目录", ".", true},
		{"斜杠", "a/b", true},
		{"反斜杠", `a\b`, true},
		{"冒号（Windows 盘符语义）", "a:b", true},
		{"NULL 字节", "a\x00b", true},
		{"恰好上限 128 字节", strings.Repeat("a", MaxVaultNameLen), false},
		{"超限 129 字节", strings.Repeat("a", MaxVaultNameLen+1), true},
		{"多字节名称按字节计长", strings.Repeat("中", 42) + "ab", false}, // 42×3+2=128 恰好上限
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVaultName(tc.vault)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateVaultName(%q) error = %v, wantErr %v", tc.vault, err, tc.wantErr)
			}
		})
	}
}
