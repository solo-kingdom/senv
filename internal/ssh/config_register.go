package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/storage"
)

// IncludeLine 是 senv 注册进 ~/.ssh/config 的唯一一行：glob 覆盖
// groups/ 下所有现有与未来组，组增删对该行零扰动（ADR-0023 D5）。
// 它同时是所有权声明——Register/Unregister 以逐行精确匹配识别 senv
// 拥有的行，用户手写等价行（空白差异）会被再加一行，重复 Include
// 同一 glob 首匹配生效，无害。
const IncludeLine = "Include ~/.ssh/senv/groups/*.conf"

func sshConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// RegisterInclude 幂等保证 ~/.ssh/config 顶部存在 IncludeLine。文件不
// 存在时创建（0600）；已存在时只插入缺失行，其余内容一字不动。写入走
// 备份 + temp+rename 原子替换；ssh config 非机密文件，备份按规约留存
// 供人工恢复（区别于 llm 凭据配置的用完即删）。
func RegisterInclude() (bool, error) {
	path, err := sshConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := storage.WriteSensitiveFile(path, []byte(IncludeLine+"\n"), 0o700, 0o600); err != nil {
			return false, fmt.Errorf("create ssh config: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read ssh config: %w", err)
	}
	if hasExactLine(existing, IncludeLine) {
		return false, nil
	}
	updated := IncludeLine + "\n" + string(existing)
	if err := writeSSHConfigAtomic(path, []byte(updated)); err != nil {
		return false, err
	}
	return true, nil
}

// UnregisterInclude 摘除 ~/.ssh/config 中的 IncludeLine；行不存在时报
// 告无变更。其余内容（含用户手写的其他 Include）一字不动。
func UnregisterInclude() (bool, error) {
	path, err := sshConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read ssh config: %w", err)
	}
	lines := strings.Split(string(existing), "\n")
	kept := lines[:0]
	removed := 0
	for _, line := range lines {
		if line == IncludeLine {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return false, nil
	}
	updated := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if err := writeSSHConfigAtomic(path, []byte(updated)); err != nil {
		return false, err
	}
	return true, nil
}

func hasExactLine(data []byte, want string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if line == want {
			return true
		}
	}
	return false
}

// writeSSHConfigAtomic 备份后原子写回 ~/.ssh/config，保持原文件权限。
func writeSSHConfigAtomic(path string, data []byte) error {
	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".senv-bak", existing, perm); err != nil {
			return fmt.Errorf("backup ssh config: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".senv-ssh-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace ssh config: %w", err)
	}
	return nil
}

// globIncludeUnsupported 尽力检测本机 OpenSSH 是否低于 7.3（glob Include
// 的起点）；ssh 缺失或输出不可解析时不告警，由文档覆盖。
func globIncludeUnsupported() bool {
	out, err := exec.Command("ssh", "-V").CombinedOutput()
	if err != nil {
		return false
	}
	s := string(out)
	idx := strings.Index(s, "OpenSSH_")
	if idx < 0 {
		return false
	}
	var major, minor int
	if _, err := fmt.Sscanf(s[idx+len("OpenSSH_"):], "%d.%d", &major, &minor); err != nil {
		return false
	}
	return major < 7 || (major == 7 && minor < 3)
}
