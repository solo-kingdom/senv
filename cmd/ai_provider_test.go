package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
)

const aiProviderTestCatalog = `{"p1":{"id":"p1","name":"P1","models":{"m1":{"id":"m1","limit":{"context":128000}},"m2":{"id":"m2","limit":{"context":200000}}}}}`

func writeAIProviderTestCatalog(t *testing.T) {
	t.Helper()
	cat, err := llm.Parse([]byte(aiProviderTestCatalog), "https://x.test", time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := llm.Save(catalogCachePath(), cat); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
}

// setProviderAddFlags 设置 add 命令 flags 并在测试结束后还原。
func setProviderAddFlags(t *testing.T, set func()) {
	t.Helper()
	old := providerAddFlags{
		baseURL: providerAddBaseURL, keyRef: providerAddKeyRef,
		catalog: providerAddCatalog, models: providerAddModels,
		modelCtx:     providerAddModelCtx,
		defaultModel: providerAddDefault, force: providerAddForce,
		stdin: providerAddAPIKeyStdin, allowHTTP: providerAddAllowHTTP,
	}
	set()
	t.Cleanup(func() {
		providerAddBaseURL = old.baseURL
		providerAddKeyRef = old.keyRef
		providerAddCatalog = old.catalog
		providerAddModels = old.models
		providerAddModelCtx = old.modelCtx
		providerAddDefault = old.defaultModel
		providerAddForce = old.force
		providerAddAPIKeyStdin = old.stdin
		providerAddAllowHTTP = old.allowHTTP
	})
}

type providerAddFlags struct {
	baseURL, keyRef, catalog, defaultModel string
	models, modelCtx                       []string
	force                                  bool
	stdin, allowHTTP                       bool
}

func setProviderCredentialReader(t *testing.T, value string) {
	t.Helper()
	old := providerCredentialReader
	providerCredentialReader = func(_ io.Reader, _ io.Writer, _ bool) ([]byte, error) {
		return []byte(value), nil
	}
	t.Cleanup(func() { providerCredentialReader = old })
}

func runAIProviderCmd(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

func TestAIProviderFullLifecycle(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-secret-value")

	// add：目录模型 + 自定义模型 + 自有凭据。
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddCatalog = "p1"
		providerAddModels = []string{"custom-1"}
		providerAddModelCtx = []string{"custom-1=1000000"}
		providerAddDefault = "custom-1"
	})
	out, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "已保存 LLM Provider main") || !strings.Contains(out, "3 个模型") {
		t.Fatalf("add output = %q", out)
	}
	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("getAIProviderManager: %v", err)
	}
	entry, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got := entry.ModelInfo["custom-1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("custom-1 context window = %d, want 1000000", got)
	}
	if got := entry.ModelInfo["m1"].ContextWindow; got != 128000 {
		t.Fatalf("m1 context window = %d, want 128000", got)
	}

	// 重复别名：未加 --force 报错。
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate error = %v", err)
	}

	// show：展示凭据引用，绝不出现凭据明文。
	out, err = runAIProviderCmd(t, aiProviderShowCmd, []string{"main"})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "text:llm-keys/main") {
		t.Fatalf("show output = %q, want credential ref", out)
	}
	if strings.Contains(out, "sk-secret-value") {
		t.Fatalf("show output leaks credential: %q", out)
	}

	// list：包含条目且无凭据明文。
	out, err = runAIProviderCmd(t, aiProviderListCmd, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "main") || strings.Contains(out, "sk-secret-value") {
		t.Fatalf("list output = %q", out)
	}

	// remove：自有凭据连带删除。
	out, err = runAIProviderCmd(t, aiProviderRemoveCmd, []string{"main"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, "已删除") || !strings.Contains(out, "已同时删除其自有凭据条目") {
		t.Fatalf("remove output = %q", out)
	}

	// 删除后再 show：报 not found。
	if _, err := runAIProviderCmd(t, aiProviderShowCmd, []string{"main"}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("show after remove error = %v", err)
	}
}

func TestAIProviderAddExternalKeyRef(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddKeyRef = "env:openai/KEY"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
	})
	out, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"ext"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	_ = out

	out, err = runAIProviderCmd(t, aiProviderRemoveCmd, []string{"ext"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, "凭据为外部引用，已保留") {
		t.Fatalf("remove output = %q", out)
	}
}

func TestAIProviderAddRequiresModelContext(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "sk-secret-value")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err == nil ||
		!strings.Contains(err.Error(), "--model-context") {
		t.Fatalf("add error = %v, want model context guidance", err)
	}
	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("getAIProviderManager: %v", err)
	}
	entries, err := mgr.ListProviders()
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("provider written despite missing model context: %+v", entries)
	}
}

func TestAIProviderAddCatalogMissing(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "k")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddCatalog = "p1"
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err == nil ||
		!strings.Contains(err.Error(), "senv ai refresh") {
		t.Fatalf("error = %v, want refresh hint", err)
	}
}

func TestAIProviderAddUsesCredentialReader(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-from-prompt")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddCatalog = "p1"
		providerAddModels = []string{"m1"}
	})
	out, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if strings.Contains(out, "sk-from-prompt") {
		t.Fatalf("credential leaked into output: %q", out)
	}
	if aiProviderAddCmd.Flags().Lookup("api-key") != nil {
		t.Fatal("--api-key flag still exists")
	}
}

func TestAIProviderAddStdinFlagAndNonTTYGuard(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "sk-from-stdin")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddAPIKeyStdin = true
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"stdin"}); err != nil {
		t.Fatalf("stdin reader path failed: %v", err)
	}

	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddAPIKeyStdin = false
	})
	providerCredentialReader = readProviderCredential
	_, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"nontty"})
	if err == nil || !strings.Contains(err.Error(), "--api-key-stdin") {
		t.Fatalf("non-TTY error = %v, want stdin guidance", err)
	}
}

