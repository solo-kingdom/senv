package cmd

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

type fakeRichSyncServer struct {
	mu       sync.Mutex
	metadata []byte
	entries  map[string]provider.Entry
	history  map[string][]provider.HistoryVersion
	revision int64
}

func newFakeRichSyncServer() *fakeRichSyncServer {
	return &fakeRichSyncServer{
		entries: make(map[string]provider.Entry),
		history: make(map[string][]provider.HistoryVersion),
	}
}

func (s *fakeRichSyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/metadata"):
		if s.metadata == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string][]byte{"blob": s.metadata})
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/metadata"):
		var body struct {
			Blob []byte `json:"blob"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.metadata = body.Blob
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/entries/lookup"):
		var body struct {
			Keys []provider.EntryKey `json:"keys"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var found []provider.Entry
		for _, k := range body.Keys {
			id := k.Kind + "\x00" + k.Grp + "\x00" + k.Key
			if e, ok := s.entries[id]; ok {
				found = append(found, e)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": found, "latest_revision": s.revision})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/history"):
		kind := r.URL.Query().Get("kind")
		grp := r.URL.Query().Get("grp")
		key := r.URL.Query().Get("key")
		id := kind + "\x00" + grp + "\x00" + key
		hist := s.history[id]
		var out []provider.HistoryVersion
		for i := len(hist) - 1; i >= 0; i-- {
			out = append(out, hist[i])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"history": out})
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/entries"):
		since, _ := strconv64(r.URL.Query().Get("since"))
		entries := make([]provider.Entry, 0)
		for _, entry := range s.entries {
			if entry.Revision > since {
				entries = append(entries, entry)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries, "latest_revision": s.revision})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/entries"):
		var body struct {
			Entries []provider.Entry `json:"entries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var conflicts []provider.Conflict
		for _, entry := range body.Entries {
			id := entry.Kind + "\x00" + entry.Grp + "\x00" + entry.Key
			current, exists := s.entries[id]
			if exists && current.Revision != entry.BaseRevision {
				conflicts = append(conflicts, provider.Conflict{
					Kind:            entry.Kind,
					Grp:             entry.Grp,
					Key:             entry.Key,
					CurrentRevision: current.Revision,
					Deleted:         current.Deleted,
					Size:            int64(len(current.Ciphertext)),
				})
			}
		}
		if len(conflicts) > 0 {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "revision conflict", "conflicts": conflicts})
			return
		}
		acks := make([]map[string]any, 0, len(body.Entries))
		for _, entry := range body.Entries {
			id := entry.Kind + "\x00" + entry.Grp + "\x00" + entry.Key
			if prev, exists := s.entries[id]; exists {
				s.history[id] = append(s.history[id], provider.HistoryVersion{
					Kind:       prev.Kind,
					Grp:        prev.Grp,
					Key:        prev.Key,
					Ciphertext: prev.Ciphertext,
					Revision:   prev.Revision,
					Deleted:    prev.Deleted,
					ReplacedAt: time.Now(),
				})
			}
			s.revision++
			entry.Revision = s.revision
			entry.UpdatedAt = time.Now()
			s.entries[id] = entry
			acks = append(acks, map[string]any{
				"kind": entry.Kind, "grp": entry.Grp, "key": entry.Key, "revision": entry.Revision,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revisions": acks, "latest_revision": s.revision})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type testClientNode struct {
	configPath string
	dataPath   string
	password   string
	store      *storage.Manager
	key        []byte
	sp         *provider.ServerProvider
	skillMgr   *skill.Manager
}

func setupSyncNodeFromExisting(t *testing.T, srvURL, password string, initVault bool) *testClientNode {
	t.Helper()
	cfg := t.TempDir()
	data := t.TempDir()
	st := storage.NewManager(cfg, data)
	if initVault {
		if err := st.Initialize(password); err != nil {
			t.Fatal(err)
		}
		settings, _ := st.LoadSettings()
		settings.Provider = storage.ProviderConfig{
			Type: provider.TypeServer, Address: srvURL, Token: "test-token", Vault: "main",
		}
		if err := st.SaveSettings(settings); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := storage.EnsurePrivateDir(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := storage.EnsurePrivateDir(data, 0o700); err != nil {
			t.Fatal(err)
		}
		settings := storage.Settings{DefaultGroup: "default"}
		settings.Provider = storage.ProviderConfig{
			Type: provider.TypeServer, Address: srvURL, Token: "test-token", Vault: "main",
		}
		if err := st.SaveSettings(&settings); err != nil {
			t.Fatal(err)
		}
	}
	sp := provider.NewServerProvider(srvURL, "test-token", cfg, data, "main")
	if initVault {
		if err := sp.Push("init"); err != nil {
			t.Fatalf("init push: %v", err)
		}
	} else {
		if err := sp.Bootstrap(context.Background()); err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
	}
	key, err := st.DeriveKeyFromPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return &testClientNode{
		configPath: cfg,
		dataPath:   data,
		password:   password,
		store:      st,
		key:        key,
		sp:         sp,
		skillMgr:   skill.NewManagerWithKey(st, key),
	}
}

func clearTestProviderCache() {
	providerMu.Lock()
	providerCache = map[providerCacheKey]provider.Provider{}
	providerMu.Unlock()
}

func useNodeAsActiveCLI(t *testing.T, node *testClientNode) {
	t.Helper()
	prevCfg := configPathFn
	prevData := dataPath
	configPathFn = func() string { return node.configPath }
	dataPath = node.dataPath
	clearTestProviderCache()
	t.Cleanup(func() {
		configPathFn = prevCfg
		dataPath = prevData
		clearTestProviderCache()
		syncResolve = nil
		syncResolveDefault = ""
		syncNoHealEquivalent = false
		syncNoInteractive = false
		strictSync = false
	})
	sessMgr := session.NewManager(node.configPath, node.dataPath)
	defer sessMgr.Close()
	if err := sessMgr.StartSession(node.password, &session.SessionTimeout{Type: session.TimeoutDuration, Value: time.Hour}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
}

func makeSkillDir(t *testing.T, id, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + id + "\ndescription: test\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func buildLegacyRawTarForSkill(t *testing.T, id, body string) []byte {
	t.Helper()
	md := "---\nname: " + id + "\ndescription: test\n---\n" + body + "\n"
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "SKILL.md",
		Mode:     0o644,
		Size:     int64(len(md)),
		Format:   tar.FormatUSTAR,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(md)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestE2E_PreWritePull_Diff_Export_EquivalentHeal_Resolve_Merge_Rebase(t *testing.T) {
	t.Setenv("SENV_ALLOW_INSECURE_HTTP", "1")
	password := "e2e-mechanism-pass"
	srv := newFakeRichSyncServer()
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// 初始化节点 A（类似 msa）并写入初始 skill（butler@v5 + old1..old3）
	nodeA := setupSyncNodeFromExisting(t, httpSrv.URL, password, true)
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "v5-base"), Group: "base", Version: "v5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "old1", SourceDir: makeSkillDir(t, "old1", "old1-body"), Group: "on-demand", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "equiv", SourceDir: makeSkillDir(t, "equiv", "same-body"), Group: "base", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("seed"); err != nil {
		t.Fatal(err)
	}

	// 节点 B（类似 iship）与节点 Offline 从服务端拉取同一基线
	nodeB := setupSyncNodeFromExisting(t, httpSrv.URL, password, false)
	nodeOffline := setupSyncNodeFromExisting(t, httpSrv.URL, password, false)

	// --- Scenario 1 & 4: 写前 preWriteAutoPull + --strict-sync 早停 + 自适应预算 ---
	if budget := provider.AutoPushBudgetForBytes(512 * 1024); budget <= 2*time.Second || budget > 15*time.Second {
		t.Fatalf("AutoPushBudgetForBytes(512KiB) = %v, want (2s, 15s]", budget)
	}
	if budget := provider.AutoPushBudgetForBytes(10 * 1024 * 1024); budget != 15*time.Second {
		t.Fatalf("AutoPushBudgetForBytes(10MiB) = %v, want 15s cap", budget)
	}

	// A 推送 butler@v6，并把 old1、equiv 重加密/重压推到远端
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "remote-v6"), Group: "base", Version: "v6"}); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "old1", SourceDir: makeSkillDir(t, "old1", "old1-body"), Group: "on-demand", Force: true, Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-push-v6"); err != nil {
		t.Fatal(err)
	}

	// 在线写前 preWriteAutoPull 应自动拉齐远端新增（先另建一个干净的 nodeC 验证在线写前拉取防撞车）
	nodeC := setupSyncNodeFromExisting(t, httpSrv.URL, password, false)
	// 先让 A 推送一个新 skill online1
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "online1", SourceDir: makeSkillDir(t, "online1", "from-A"), Group: "base", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-push-online1"); err != nil {
		t.Fatal(err)
	}
	useNodeAsActiveCLI(t, nodeC)
	resetPullThrottle(t, nodeC)
	parentCmd := &cobra.Command{Use: "skill"}
	preCmd := &cobra.Command{Use: "add"}
	parentCmd.AddCommand(preCmd)
	var preErr bytes.Buffer
	preCmd.SetErr(&preErr)
	if err := preWriteAutoPull(preCmd, nil); err != nil {
		t.Fatalf("clean preWriteAutoPull failed: %v", err)
	}
	if _, err := nodeC.skillMgr.Show("online1"); err != nil {
		t.Fatalf("preWriteAutoPull should have pulled online1 before write: %v", err)
	}

	// B 在本地先改一个未推送的 old1（制造 dirty），再开启 strict-sync 跑写前钩子应早停
	if _, err := nodeB.skillMgr.Add(skill.AddOptions{ID: "old1", SourceDir: makeSkillDir(t, "old1", "local-dirty"), Group: "on-demand", Force: true, Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	useNodeAsActiveCLI(t, nodeB)
	resetPullThrottle(t, nodeB)
	strictSync = true
	parentCmd2 := &cobra.Command{Use: "env"}
	writeCmd := &cobra.Command{Use: "set"}
	parentCmd2.AddCommand(writeCmd)
	var stderrBuf bytes.Buffer
	writeCmd.SetErr(&stderrBuf)
	err := preWriteAutoPull(writeCmd, nil)
	strictSync = false
	if !errors.Is(err, ErrSyncConflictPending) {
		t.Fatalf("strict-sync preWriteAutoPull err = %v, want ErrSyncConflictPending", err)
	}

	// --- Scenario 2: nodeOffline 离线批量制造 3 类冲突：
	//   1) equiv: 裸 tar 内容等价于远端（等价假冲突）
	//   2) old1: nodeOffline 删除 old1（撞上 A 刚重压的 old1）
	//   3) butler@v6: nodeOffline 也写了不同内容的 butler@v6（撞上 A 的 butler@v6）
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "equiv", SourceDir: makeSkillDir(t, "equiv", "same-body"), Group: "base", Force: true, Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-repack-equiv"); err != nil {
		t.Fatal(err)
	}

	useNodeAsActiveCLI(t, nodeOffline)
	// nodeOffline 本地写入裸 tar 版 equiv（同内容不同格式）
	rawEquiv := buildLegacyRawTarForSkill(t, "equiv", "same-body")
	if err := nodeOffline.store.SaveSkillWithKey("base", "equiv", rawEquiv, nodeOffline.key); err != nil {
		t.Fatal(err)
	}
	// nodeOffline 本地删除 old1，且本地写入不同内容的 butler@v6
	if err := nodeOffline.skillMgr.Remove("old1"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeOffline.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "local-v6-reorg"), Group: "base", Version: "v6"}); err != nil {
		t.Fatal(err)
	}

	// 验证 senv sync diff --reveal --export-remote
	exportDir := filepath.Join(t.TempDir(), "remote-export")
	syncDiffReveal = true
	syncDiffJSON = false
	syncDiffExportRemote = exportDir
	var diffOut bytes.Buffer
	diffCmd := &cobra.Command{Use: "diff"}
	diffCmd.SetOut(&diffOut)
	if err := runSyncDiff(diffCmd, nil); err != nil {
		t.Fatalf("runSyncDiff: %v", err)
	}
	syncDiffReveal = false
	syncDiffExportRemote = ""

	diffText := diffOut.String()
	if !strings.Contains(diffText, "内容完全一致") {
		t.Fatalf("sync diff should detect equivalent equiv archive:\n%s", diffText)
	}
	if !strings.Contains(diffText, "远端相对本地删除前基线内容未变") {
		t.Fatalf("sync diff should detect delete vs format-only on old1:\n%s", diffText)
	}
	if !strings.Contains(diffText, "version collision") || !strings.Contains(diffText, "senv skill rebase butler") {
		t.Fatalf("sync diff should report butler@v6 version collision:\n%s", diffText)
	}
	exportedButler, err := os.ReadFile(filepath.Join(exportDir, "skill", "base", "butler", "SKILL.md"))
	if err != nil || !strings.Contains(string(exportedButler), "remote-v6") {
		t.Fatalf("exported remote butler SKILL.md mismatch: %q, err=%v", exportedButler, err)
	}

	// 校验 --export-remote 拒绝导出 ssh_keypair
	if err := exportRemoteConflictEntries(t.TempDir(), []provider.ConflictDetail{{
		Kind: provider.KindSSHKeypair, Grp: "default", Key: "id_ed25519",
	}}, buildTestConflictAuth(nodeOffline)); err == nil {
		t.Fatal("exportRemoteConflictEntries must refuse ssh_keypair")
	}

	// 验证 senv sync 自动收养 equiv（剩余真实冲突不崩溃），并用 --resolve 解决
	syncResolve = []string{
		"skill:on-demand:old1*=local",
		"skill:base:butler*=local",
		"skill_index=local",
	}
	syncResolveDefault = ""
	syncNoHealEquivalent = false
	var syncOut bytes.Buffer
	sCmd := &cobra.Command{Use: "sync"}
	sCmd.SetOut(&syncOut)
	sCmd.SetErr(&syncOut)
	if err := runServerSync(sCmd, nodeOffline.sp); err != nil {
		t.Fatalf("runServerSync with --resolve: %v\nout=%s", err, syncOut.String())
	}
	syncResolve = nil
	if !strings.Contains(syncOut.String(), "已自动收养 1 条内容等价冲突") {
		t.Fatalf("expected auto-heal message for equiv, got:\n%s", syncOut.String())
	}
	if !strings.Contains(syncOut.String(), "已按规则解决") {
		t.Fatalf("expected resolve summary, got:\n%s", syncOut.String())
	}
}

func resetPullThrottle(t *testing.T, node *testClientNode) {
	t.Helper()
	_ = os.Remove(filepath.Join(node.dataPath, ".senv-last-pull"))
	statePath := filepath.Join(node.dataPath, ".senv-sync-state.json")
	if rawState, err := os.ReadFile(statePath); err == nil {
		var stMap map[string]any
		if json.Unmarshal(rawState, &stMap) == nil {
			stMap["last_pull_at"] = 0
			if updated, merr := json.Marshal(stMap); merr == nil {
				_ = os.WriteFile(statePath, updated, 0o600)
			}
		}
	}
}

func buildTestConflictAuth(node *testClientNode) renderconflict.Auth {
	md, _ := node.store.LoadMetadata()
	ep := 0
	if md != nil {
		ep = md.KeyEpoch
	}
	return renderconflict.Auth{
		Key:                 node.key,
		KeyEpoch:            ep,
		RemoteKeyEpoch:      ep,
		RemoteKeyCompatible: true,
	}
}

func TestE2E_Phase3_AutoMergeAndRebase(t *testing.T) {
	t.Setenv("SENV_ALLOW_INSECURE_HTTP", "1")
	password := "e2e-phase3-pass"
	srv := newFakeRichSyncServer()
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	nodeA := setupSyncNodeFromExisting(t, httpSrv.URL, password, true)
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "v1-base"), Group: "base", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("seed"); err != nil {
		t.Fatal(err)
	}
	nodeB := setupSyncNodeFromExisting(t, httpSrv.URL, password, false)

	// 1. A 新增 hela，B 新增 publish → skill_index 冲突但互不干扰，senv sync 自动三方合并
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "hela", SourceDir: makeSkillDir(t, "hela", "hela-v1"), Group: "on-demand", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-add-hela"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeB.skillMgr.Add(skill.AddOptions{ID: "publish", SourceDir: makeSkillDir(t, "publish", "pub-v1"), Group: "on-demand", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	useNodeAsActiveCLI(t, nodeB)
	var mergeOut bytes.Buffer
	mCmd := &cobra.Command{Use: "sync"}
	mCmd.SetOut(&mergeOut)
	mCmd.SetErr(&mergeOut)
	if err := runServerSync(mCmd, nodeB.sp); err != nil {
		t.Fatalf("runServerSync auto 3-way merge: %v\nout=%s", err, mergeOut.String())
	}
	if !strings.Contains(mergeOut.String(), "已自动三方合并 skill_index") {
		t.Fatalf("expected 3-way merge message, got:\n%s", mergeOut.String())
	}
	// 同步拉回后，服务端和 B 都应完整存在 publish 密文以及合并后的 hela/publish 索引
	srv.mu.Lock()
	_, hasPublishCipher := srv.entries[provider.KindSkill+"\x00on-demand\x00publish"]
	srv.mu.Unlock()
	if !hasPublishCipher {
		t.Fatal("expected publish ciphertext pushed after 3-way skill_index merge")
	}
	if _, err := nodeB.skillMgr.Show("hela"); err != nil {
		t.Fatalf("expected hela in merged index: %v", err)
	}
	if _, err := nodeB.skillMgr.Show("publish"); err != nil {
		t.Fatalf("expected publish in merged index: %v", err)
	}

	// 1.5 验证缺失精确 base 时 fail-closed 不误合
	if err := nodeA.sp.Sync("A-pull-merged"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "extra-a", SourceDir: makeSkillDir(t, "extra-a", "a"), Group: "on-demand", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-add-extra"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeB.skillMgr.Add(skill.AddOptions{ID: "extra-b", SourceDir: makeSkillDir(t, "extra-b", "b"), Group: "on-demand", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	// 清空服务端 skill_index 历史以模拟无精确 base
	srv.mu.Lock()
	delete(srv.history, provider.KindSkillIndex+"\x00\x00")
	srv.mu.Unlock()
	var failClosedOut bytes.Buffer
	fcCmd := &cobra.Command{Use: "sync"}
	fcCmd.SetOut(&failClosedOut)
	fcCmd.SetErr(&failClosedOut)
	syncNoInteractive = true
	err := runServerSync(fcCmd, nodeB.sp)
	syncNoInteractive = false
	var fcConflict *provider.SyncConflictError
	if !errors.As(err, &fcConflict) {
		t.Fatalf("expected SyncConflictError when base history missing, got: %v", err)
	}
	// 恢复合并：用 --accept-remote 清掉 extra-b 以便继续测 rebase
	if err := nodeB.sp.AcceptRemote(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 2. 双端同时写 butler@v2（内容不同）→ senv skill rebase butler 顺延为 v3
	if err := nodeA.sp.Sync("A-pull"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "A-v2"), Group: "base", Version: "v2"}); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.sp.Sync("A-push-v2"); err != nil {
		t.Fatal(err)
	}

	if _, err := nodeB.skillMgr.Add(skill.AddOptions{ID: "butler", SourceDir: makeSkillDir(t, "butler", "B-v2"), Group: "base", Version: "v2"}); err != nil {
		t.Fatal(err)
	}
	var rebaseOut bytes.Buffer
	rbCmd := &cobra.Command{Use: "rebase"}
	rbCmd.SetOut(&rebaseOut)
	if err := runSkillRebase(rbCmd, []string{"butler"}); err != nil {
		t.Fatalf("runSkillRebase: %v", err)
	}
	if !strings.Contains(rebaseOut.String(), "保留远端 v2，本地版本顺延为 v3") {
		t.Fatalf("unexpected rebase output: %s", rebaseOut.String())
	}
	detail, err := nodeB.skillMgr.Show("butler")
	if err != nil || detail.Entry.CurrentVersion != "v3" {
		t.Fatalf("expected butler rebased to v3, got %+v, err=%v", detail, err)
	}
	if _, hasV2 := detail.Entry.FindVersion("v2"); !hasV2 {
		t.Fatal("expected remote v2 snapshot preserved in rebased index")
	}

	// 清理验证：即使开启 --no-heal-equivalent，butler@v2 也不会报冲突；仅解决 skill_index 与活跃 butler 即可收敛
	syncResolve = []string{"skill_index=local", "skill:base:butler=local"}
	syncNoHealEquivalent = true
	var finalOut bytes.Buffer
	fCmd := &cobra.Command{Use: "sync"}
	fCmd.SetOut(&finalOut)
	fCmd.SetErr(&finalOut)
	if err := runServerSync(fCmd, nodeB.sp); err != nil {
		t.Fatalf("final sync after rebase: %v\nout=%s", err, finalOut.String())
	}
	syncResolve = nil
	syncNoHealEquivalent = false
}

// 确保 ValidateSkillIndexConsistency 拦截断链组合
func TestBuildResolvePlanRejectsDanglingSkillIndex(t *testing.T) {
	key := bytes.Repeat([]byte{5}, 32)
	idx := storage.NewSkillIndex()
	idx.Skills["old1"] = storage.SkillEntry{ID: "old1", Group: "on-demand", CurrentVersion: "v1"}
	idxBytes, _ := storage.ToJSON(idx)
	encIdx, _ := crypto.EncryptWithAAD(key, idxBytes, crypto.BuildEntryAAD(provider.KindSkillIndex, "", "", 1))

	conflictErr := &provider.SyncConflictError{
		Details: []provider.ConflictDetail{
			{
				Kind:   provider.KindSkillIndex,
				Local:  provider.ConflictSide{Revision: 1, Ciphertext: []byte(encIdx)},
				Remote: provider.ConflictSide{Revision: 2, Ciphertext: []byte(encIdx)},
			},
			{
				Kind:   provider.KindSkill,
				Grp:    "on-demand",
				Key:    "old1",
				Local:  provider.ConflictSide{Revision: 1, Deleted: true},
				Remote: provider.ConflictSide{Revision: 2, Deleted: false},
			},
		},
	}
	rules, def, err := parseResolveFlags([]string{"skill_index=local", "skill:on-demand:old1=local"}, "")
	if err != nil {
		t.Fatal(err)
	}
	auth := buildTestConflictAuthFromKey(key, 1)
	if _, err := buildResolvePlan(conflictErr, rules, def, auth); err == nil || !strings.Contains(err.Error(), "断链") {
		t.Fatalf("expected dangling reference error, got: %v", err)
	}
}

func buildTestConflictAuthFromKey(key []byte, epoch int) renderconflict.Auth {
	return renderconflict.Auth{
		Key:                 key,
		KeyEpoch:            epoch,
		RemoteKeyEpoch:      epoch,
		RemoteKeyCompatible: true,
	}
}
