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
