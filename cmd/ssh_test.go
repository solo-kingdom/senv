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
	target := filepath.Join(dir, ".ssh", "senv", "material-key")
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
