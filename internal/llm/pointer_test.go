package llm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPointerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agent-pointers.json")

	if _, err := LoadPointers(path); !errors.Is(err, ErrPointerNotFound) {
		t.Fatalf("LoadPointers(missing) error = %v, want ErrPointerNotFound", err)
	}

	pf := &PointerFile{}
	pf.Set("claude-code", "p1", "m1")
	pf.Set("opencode", "p2", "m2")
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
	if p.Provider != "p1" || p.Model != "m1" {
		t.Fatalf("pointer = %+v, want provider p1 model m1", p)
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
	pf.Set("claude-code", "p1", "m1")
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
	pf.Set("a", "p", "m")
	p1, _ := pf.Get("a")
	ts1, err := p1.SwitchedAtTime()
	if err != nil {
		t.Fatalf("SwitchedAtTime() error = %v", err)
	}
	if ts1.Before(before.Add(-time.Second)) || ts1.After(time.Now().Add(time.Second)) {
		t.Fatalf("timestamp %v outside plausible window", ts1)
	}
}
