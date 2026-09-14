package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnexportStateNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	registered, fragments, err := UnexportState()
	if err != nil {
		t.Fatalf("missing config must not error: %v", err)
	}
	if registered || fragments != 0 {
		t.Fatalf("registered=%v fragments=%d, want false/0", registered, fragments)
	}
}

func TestUnexportStateRegisteredNoFragments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := RegisterInclude(); err != nil {
		t.Fatal(err)
	}
	registered, fragments, err := UnexportState()
	if err != nil {
		t.Fatal(err)
	}
	if !registered || fragments != 0 {
		t.Fatalf("registered=%v fragments=%d, want true/0", registered, fragments)
	}
}

func TestUnexportStateFragmentsNoRegister(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	gdir := senvPath(t, "groups")
	if err := os.MkdirAll(gdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "prod.conf"), []byte("Host web\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "notes.txt"), []byte("ignore"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered, fragments, err := UnexportState()
	if err != nil {
		t.Fatal(err)
	}
	if registered || fragments != 1 {
		t.Fatalf("registered=%v fragments=%d, want false/1 (non-.conf ignored)", registered, fragments)
	}
}

func TestUnexportStateBothPresent(t *testing.T) {
	mgr := newApplyFixture(t)
	if _, err := mgr.Apply(RenderFilter{}); err != nil {
		t.Fatal(err)
	}
	registered, fragments, err := UnexportState()
	if err != nil {
		t.Fatal(err)
	}
	if !registered || fragments != 2 {
		t.Fatalf("registered=%v fragments=%d, want true/2", registered, fragments)
	}
}

func TestUnexportStateUnreadableConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".ssh", "config")
	// 把 config 做成目录，ReadFile 必失败——比 chmod 000 更不依赖 uid。
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	registered, fragments, err := UnexportState()
	if err == nil {
		t.Fatal("unreadable config must error")
	}
	if !strings.Contains(err.Error(), "read ssh config") {
		t.Fatalf("error = %v, want read ssh config", err)
	}
	if registered || fragments != 0 {
		t.Fatalf("hard failure must not report state: registered=%v fragments=%d", registered, fragments)
	}
}

func TestUnexportStateDoesNotMutate(t *testing.T) {
	mgr := newApplyFixture(t)
	if _, err := mgr.Apply(RenderFilter{}); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(mustHome(t), ".ssh", "config")
	before, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UnexportState(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(cfg)
	if err != nil || string(after) != string(before) {
		t.Fatalf("UnexportState must not rewrite ssh config")
	}
	if _, err := os.Stat(senvPath(t, "groups", "prod.conf")); err != nil {
		t.Fatalf("UnexportState must not remove fragments: %v", err)
	}
}
