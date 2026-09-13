package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

// TestE2ESSHAssetProfiles 端到端（ADR-0020）：机器 A 导入 keypair、添加 host 并推送 →
// 机器 B（全新）首次 pull 拉到 SSH 资产档案 → 双端改同一 host 产生冲突 → --accept-remote 以远端重建。
func TestE2ESSHAssetProfiles(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "e2e-ssh-assets"

	// 机器 A：本地 vault + 一把 keypair + 一个引用它的 host
	cfgA, dataA, keyA := newLocalVault(t, password)
	smA := storage.NewManager(cfgA, dataA)
	now := time.Now()
	if err := smA.SaveKeyPairWithKey("deploy-key", &storage.KeyPairEntry{
		Name: "deploy-key", PrivateKey: "TEST-PRIVATE-KEY-MATERIAL",
		ImportedAt: now,
	}, keyA); err != nil {
		t.Fatalf("A SaveKeyPairWithKey: %v", err)
	}
	if err := smA.SaveHostWithKey("web-prod", &storage.HostEntry{
		Alias: "web-prod", Hostname: "web.example.com", User: "deploy",
		IdentityKey: "deploy-key", UpdatedAt: now,
	}, keyA); err != nil {
		t.Fatalf("A SaveHostWithKey: %v", err)
	}
	pA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A initial sync: %v", err)
	}

	// 机器 B：全新 bootstrap 接入，SSH 资产档案随首次 pull 落地
	cfgB, dataB := t.TempDir(), t.TempDir()
	pB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	if err := pB.Bootstrap(ctx); err != nil {
		t.Fatalf("B bootstrap: %v", err)
	}
	smB := storage.NewManager(cfgB, dataB)
	keyB := deriveKey(t, smB, password)
	kp, err := smB.LoadKeyPairWithKey("deploy-key", keyB)
	if err != nil {
		t.Fatalf("B LoadKeyPairWithKey: %v", err)
	}
	if kp.PrivateKey != "TEST-PRIVATE-KEY-MATERIAL" {
		t.Errorf("B keypair private key = %q", kp.PrivateKey)
	}
	host, err := smB.LoadHostWithKey("web-prod", keyB)
	if err != nil {
		t.Fatalf("B LoadHostWithKey: %v", err)
	}
	if host.Hostname != "web.example.com" || host.IdentityKey != "deploy-key" {
		t.Errorf("B host = %+v", host)
	}
	// list 视角可见（与 senv ssh host list / keypair list 同一来源）
	hosts, err := smB.ListHosts()
	if err != nil || len(hosts) != 1 || hosts[0] != "web-prod" {
		t.Errorf("B ListHosts = %v, err = %v", hosts, err)
	}
	keypairs, err := smB.ListKeyPairs()
	if err != nil || len(keypairs) != 1 || keypairs[0] != "deploy-key" {
		t.Errorf("B ListKeyPairs = %v, err = %v", keypairs, err)
	}

	// 冲突：A 与 B 基于同一 revision 各自修改同一 host
	now2 := time.Now()
	if err := smA.SaveHostWithKey("web-prod", &storage.HostEntry{
		Alias: "web-prod", Hostname: "a.web.example.com", User: "deploy",
		IdentityKey: "deploy-key", UpdatedAt: now2,
	}, keyA); err != nil {
		t.Fatalf("A update host: %v", err)
	}
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A second sync: %v", err)
	}
	if err := smB.SaveHostWithKey("web-prod", &storage.HostEntry{
		Alias: "web-prod", Hostname: "b.web.example.com", User: "deploy",
		IdentityKey: "deploy-key", UpdatedAt: now2,
	}, keyB); err != nil {
		t.Fatalf("B update host: %v", err)
	}
	_, err = pB.SyncWithReport(ctx)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("B sync err = %v, want SyncConflictError", err)
	}
	found := false
	for _, c := range conflictErr.Conflicts {
		if c.Kind == KindSSHHost {
			found = true
		}
	}
	if !found {
		t.Errorf("conflict list missing ssh_host entry: %+v", conflictErr.Conflicts)
	}

	// --accept-remote：以远端为准重建，B 的 host 回到 A 的版本
	if err := pB.AcceptRemote(ctx); err != nil {
		t.Fatalf("B accept-remote: %v", err)
	}
	host, err = smB.LoadHostWithKey("web-prod", keyB)
	if err != nil {
		t.Fatalf("B load after accept-remote: %v", err)
	}
	if host.Hostname != "a.web.example.com" {
		t.Errorf("B host after accept-remote = %q, want remote version", host.Hostname)
	}
}
