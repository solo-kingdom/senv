package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// newApplyFixture 造出 prod 组（web→prod-key）+ 未分组 api 的最小 vault，
// HOME 指向临时目录，返回 manager。
func newApplyFixture(t *testing.T) *Manager {
	t.Helper()
	mgr, _ := newTestSSHManager(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyPath := writePrivateKey(t, t.TempDir(), ed25519Private(t), "apply@test", "")
	if _, err := mgr.ImportKeyPairWithGroup("prod-key", keyPath, "prod", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "web", Hostname: "10.0.0.1", Group: "prod", IdentityKey: "prod-key"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddHost(&storage.HostEntry{Alias: "api", Hostname: "10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func senvPath(t *testing.T, elems ...string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{home, ".ssh", "senv"}, elems...)...)
}

func TestApplyWritesFragmentsKeysAndInclude(t *testing.T) {
	mgr := newApplyFixture(t)

	res, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 2 {
		t.Fatalf("written = %v, want 2 group fragments", res.Written)
	}
	if !res.Registered {
		t.Fatal("include must be registered on first apply")
	}
	for _, p := range []string{senvPath(t, "groups", "prod.conf"), senvPath(t, "groups", "_ungrouped.conf")} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("fragment %s: %v, mode %o", p, err, info.Mode().Perm())
		}
	}
	prod, err := os.ReadFile(senvPath(t, "groups", "prod.conf"))
	if err != nil || !strings.Contains(string(prod), "Host web\n") {
		t.Fatalf("prod fragment:\n%s", prod)
	}
	keyInfo, err := os.Stat(senvPath(t, "keys", "prod", "prod-key"))
	if err != nil || keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("materialized key: %v", err)
	}
	summary, err := mgr.GetKeyPairSummary("prod-key")
	if err != nil {
		t.Fatal(err)
	}
	assertPublicCompanion(t, senvPath(t, "keys", "prod", "prod-key"), summary.PublicKey)
	cfg, err := os.ReadFile(filepath.Join(mustHome(t), ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(cfg), IncludeLine+"\n") {
		t.Fatalf("ssh config must start with the include line:\n%s", cfg)
	}
	// 新建场景无备份可留；对既有文件的修改才留 .senv-bak（见 idempotence 用例）。
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestApplyIsIdempotent(t *testing.T) {
	mgr := newApplyFixture(t)
	first, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Materialized) != 0 {
		t.Fatalf("second apply must not rematerialize: %v", second.Materialized)
	}
	if len(second.KeysSkipped) != len(first.Materialized) {
		t.Fatalf("skipped = %v, want first materialized %v", second.KeysSkipped, first.Materialized)
	}
	if second.Registered || !second.IncludeExisted {
		t.Fatalf("include must already exist: %+v", second)
	}
	cfg, err := os.ReadFile(filepath.Join(mustHome(t), ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(cfg), IncludeLine) != 1 {
		t.Fatalf("duplicate include lines:\n%s", cfg)
	}
	// 既有文件的修改留 .senv-bak：撤回后重新注册触发备份。
	unregistered, _, err := mgr.Unexport()
	if err != nil || !unregistered {
		t.Fatalf("unexport for backup cycle: %v, %v", unregistered, err)
	}
	if _, err := mgr.Apply(RenderFilter{Group: "prod"}); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(filepath.Join(mustHome(t), ".ssh", "config.senv-bak"))
	if err != nil {
		t.Fatalf("backup must be retained after modifying existing config: %v", err)
	}
	if strings.Contains(string(bak), IncludeLine) {
		t.Fatalf("backup must hold the pre-modification content:\n%s", bak)
	}
}

func TestApplyFillsMissingPublicCompanion(t *testing.T) {
	mgr := newApplyFixture(t)
	first, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Materialized) != 1 || first.Materialized[0] != "prod-key" {
		t.Fatalf("first materialized = %v, want prod-key", first.Materialized)
	}
	priv := senvPath(t, "keys", "prod", "prod-key")
	pub := priv + ".pub"
	if err := os.Remove(pub); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Materialized) != 0 {
		t.Fatalf("must not rematerialize private key: %v", second.Materialized)
	}
	after, err := os.ReadFile(priv)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing private key must not be overwritten when filling .pub")
	}
	summary, err := mgr.GetKeyPairSummary("prod-key")
	if err != nil {
		t.Fatal(err)
	}
	assertPublicCompanion(t, priv, summary.PublicKey)
}

