package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectPlatform(t *testing.T) {
	osName, arch, err := DetectPlatform()
	if err != nil {
		t.Fatal(err)
	}
	if osName != "linux" && osName != "darwin" {
		t.Fatalf("unexpected os %q", osName)
	}
	if arch != "amd64" && arch != "arm64" {
		t.Fatalf("unexpected arch %q", arch)
	}
}

func TestIsUpToDate(t *testing.T) {
	tests := []struct {
		current string
		target  string
		want    bool
	}{
		{"v0.1.3", "v0.1.3", true},
		{"0.1.3", "v0.1.3", true},
		{"v0.1.2", "v0.1.3", false},
		{"dev", "v0.1.3", false},
		{"", "v0.1.3", false},
	}
	for _, tt := range tests {
		if got := isUpToDate(tt.current, tt.target); got != tt.want {
			t.Fatalf("isUpToDate(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
		}
	}
}

func TestFetchLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/solo-kingdom/senv/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(releaseInfo{TagName: "v0.1.3"})
	}))
	t.Cleanup(srv.Close)

	client := srv.Client()
	transport := client.Transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return transport.RoundTrip(req)
	})

	tag, err := FetchLatestRelease("solo-kingdom/senv", client)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.1.3" {
		t.Fatalf("got %q, want v0.1.3", tag)
	}
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	var buf bytes.Buffer
	result, err := Update(Options{
		Repo:    "solo-kingdom/senv",
		Version: "v0.1.3",
		Current: "v0.1.3",
		Out:     &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.AlreadyUpToDate {
		t.Fatal("expected already up to date")
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestUpdateInstallsRelease(t *testing.T) {
	osName, arch, err := DetectPlatform()
	if err != nil {
		t.Fatal(err)
	}

	binaryContent := []byte("#!/bin/sh\necho updated\n")
	asset := "senv_v0.1.4_" + osName + "_" + arch + ".tar.gz"
	archiveBytes, checksum := buildTestArchive(t, binaryContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/solo-kingdom/senv/releases/latest":
			_ = json.NewEncoder(w).Encode(releaseInfo{TagName: "v0.1.4"})
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			w.Write(archiveBytes)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(checksum + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := srv.Client()
	transport := client.Transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return transport.RoundTrip(req)
	})

	installDir := t.TempDir()
	var buf bytes.Buffer
	result, err := Update(Options{
		Repo:       "solo-kingdom/senv",
		InstallDir: installDir,
		Current:    "v0.1.3",
		HTTPClient: client,
		Out:        &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AlreadyUpToDate {
		t.Fatal("expected update to run")
	}
	if result.TargetVersion != "v0.1.4" {
		t.Fatalf("target version = %q", result.TargetVersion)
	}

	installed := filepath.Join(installDir, "senv")
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binaryContent) {
		t.Fatalf("installed binary mismatch")
	}
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %o", info.Mode().Perm())
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "senv_v0.1.4_linux_amd64.tar.gz")
	if err := os.WriteFile(archive, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	checksums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksums, []byte("deadbeef  senv_v0.1.4_linux_amd64.tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(archive, checksums); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func buildTestArchive(t *testing.T, binary []byte) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "senv",
		Mode: 0o755,
		Size: int64(len(binary)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExtractBinaryMissing(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: "other",
		Mode: 0o644,
		Size: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	dest := filepath.Join(dir, "senv")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, dest); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestResolveInstallDirDefaultLocalBin(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "bin")
	got, err := resolveInstallDir("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveInstallDirOverride(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveInstallDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "payload")
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "download.txt")
	if err := downloadFile(srv.Client(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}
}
