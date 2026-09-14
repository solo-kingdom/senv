package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

func newSSHTestProject(t *testing.T) string {
	t.Helper()
	newAuditTestProject(t)
	// CLI integration tests invoke RunE directly; SSH key selection must not
	// consume the test process's real stdin.
	sshInteractiveSelection = false
	return t.TempDir()
}

func TestHostExportParsesWithSSH_G(t *testing.T) {
	dir := newSSHTestProject(t)
	t.Setenv("HOME", dir)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "ssh-g@test")
	keypairImportFile = keyPath
	hostAddHostname = "10.1.2.3"
	hostAddUser = "deploy"
	hostAddPort = 2222
	hostAddKeypair = "ssh-g-key"
	t.Cleanup(func() {
		keypairImportFile, hostAddHostname, hostAddUser = "", "", ""
		hostAddPort, hostAddKeypair = 0, ""
	})
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"ssh-g-key", "--file", keyPath}))
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"gweb"}))

	configPath := filepath.Join(dir, "ssh-config")
	hostExportAlias = "gweb"
	hostExportOut = configPath
	t.Cleanup(func() { hostExportAlias, hostExportOut = "", "" })
	runSSHCommand(t, hostExportCmd.RunE(&cobra.Command{}, nil))

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh not installed")
	}
	output, err := exec.Command(sshPath, "-G", "-F", configPath, "gweb").Output()
	if err != nil {
		t.Fatalf("ssh -G: %v\n%s", err, output)
	}
	parsed := string(output)
	for _, want := range []string{"hostname 10.1.2.3\n", "user deploy\n", "port 2222\n", "identityfile "} {
		if !strings.Contains(parsed, want) {
			t.Fatalf("ssh -G output missing %q:\n%s", want, parsed)
		}
	}
}

func ed25519TestKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return private
}

func writeTestEd25519Key(t *testing.T, dir, name, comment string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	private := ed25519TestKey(t)
	block, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSSHCommand(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("command: %v", err)
	}
}

func TestKeypairAndHostCLIFlow(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "web@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile, keypairImportForce = "", false })

	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"web-key", "--file", keyPath}))
	if err := keypairImportCmd.RunE(&cobra.Command{}, []string{"web-key", "--file", keyPath}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate import error = %v", err)
	}
	keypairImportForce = true
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"web-key", "--file", keyPath, "--force"}))
	runSSHCommand(t, keypairListCmd.RunE(&cobra.Command{}, nil))

	hostAddHostname = "10.0.0.1"
	hostAddUser = "deploy"
	hostAddPort = 2222
	hostAddKeypair = "web-key"
	hostAddTags = []string{"prod"}
	hostAddAttrs = []string{"ForwardAgent=yes", "ServerAliveInterval=30"}
	t.Cleanup(func() {
		hostAddHostname, hostAddUser, hostAddKeypair = "", "", ""
		hostAddPort, hostAddTags, hostAddAttrs = 0, nil, nil
	})
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"web"}))

	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	hostExportOut = "-"
	t.Cleanup(func() { hostExportOut = "" })
	getErr := hostExportCmd.RunE(&cobra.Command{}, nil)
	writer.Close()
	os.Stdout = stdout
	if getErr != nil {
		t.Fatalf("host export: %v", getErr)
	}
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	exported := string(captured)
	for _, want := range []string{
		"Host web\n",
		"HostName 10.0.0.1\n",
		"User deploy\n",
		"Port 2222\n",
		"IdentityFile ",
		"ForwardAgent yes\n",
		"ServerAliveInterval 30\n",
	} {
		if !strings.Contains(exported, want) {
			t.Fatalf("export missing %q:\n%s", want, exported)
		}
	}

	if err := keypairDeleteCmd.RunE(&cobra.Command{}, []string{"web-key"}); err == nil || !strings.Contains(err.Error(), "referenced by host(s): web") {
		t.Fatalf("referenced delete error = %v", err)
	}
	keypairDeleteForce = true
	t.Cleanup(func() { keypairDeleteForce = false })
	runSSHCommand(t, keypairDeleteCmd.RunE(&cobra.Command{}, []string{"web-key", "--force"}))
	if log := readAuditLogForTest(t); !strings.Contains(log, `"target":"keypair:web-key"`) {
		t.Fatalf("audit missing keypair target: %s", log)
	}
}

