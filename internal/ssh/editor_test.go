package ssh

import (
	"os"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestHostEditorPrepareFinish(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	if err := mgr.AddHost(&storage.HostEntry{
		Alias:    "web",
		Hostname: "old",
		User:     "old",
		Port:     22,
		Extra:    map[string]string{"ForwardAgent": "yes"},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := mgr.PrepareHostEditor("web")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(session.TmpPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("editor temp stat = %v, %v", info, err)
	}
	edited := `hostname new
user deploy
port 2222
proxy-jump 
identity-key 
tags prod, edge
attr ForwardAgent=no
ServerAliveInterval 30
`
	if err := os.WriteFile(session.TmpPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.FinishHostEditor(session)
	if err != nil || !changed {
		t.Fatalf("finish = %v, %v", changed, err)
	}
	if _, err := os.Stat(session.TmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file not removed: %v", err)
	}
	host, err := mgr.GetHost("web")
	if err != nil {
		t.Fatal(err)
	}
	if host.Hostname != "new" || host.User != "deploy" || host.Port != 2222 || host.ProxyJump != "" {
		t.Fatalf("host = %+v", host)
	}
	if strings.Join(host.Tags, ",") != "prod,edge" || host.Extra["ForwardAgent"] != "no" || host.Extra["ServerAliveInterval"] != "30" {
		t.Fatalf("host extras = %+v, tags = %v", host.Extra, host.Tags)
	}
}

func TestHostEditorGroupRoundTrip(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	if err := mgr.AddHost(&storage.HostEntry{
		Alias:    "web",
		Hostname: "10.0.0.1",
		Group:    "prod",
		Tags:     []string{"edge"},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := mgr.PrepareHostEditor("web")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(session.TmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "group prod\n") {
		t.Fatalf("editor content missing group: %s", content)
	}
	// group 改值。
	edited := strings.Replace(string(content), "group prod\n", "group staging\n", 1)
	if err := os.WriteFile(session.TmpPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := mgr.FinishHostEditor(session); err != nil || !changed {
		t.Fatalf("finish = %v, %v", changed, err)
	}
	host, err := mgr.GetHost("web")
	if err != nil {
		t.Fatal(err)
	}
	if host.Group != "staging" {
		t.Fatalf("group = %q, want staging", host.Group)
	}

	// 裸 "group" 指令清空回退未分组，其余字段不受影响。
	session, err = mgr.PrepareHostEditor("web")
	if err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(session.TmpPath)
	if err != nil {
		t.Fatal(err)
	}
	edited = strings.Replace(string(content), "group staging\n", "group \n", 1)
	if err := os.WriteFile(session.TmpPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := mgr.FinishHostEditor(session); err != nil || !changed {
		t.Fatalf("clear finish = %v, %v", changed, err)
	}
	host, err = mgr.GetHost("web")
	if err != nil {
		t.Fatal(err)
	}
	if host.Group != "" {
		t.Fatalf("group = %q, want empty", host.Group)
	}
	if host.Hostname != "10.0.0.1" || strings.Join(host.Tags, ",") != "edge" {
		t.Fatalf("sibling fields clobbered: %+v", host)
	}
}

func TestHostEditorCreateAndUnchanged(t *testing.T) {
	mgr, _ := newTestSSHManager(t)
	session, err := mgr.PrepareHostEditor("new")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.FinishHostEditor(session)
	if err != nil || changed {
		t.Fatalf("unchanged create = %v, %v", changed, err)
	}
	if _, err := mgr.GetHost("new"); err == nil {
		t.Fatal("unchanged editor created host")
	}
	session, err = mgr.PrepareHostEditor("new")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.TmpPath, []byte("hostname example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := mgr.FinishHostEditor(session); err != nil || !changed {
		t.Fatalf("create finish = %v, %v", changed, err)
	}
	if _, err := mgr.GetHost("new"); err != nil {
		t.Fatal(err)
	}
}
