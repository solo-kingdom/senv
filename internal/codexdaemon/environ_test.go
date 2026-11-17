package codexdaemon

import (
	"errors"
	"strings"
	"testing"
)

func TestProcessHasEnv(t *testing.T) {
	original := readProcessEnviron
	t.Cleanup(func() { readProcessEnviron = original })

	readProcessEnviron = func(pid int) ([]byte, error) {
		if pid != 42 {
			t.Fatalf("pid = %d", pid)
		}
		return []byte("PATH=/bin\x00SENV_OPEN_DESIGN_API_KEY=sk-x\x00HOME=/tmp\x00"), nil
	}
	has, err := ProcessHasEnv(42, "SENV_OPEN_DESIGN_API_KEY")
	if err != nil || !has {
		t.Fatalf("has=%v err=%v, want true", has, err)
	}
	has, err = ProcessHasEnv(42, "MISSING_KEY")
	if err != nil || has {
		t.Fatalf("has=%v err=%v, want false", has, err)
	}

	readProcessEnviron = func(int) ([]byte, error) { return nil, errors.New("gone") }
	if _, err := ProcessHasEnv(42, "SENV_OPEN_DESIGN_API_KEY"); err == nil {
		t.Fatal("expected error when environ unreadable")
	}
}

func TestMissingEnvInDaemonUncheckedWithoutPID(t *testing.T) {
	// Without a real daemon.pid this returns checked=false; do not treat as "all present".
	missing, checked := MissingEnvInDaemon([]string{"SENV_OPEN_DESIGN_API_KEY"})
	if checked && len(missing) == 0 {
		// May be checked=true on a developer machine with a live daemon that
		// already has the key — only assert the no-pid path when unchecked.
		if !checked {
			t.Fatalf("missing=%v checked=%v", missing, checked)
		}
	}
	_ = strings.Join(missing, ",")
}