func TestHostGroupFlagFlow(t *testing.T) {
	newSSHTestProject(t)
	hostAddHostname = "10.0.0.1"
	hostAddUser = "deploy"
	hostAddGroup = "prod"
	t.Cleanup(func() { hostAddHostname, hostAddUser, hostAddGroup = "", "", "" })
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"web"}))

	mgr, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	host, err := mgr.GetHost("web")
	if err != nil {
		t.Fatal(err)
	}
	if host.Group != "prod" {
		t.Fatalf("group = %q, want prod", host.Group)
	}

	// get 与 list 输出在有 group 值时展示。
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	runSSHCommand(t, hostGetCmd.RunE(&cobra.Command{}, []string{"web"}))
	runSSHCommand(t, hostListCmd.RunE(&cobra.Command{}, nil))
	writer.Close()
	os.Stdout = stdout
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	out := string(captured)
	if !strings.Contains(out, "Group: prod") {
		t.Fatalf("get output missing group: %s", out)
	}
	if !strings.Contains(out, "group:prod") {
		t.Fatalf("list output missing group: %s", out)
	}

	// edit --group 修改分组。
	runSSHCommand(t, hostEditCmd.RunE(newHostEditGroupCmd(t, "staging"), []string{"web"}))
	host, err = mgr.GetHost("web")
	if err != nil || host.Group != "staging" {
		t.Fatalf("edited group = %+v, %v", host, err)
	}

	// edit --group "" 回退为未分组。
	runSSHCommand(t, hostEditCmd.RunE(newHostEditGroupCmd(t, ""), []string{"web"}))
	host, err = mgr.GetHost("web")
	if err != nil || host.Group != "" {
		t.Fatalf("cleared group = %+v, %v", host, err)
	}
}

// newHostEditGroupCmd builds a throwaway command whose --group flag is bound
// to hostEditGroup and explicitly set, mirroring `senv host edit --group v`.
func newHostEditGroupCmd(t *testing.T, value string) *cobra.Command {
	t.Helper()
	t.Cleanup(func() { hostEditGroup = "" })
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&hostEditGroup, "group", "", "")
	if err := cmd.Flags().Set("group", value); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestKeypairGroupFlagFlow(t *testing.T) {
	newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, t.TempDir(), "id_group", "group@test")
	keypairImportFile = keyPath
	keypairImportGroup = "prod"
	t.Cleanup(func() { keypairImportFile, keypairImportGroup = "", "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"group-key", "--file", keyPath}))

	mgr, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := mgr.GetKeyPairSummary("group-key")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Group != "prod" {
		t.Fatalf("group = %q, want prod", summary.Group)
	}

	// list 输出在有 group 值时行尾追加。
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	runSSHCommand(t, keypairListCmd.RunE(&cobra.Command{}, nil))
	writer.Close()
	os.Stdout = stdout
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if out := string(captured); !strings.Contains(out, "group:prod") {
		t.Fatalf("list output missing group: %s", out)
	}
}

func TestKeypairEditGroupFlow(t *testing.T) {
	newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, t.TempDir(), "id_edit", "edit@test")
	keypairImportFile = keyPath
	keypairImportGroup = "prod"
	t.Cleanup(func() { keypairImportFile, keypairImportGroup = "", "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"edit-key", "--file", keyPath}))

	mgr, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	before, err := mgr.GetKeyPairSummary("edit-key")
	if err != nil {
		t.Fatal(err)
	}

	// 缺 --group：参数错误提示用法，零变更。
	if err := keypairEditCmd.RunE(&cobra.Command{}, []string{"edit-key"}); err == nil || !strings.Contains(err.Error(), "--group is required") {
		t.Fatalf("missing --group error = %v", err)
	}
	summary, err := mgr.GetKeyPairSummary("edit-key")
	if err != nil || summary.Group != "prod" {
		t.Fatalf("group after missing flag = %+v, %v", summary, err)
	}

	// edit --group 改组生效，key 材料不动。
	runSSHCommand(t, keypairEditCmd.RunE(newKeypairEditGroupCmd(t, "staging"), []string{"edit-key"}))
	summary, err = mgr.GetKeyPairSummary("edit-key")
	if err != nil || summary.Group != "staging" {
		t.Fatalf("edited group = %+v, %v", summary, err)
	}
	if summary.Fingerprint != before.Fingerprint || summary.PublicKey != before.PublicKey {
		t.Fatalf("key material changed: before=%+v after=%+v", before, summary)
	}

	// list 输出展示新组。
	out := captureStdout(t, func() {
		runSSHCommand(t, keypairListCmd.RunE(&cobra.Command{}, nil))
	})
	if !strings.Contains(out, "group:staging") {
		t.Fatalf("list output missing group: %s", out)
	}

	// 非法组名（含 /）拒绝，原值不变。
	if err := keypairEditCmd.RunE(newKeypairEditGroupCmd(t, "a/b"), []string{"edit-key"}); err == nil {
		t.Fatal("edit --group a/b should fail")
	}
	summary, err = mgr.GetKeyPairSummary("edit-key")
	if err != nil || summary.Group != "staging" {
		t.Fatalf("group after invalid edit = %+v, %v", summary, err)
	}

	// edit --group "" 清除分组。
	runSSHCommand(t, keypairEditCmd.RunE(newKeypairEditGroupCmd(t, ""), []string{"edit-key"}))
	summary, err = mgr.GetKeyPairSummary("edit-key")
	if err != nil || summary.Group != "" {
		t.Fatalf("cleared group = %+v, %v", summary, err)
	}

	// 不存在的 keypair：报错且零副作用（vault 无新条目，审计记录失败）。
	if err := keypairEditCmd.RunE(newKeypairEditGroupCmd(t, "prod"), []string{"nope"}); err == nil {
		t.Fatal("edit nonexistent keypair should fail")
	}
	if _, err := mgr.GetKeyPairSummary("nope"); err == nil {
		t.Fatal("failed edit must not create a keypair")
	}
	pairs, err := mgr.ListKeyPairs()
	if err != nil || len(pairs) != 1 {
		t.Fatalf("vault side effects: pairs=%v, %v", pairs, err)
	}
	if log := readAuditLogForTest(t); !strings.Contains(log, `"target":"keypair:nope"`) || !strings.Contains(log, `"success":false`) {
		t.Fatalf("audit missing failed edit: %s", log)
	}
}

// newKeypairEditGroupCmd builds a throwaway command whose --group flag is bound
// to keypairEditGroup and explicitly set, mirroring `senv keypair edit --group v`.
func newKeypairEditGroupCmd(t *testing.T, value string) *cobra.Command {
	t.Helper()
	t.Cleanup(func() { keypairEditGroup = "" })
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&keypairEditGroup, "group", "", "")
	if err := cmd.Flags().Set("group", value); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestHostAddInvalidReferences(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "bad@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile = "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"bad-key", "--file", keyPath}))

	hostAddHostname = "example"
	hostAddKeypair = "missing-key"
	t.Cleanup(func() { hostAddHostname, hostAddKeypair = "", "" })
	if err := hostAddCmd.RunE(&cobra.Command{}, []string{"bad"}); err == nil || !strings.Contains(err.Error(), "missing-key") {
		t.Fatalf("invalid identity error = %v", err)
	}

	hostAddKeypair = ""
	hostAddProxyJump = "missing-host"
	t.Cleanup(func() { hostAddProxyJump = "" })
	if err := hostAddCmd.RunE(&cobra.Command{}, []string{"bad"}); err == nil || !strings.Contains(err.Error(), "missing-host") {
		t.Fatalf("invalid proxy error = %v", err)
	}
}

