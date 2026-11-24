package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRepoAndVersionRejectTraversal(t *testing.T) {
	for _, badRepo := range []string{"", "owner", "a/b/c", "../evil", "owner/repo..x"} {
		if err := validateRepo(badRepo); err == nil {
			t.Fatalf("validateRepo(%q) unexpectedly succeeded", badRepo)
		}
	}
	for _, badVer := range []string{"", "../v1.0.0", "v1.0.0/../../x", "v1.0.0?foo=bar", "latest"} {
		if err := validateVersion(badVer); err == nil {
			t.Fatalf("validateVersion(%q) unexpectedly succeeded", badVer)
		}
	}
	for _, okVer := range []string{"v1.2.3", "t0.1.0", "1.2.3-rc.1"} {
		if err := validateVersion(okVer); err != nil {
			t.Fatalf("validateVersion(%q) failed: %v", okVer, err)
		}
	}
}

func TestUpdateRejectsPathTraversalVersion(t *testing.T) {
	_, err := Update(Options{
		Repo:       DefaultRepo,
		Version:    "../../evil",
		InstallDir: t.TempDir(),
		Force:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("expected invalid version error, got: %v", err)
	}
}

func TestDownloadFileEnforcesMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("A", 128)))
	}))
	defer srv.Close()

	err := downloadFileWithLimit(srv.Client(), srv.URL, t.TempDir()+"/out", 64)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}
