package cmd

import "testing"

// TestGetSyncProviderSingletonPerConfig 验证同一份配置复用同一 provider 实例
// （进程内连接复用的根基）；不同配置各得各的实例，测试间互不污染。
func TestGetSyncProviderSingletonPerConfig(t *testing.T) {
	cfg := t.TempDir()
	prevCfg := configPathFn
	configPathFn = func() string { return cfg }
	t.Cleanup(func() { configPathFn = prevCfg })

	p1, err := getSyncProvider()
	if err != nil {
		t.Fatalf("first getSyncProvider: %v", err)
	}
	p2, err := getSyncProvider()
	if err != nil {
		t.Fatalf("second getSyncProvider: %v", err)
	}
	if p1 != p2 {
		t.Fatal("同一配置应复用同一 provider 实例")
	}

	// 换一份配置（不同 config path）：得到不同实例
	cfg2 := t.TempDir()
	configPathFn = func() string { return cfg2 }
	p3, err := getSyncProvider()
	if err != nil {
		t.Fatalf("third getSyncProvider: %v", err)
	}
	if p3 == p1 {
		t.Fatal("不同配置不应复用实例")
	}
}