func TestKeypairMaterializePermissionsAndOverwrite(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "material@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile = "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"material-key", "--file", keyPath}))

	t.Setenv("HOME", dir)
	runSSHCommand(t, keypairMaterializeCmd.RunE(&cobra.Command{}, []string{"material-key"}))
	target := filepath.Join(dir, ".ssh", "senv", "keys", "_ungrouped", "material-key")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("materialize dir mode = %o, want 700", dirInfo.Mode().Perm())
	}
	if err := keypairMaterializeCmd.RunE(&cobra.Command{}, []string{"material-key"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite error = %v", err)
	}
}

func TestHostOneStepImportAndLink(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "link@test")
	keypairImportFile = keyPath
	hostAddHostname = "example"
	hostAddKeyFile = keyPath
	hostAddKeypairName = "linked-key"
	t.Logf("before add: file=%q name=%q", hostAddKeyFile, hostAddKeypairName)
	t.Cleanup(func() {
		keypairImportFile, hostAddHostname, hostAddKeyFile, hostAddKeypairName = "", "", "", ""
	})
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"linked"}))

	mgr, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	host, err := mgr.GetHost("linked")
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityKey != "linked-key" {
		t.Fatalf("identity = %q, want linked-key", host.IdentityKey)
	}
	if _, err := mgr.GetKeyPairSummary("linked-key"); err != nil {
		t.Fatalf("linked key missing: %v", err)
	}
}

func TestHostAddInteractiveKeypairSelection(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "interactive@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile = "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"interactive-key", "--file", keyPath}))

	hostAddHostname = "example"
	t.Cleanup(func() { hostAddHostname = "" })
	sshInteractiveSelection = true
	t.Cleanup(func() { sshInteractiveSelection = false })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("1\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = reader
	runErr := hostAddCmd.RunE(&cobra.Command{}, []string{"selected"})
	os.Stdin = oldStdin
	runSSHCommand(t, runErr)

	mgr, err := getSSHManager()
	if err != nil {
		t.Fatal(err)
	}
	host, err := mgr.GetHost("selected")
	if err != nil {
		t.Fatal(err)
	}
	if host.IdentityKey != "interactive-key" {
		t.Fatalf("interactive selection = %q", host.IdentityKey)
	}
}

