package llm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPointerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agent-pointers.json")

	if _, err := LoadPointers(path); !errors.Is(err, ErrPointerNotFound) {
		t.Fatalf("LoadPointers(missing) error = %v, want ErrPointerNotFound", err)
	}

	pf := &PointerFile{}
	pf.Set("claude-code", "p1", []string{"m1", "m2"}, "m2")
	pf.Set("opencode", "p2", []string{"m3"}, "m3")
	if err := SavePointers(path, pf); err != nil {
		t.Fatalf("SavePointers() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != pointerFileMode {
		t.Fatalf("perm = %o, want %o", got, pointerFileMode)
	}

	loaded, err := LoadPointers(path)
	if err != nil {
		t.Fatalf("LoadPointers() error = %v", err)
	}
	p, ok := loaded.Get("claude-code")
	if !ok {
		t.Fatal("Get(claude-code) not found after round trip")
	}
	if p.Provider != "p1" || p.DefaultModel != "m2" {
		t.Fatalf("pointer = %+v, want provider p1 default m2", p)
	}
	// Agent 模型集保序落盘。
	if len(p.Models) != 2 || p.Models[0] != "m1" || p.Models[1] != "m2" {
		t.Fatalf("Models = %v, want [m1 m2]", p.Models)
	}
	if raw := string(mustRead(t, path)); !strings.Contains(raw, "\"models\"") || strings.Contains(raw, "\"model\"") {
		t.Fatalf("pointer file should carry models and not the legacy model field: %s", raw)
	}
	if _, err := p.SwitchedAtTime(); err != nil {
		t.Fatalf("SwitchedAtTime() error = %v", err)
	}
	if _, ok := loaded.Get("unknown"); ok {
		t.Fatal("Get(unknown) unexpectedly found")
	}
}

func TestPointerCorrupt(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"bad json":    `{"version":1,`,
		"bad version": `{"version":99,"agents":{}}`,
		"bad time":    `{"version":1,"agents":{"a":{"provider":"p","model":"m","switched_at":"nope"}}}`,
	}
	for name, content := range cases {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := LoadPointers(path); !errors.Is(err, ErrPointerCorrupt) {
			t.Fatalf("%s: LoadPointers() error = %v, want ErrPointerCorrupt", name, err)
		}
	}
}

func TestPointerSaveKeepsOldOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-pointers.json")
	pf := &PointerFile{}
	pf.Set("claude-code", "p1", []string{"m1"}, "m1")
	if err := SavePointers(path, pf); err != nil {
		t.Fatalf("SavePointers() error = %v", err)
	}

	// 目录替换为文件后 MkdirAll 失败，旧文件必须原样保留。
	if err := SavePointers(filepath.Join(path, "x", "p.json"), &PointerFile{}); err == nil {
		t.Fatal("SavePointers(nested under file) unexpectedly succeeded")
	}
	loaded, err := LoadPointers(path)
	if err != nil {
		t.Fatalf("LoadPointers() error = %v", err)
	}
	if _, ok := loaded.Get("claude-code"); !ok {
		t.Fatal("old pointer file was modified by failed save")
	}
}

func TestPointerSetTimestamps(t *testing.T) {
	pf := &PointerFile{}
	before := time.Now()
	pf.Set("a", "p", []string{"m"}, "m")
	p1, _ := pf.Get("a")
	ts1, err := p1.SwitchedAtTime()
	if err != nil {
		t.Fatalf("SwitchedAtTime() error = %v", err)
	}
	if ts1.Before(before.Add(-time.Second)) || ts1.After(time.Now().Add(time.Second)) {
		t.Fatalf("timestamp %v outside plausible window", ts1)
	}
}