func TestAIProviderAddHTTPRequiresExplicitAllow(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "k")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "http://127.0.0.1:11434/v1"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"local"}); err == nil ||
		!strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP error = %v, want HTTPS policy", err)
	}
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "http://127.0.0.1:11434/v1"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
		providerAddAllowHTTP = true
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"local"}); err != nil {
		t.Fatalf("allowed HTTP add: %v", err)
	}
}

// setProviderEditFlag 通过 pflag 设置 edit 子命令的 flag，使 Flags().Changed
// 为 true（直接赋包变量无法触发「显式提供」语义）。
func setProviderEditFlag(t *testing.T, name, value string) {
	t.Helper()
	flag := aiProviderEditCmd.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("edit flag %q not registered", name)
	}
	if err := aiProviderEditCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
	t.Cleanup(func() { flag.Changed = false })
}

func TestAIProviderEditCLI(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-secret-value")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1", "m2"}
		providerAddModelCtx = []string{"m1=128000", "m2=200000"}
		providerAddDefault = "m1"
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	setProviderEditFlag(t, "api-shape", "anthropic")
	setProviderEditFlag(t, "base-url", "https://new.example.com")
	setProviderEditFlag(t, "default-model", "m2")
	out, err := runAIProviderCmd(t, aiProviderEditCmd, []string{"main"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !strings.Contains(out, "接入形态：anthropic") {
		t.Fatalf("output missing effective shape:\n%s", out)
	}
	if !strings.Contains(out, "接入地址：https://new.example.com/v1") {
		t.Fatalf("output missing normalized base URL:\n%s", out)
	}

	// 别名不可改：edit 不接受第二个位置参数。
	if err := aiProviderEditCmd.Args(aiProviderEditCmd, []string{"main", "other"}); err == nil {
		t.Error("edit must reject an alias-change positional arg")
	}

	// 非法形态被拒且不写入。
	setProviderEditFlag(t, "api-shape", "openai")
	if _, err := runAIProviderCmd(t, aiProviderEditCmd, []string{"main"}); err == nil ||
		!strings.Contains(err.Error(), "api_shape") {
		t.Fatalf("invalid api_shape error = %v", err)
	}

	// 显式传空字符串清除字段。
	setProviderEditFlag(t, "api-shape", "")
	out, err = runAIProviderCmd(t, aiProviderEditCmd, []string{"main"})
	if err != nil {
		t.Fatalf("edit clear shape: %v", err)
	}
	if !strings.Contains(out, "接入形态：（未声明") {
		t.Fatalf("clear output = %q", out)
	}

	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("getAIProviderManager: %v", err)
	}
	entry, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if entry.APIShape != "" {
		t.Fatalf("APIShape = %q, want cleared", entry.APIShape)
	}
	if entry.DefaultModel != "m2" {
		t.Fatalf("DefaultModel = %q, want m2", entry.DefaultModel)
	}
	// show 输出包含接入形态。
	showOut, err := runAIProviderCmd(t, aiProviderShowCmd, []string{"main"})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(showOut, "接入形态：-") {
		t.Fatalf("show output = %q", showOut)
	}
}

func TestAIProviderEditBackfillsModelContext(t *testing.T) {
	newAuditTestProject(t)
	setProviderCredentialReader(t, "sk-secret-value")
	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("getAIProviderManager: %v", err)
	}
	if _, err := mgr.AddProvider(llm.AddProviderOptions{
		Alias: "legacy", BaseURL: "https://api.example.com",
		APIKey: "sk-secret-value", Models: []string{"m1"},
	}); err != nil {
		t.Fatalf("seed legacy provider: %v", err)
	}

	setProviderEditFlag(t, "model-context", "m1=1000000")
	if _, err := runAIProviderCmd(t, aiProviderEditCmd, []string{"legacy"}); err != nil {
		t.Fatalf("edit backfill: %v", err)
	}
	entry, err := mgr.GetProvider("legacy")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got := entry.ModelInfo["m1"].ContextWindow; got != 1_000_000 {
		t.Fatalf("context window = %d, want 1000000", got)
	}
}

// TestAIProviderEditStdinFlagRoutesToStdin 覆盖 add/edit 各自 --api-key-stdin
// 的取凭据路径互不串线（edit 曾误用 add 的 flag）。
func TestAIProviderEditStdinFlagRoutesToStdin(t *testing.T) {
	newAuditTestProject(t)
	writeAIProviderTestCatalog(t)
	setProviderCredentialReader(t, "sk-initial")
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
		providerAddModelCtx = []string{"m1=128000"}
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	old := providerCredentialReader
	providerCredentialReader = func(_ io.Reader, _ io.Writer, fromStdin bool) ([]byte, error) {
		if !fromStdin {
			t.Error("edit --api-key-stdin must take the stdin path, not the TTY prompt")
		}
		return []byte("sk-rotated"), nil
	}
	t.Cleanup(func() { providerCredentialReader = old })

	setProviderEditFlag(t, "api-key-stdin", "true")
	if _, err := runAIProviderCmd(t, aiProviderEditCmd, []string{"main"}); err != nil {
		t.Fatalf("edit rotate: %v", err)
	}
	// 轮换后档案仍引用同一自有凭据条目。
	mgr, err := getAIProviderManager()
	if err != nil {
		t.Fatalf("getAIProviderManager: %v", err)
	}
	entry, err := mgr.GetProvider("main")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if entry.CredentialRef != llm.OwnedCredentialRef("main") {
		t.Fatalf("CredentialRef = %q", entry.CredentialRef)
	}
}