func TestKeypairRenameCLIFlow(t *testing.T) {
	dir := newSSHTestProject(t)
	keyPath := writeTestEd25519Key(t, dir, "id_test", "rename@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile, keypairImportForce = "", false })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"web-key", "--file", keyPath}))

	hostAddHostname = "10.0.0.9"
	hostAddKeypair = "web-key"
	t.Cleanup(func() { hostAddHostname, hostAddKeypair = "", "" })
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"web"}))

	// Capture stdout: the command reports the number of updated host references.
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	renameErr := keypairRenameCmd.RunE(&cobra.Command{}, []string{"web-key", "prod-key"})
	writer.Close()
	os.Stdout = stdout
	if renameErr != nil {
		t.Fatalf("keypair rename: %v", renameErr)
	}
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if out := string(captured); !strings.Contains(out, "prod-key") || !strings.Contains(out, "1 host reference") {
		t.Errorf("rename output = %q, want new name and reference count", out)
	}

	// Renaming onto an existing name must fail without touching the vault.
	keypairImportFile = keyPath
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"other-key", "--file", keyPath}))
	if err := keypairRenameCmd.RunE(&cobra.Command{}, []string{"prod-key", "other-key"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("conflicting rename error = %v", err)
	}
}

func TestHostExportApplyUnexportAndPruneFlow(t *testing.T) {
	dir := newSSHTestProject(t)
	t.Setenv("HOME", dir)
	keyPath := writeTestEd25519Key(t, dir, "id_apply", "apply@test")
	keypairImportFile = keyPath
	t.Cleanup(func() { keypairImportFile = "" })
	runSSHCommand(t, keypairImportCmd.RunE(&cobra.Command{}, []string{"apply-key", "--file", keyPath}))

	hostAddHostname = "10.0.0.1"
	hostAddKeypair = "apply-key"
	t.Cleanup(func() { hostAddHostname, hostAddKeypair = "", "" })
	runSSHCommand(t, hostAddCmd.RunE(&cobra.Command{}, []string{"apply-web"}))

	// 默认导出 = 应用模式：组片段 + 落盘 + 注册，ssh 直接可用。
	runSSHCommand(t, hostExportCmd.RunE(&cobra.Command{}, nil))
	fragment := filepath.Join(dir, ".ssh", "senv", "groups", "_ungrouped.conf")
	data, err := os.ReadFile(fragment)
	if err != nil || !strings.Contains(string(data), "Host apply-web\n") {
		t.Fatalf("group fragment:\n%s, %v", data, err)
	}
	keyFile := filepath.Join(dir, ".ssh", "senv", "keys", "_ungrouped", "apply-key")
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized key: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(dir, ".ssh", "config"))
	if err != nil || !strings.Contains(string(cfg), "Include ~/.ssh/senv/groups/*.conf") {
		t.Fatalf("ssh config:\n%s, %v", cfg, err)
	}
	if sshPath, err := exec.LookPath("ssh"); err == nil {
		output, err := exec.Command(sshPath, "-G", "-F", filepath.Join(dir, ".ssh", "config"), "apply-web").Output()
		if err != nil {
			t.Fatalf("ssh -G: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "identityfile "+keyFile) {
			t.Fatalf("ssh -G identityfile missing %q:\n%s", keyFile, output)
		}
	}
	if log := readAuditLogForTest(t); !strings.Contains(log, `"target":"host:export"`) {
		t.Fatalf("audit missing host:export: %s", log)
	}

	// 撤回：注册行与组片段消失，落盘私钥保留。
	runSSHCommand(t, hostUnexportCmd.RunE(&cobra.Command{}, nil))
	if _, err := os.Stat(fragment); !os.IsNotExist(err) {
		t.Fatalf("fragment must be removed: %v", err)
	}
	cfg, _ = os.ReadFile(filepath.Join(dir, ".ssh", "config"))
	if strings.Contains(string(cfg), "Include ~/.ssh/senv/groups/*.conf") {
		t.Fatalf("include line must be removed:\n%s", cfg)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("materialized key must survive unexport: %v", err)
	}

	// prune：未被引用的文件被清理，被引用私钥不动。
	orphan := filepath.Join(dir, ".ssh", "senv", "keys", "_ungrouped", "orphan-key")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	keypairPruneForce = true
	t.Cleanup(func() { keypairPruneForce = false })
	runSSHCommand(t, keypairPruneCmd.RunE(&cobra.Command{}, nil))
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan must be pruned: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("referenced key must survive prune: %v", err)
	}
}

func TestHostExportRejectsHostAndGroupTogether(t *testing.T) {
	newSSHTestProject(t)
	hostExportAlias = "web"
	hostExportGroup = "prod"
	t.Cleanup(func() { hostExportAlias, hostExportGroup = "", "" })
	if err := hostExportCmd.RunE(&cobra.Command{}, nil); err == nil ||
		!strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("mutual exclusion error = %v", err)
	}
}
