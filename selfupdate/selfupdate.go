package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultRepo   = "solo-kingdom/senv"
	binaryName    = "senv"
	writeTestFile = ".senv-write-test"
)

type Options struct {
	Repo       string
	Version    string
	InstallDir string
	Current    string
	Force      bool
	HTTPClient *http.Client
	Out        io.Writer
}

type Result struct {
	CurrentVersion  string
	TargetVersion   string
	InstalledTo     string
	AlreadyUpToDate bool
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

func client(opts Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func logf(opts Options, format string, args ...any) {
	if opts.Out != nil {
		fmt.Fprintf(opts.Out, format, args...)
	}
}

func DetectPlatform() (osName, arch string, err error) {
	switch runtime.GOOS {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "darwin"
	default:
		return "", "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
	return osName, arch, nil
}

func FetchLatestRelease(repo string, httpClient *http.Client) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("fetch latest release: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	if info.TagName == "" {
		return "", fmt.Errorf("latest release has no tag_name")
	}
	return info.TagName, nil
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func isUpToDate(current, target string) bool {
	cur := normalizeVersion(current)
	tgt := normalizeVersion(target)
	if cur == "" || cur == "dev" {
		return false
	}
	return cur == tgt
}

func resolveInstallDir(installDir string) (string, error) {
	if installDir != "" {
		return installDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func downloadFile(client *http.Client, url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}

func verifyChecksum(archivePath, checksumsPath string) error {
	name := filepath.Base(archivePath)
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}
	var expected string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == name {
			expected = parts[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum not found for %s", name)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func extractBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Name != binaryName {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("%s binary not found in archive", binaryName)
}

func installBinary(src, installDir string) (string, error) {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", fmt.Errorf("create install directory: %w", err)
	}
	info, err := os.Stat(installDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", installDir)
	}

	testFile := filepath.Join(installDir, writeTestFile)
	if err := os.WriteFile(testFile, []byte("ok"), 0o644); err != nil {
		return "", fmt.Errorf("no write permission for %s; try sudo or --install-dir", installDir)
	}
	_ = os.Remove(testFile)

	dest := filepath.Join(installDir, binaryName)
	tmp := dest + ".new"
	if err := os.Rename(src, tmp); err != nil {
		if err := copyFile(src, tmp); err != nil {
			return "", err
		}
		_ = os.Remove(src)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}
	return dest, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func Update(opts Options) (*Result, error) {
	repo := opts.Repo
	if repo == "" {
		repo = DefaultRepo
	}

	osName, arch, err := DetectPlatform()
	if err != nil {
		return nil, err
	}

	httpClient := client(opts)

	targetVersion := strings.TrimSpace(opts.Version)
	if targetVersion == "" || targetVersion == "latest" {
		targetVersion, err = FetchLatestRelease(repo, httpClient)
		if err != nil {
			return nil, err
		}
	}

	result := &Result{
		CurrentVersion: opts.Current,
		TargetVersion:  targetVersion,
	}
	if !opts.Force && isUpToDate(opts.Current, targetVersion) {
		result.AlreadyUpToDate = true
		logf(opts, "%s %s is already up to date\n", binaryName, opts.Current)
		return result, nil
	}

	installDir, err := resolveInstallDir(opts.InstallDir)
	if err != nil {
		return nil, err
	}

	asset := fmt.Sprintf("%s_%s_%s_%s.tar.gz", binaryName, targetVersion, osName, arch)
	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, targetVersion)
	archiveURL := baseURL + "/" + asset
	checksumsURL := baseURL + "/checksums.txt"

	tmpDir, err := os.MkdirTemp("", binaryName+"-update-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	binaryPath := filepath.Join(tmpDir, binaryName)

	logf(opts, "Downloading %s...\n", asset)
	if err := downloadFile(httpClient, archiveURL, archivePath); err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	if err := downloadFile(httpClient, checksumsURL, checksumsPath); err != nil {
		return nil, fmt.Errorf("download checksums.txt: %w", err)
	}
	if err := verifyChecksum(archivePath, checksumsPath); err != nil {
		return nil, err
	}
	if err := extractBinary(archivePath, binaryPath); err != nil {
		return nil, err
	}

	installedTo, err := installBinary(binaryPath, installDir)
	if err != nil {
		return nil, err
	}
	result.InstalledTo = installedTo
	logf(opts, "Installed %s %s to %s\n", binaryName, targetVersion, installedTo)
	return result, nil
}