func TestApplyGroupScopeAndGhostPrune(t *testing.T) {
	mgr := newApplyFixture(t)
	if _, err := mgr.Apply(RenderFilter{}); err != nil {
		t.Fatal(err)
	}

	// 单组导出：只重建该组文件，不动其他组。
	res, err := mgr.Apply(RenderFilter{Group: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 1 || filepath.Base(res.Written[0]) != "prod.conf" {
		t.Fatalf("group scope written = %v", res.Written)
	}
	if len(res.Pruned) != 0 {
		t.Fatalf("group scope must not prune: %v", res.Pruned)
	}

	// 全量导出：host 与 keypair 双双改组后，旧组片段被清理、旧路径私钥变
	// 未引用（只 warning 不自动删除）。
	if err := mgr.UpdateHost("web", func(h *storage.HostEntry) error {
		h.Group = "staging"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateKeyPair("prod-key", func(e *storage.KeyPairEntry) error {
		e.Group = "staging"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err = mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pruned) != 1 || filepath.Base(res.Pruned[0]) != "prod.conf" {
		t.Fatalf("pruned = %v, want prod.conf", res.Pruned)
	}
	if _, err := os.Stat(senvPath(t, "groups", "prod.conf")); !os.IsNotExist(err) {
		t.Fatalf("stale fragment must be removed: %v", err)
	}
	if _, err := os.Stat(senvPath(t, "groups", "staging.conf")); err != nil {
		t.Fatalf("staging fragment missing: %v", err)
	}
	// 旧分组路径的私钥变未引用 → warning，不自动删除。
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "keys/prod/prod-key") && strings.Contains(w, "prune") {
			found = true
		}
	}
	if !found {
		t.Fatalf("old-path key warning missing: %v", res.Warnings)
	}
	if _, err := os.Stat(senvPath(t, "keys", "prod", "prod-key")); err != nil {
		t.Fatalf("unreferenced key must not be auto-deleted: %v", err)
	}
}

func TestApplyLegacyFlatKeyWarned(t *testing.T) {
	mgr := newApplyFixture(t)
	legacy := senvPath(t, "legacy-key")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Apply(RenderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, legacy) {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy flat key warning missing: %v", res.Warnings)
	}
}

func TestApplyRenderFailureZeroSideEffects(t *testing.T) {
	mgr, store := newTestSSHManager(t)
	t.Setenv("HOME", t.TempDir())
	if err := store.WithVaultMutation(func(locked *storage.Manager) error {
		return locked.SaveHost("web", &storage.HostEntry{Alias: "web", Hostname: "web", ProxyJump: "gone"}, "test-password")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Apply(RenderFilter{}); err == nil {
		t.Fatal("dangling proxyJump must fail apply")
	}
	if _, err := os.Stat(senvPath(t)); !os.IsNotExist(err) {
		t.Fatalf("apply failure must not create senv dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mustHome(t), ".ssh", "config")); !os.IsNotExist(err) {
		t.Fatalf("apply failure must not touch ssh config: %v", err)
	}
}

func TestApplyHostFilterRewritesWholeGroup(t *testing.T) {
	mgr := newApplyFixture(t)
	res, err := mgr.Apply(RenderFilter{Host: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 1 || filepath.Base(res.Written[0]) != "prod.conf" {
		t.Fatalf("host filter written = %v, want whole prod group fragment", res.Written)
	}
	if _, err := os.Stat(senvPath(t, "groups", "_ungrouped.conf")); !os.IsNotExist(err) {
		t.Fatalf("host filter must not write other groups: %v", err)
	}
}

func TestUnexportRemovesRegistrationAndFragmentsOnly(t *testing.T) {
	mgr := newApplyFixture(t)
	if _, err := mgr.Apply(RenderFilter{}); err != nil {
		t.Fatal(err)
	}
	unregistered, groupsRemoved, err := mgr.Unexport()
	if err != nil {
		t.Fatal(err)
	}
	if !unregistered || !groupsRemoved {
		t.Fatalf("unexport result = %v, %v", unregistered, groupsRemoved)
	}
	cfg, err := os.ReadFile(filepath.Join(mustHome(t), ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), IncludeLine) {
		t.Fatalf("include line must be removed:\n%s", cfg)
	}
	if _, err := os.Stat(senvPath(t, "groups")); !os.IsNotExist(err) {
		t.Fatalf("groups dir must be removed: %v", err)
	}
	if _, err := os.Stat(senvPath(t, "keys", "prod", "prod-key")); err != nil {
		t.Fatalf("materialized keys must survive unexport: %v", err)
	}
	// 幂等：再次撤回报告无变更。
	unregistered, groupsRemoved, err = mgr.Unexport()
	if err != nil || unregistered || groupsRemoved {
		t.Fatalf("second unexport = %v, %v, %v", unregistered, groupsRemoved, err)
	}
}
