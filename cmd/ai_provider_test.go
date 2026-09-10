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

const aiProviderTestCatalog = `{"p1":{"id":"p1","name":"P1","models":{"m1":{"id":"m1"},"m2":{"id":"m2"}}}}`

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
		defaultModel: providerAddDefault, force: providerAddForce,
		stdin: providerAddAPIKeyStdin, allowHTTP: providerAddAllowHTTP,
	}
	set()
	t.Cleanup(func() {
		providerAddBaseURL = old.baseURL
		providerAddKeyRef = old.keyRef
		providerAddCatalog = old.catalog
		providerAddModels = old.models
		providerAddDefault = old.defaultModel
		providerAddForce = old.force
		providerAddAPIKeyStdin = old.stdin
		providerAddAllowHTTP = old.allowHTTP
	})
}

type providerAddFlags struct {
	baseURL, keyRef, catalog, defaultModel string
	models                                 []string
	force                                  bool
	stdin, allowHTTP                       bool
}

func setProviderCredentialReader(t *testing.T, value string) {
	t.Helper()
	old := providerCredentialReader
	providerCredentialReader = func(io.Reader, io.Writer) ([]byte, error) {
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
		providerAddDefault = "custom-1"
	})
	out, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"main"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "已保存 LLM Provider main") || !strings.Contains(out, "3 个模型") {
		t.Fatalf("add output = %q", out)
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
		providerAddAPIKeyStdin = true
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"stdin"}); err != nil {
		t.Fatalf("stdin reader path failed: %v", err)
	}

	setProviderAddFlags(t, func() {
		providerAddBaseURL = "https://api.example.com"
		providerAddModels = []string{"m1"}
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
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"local"}); err == nil ||
		!strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP error = %v, want HTTPS policy", err)
	}
	setProviderAddFlags(t, func() {
		providerAddBaseURL = "http://127.0.0.1:11434/v1"
		providerAddModels = []string{"m1"}
		providerAddAllowHTTP = true
	})
	if _, err := runAIProviderCmd(t, aiProviderAddCmd, []string{"local"}); err != nil {
		t.Fatalf("allowed HTTP add: %v", err)
	}
}
