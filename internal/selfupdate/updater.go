package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/elev1e1nSure/sieve/internal/github"
	"github.com/elev1e1nSure/sieve/internal/version"
)

const defaultAPIURL = "https://api.github.com/repos/elev1e1nSure/sieve/releases/latest"

var (
	ErrNoRelease = errors.New("latest release not found")
	ErrNoAsset   = errors.New("release has no compatible sieve binary")
	ErrGoRun     = errors.New("self-update is disabled for go run builds")
	ErrCurrent   = errors.New("already up to date")
)

type Updater struct {
	APIURL string
	Client *http.Client
}

type Result struct {
	Updated bool
	Version string
	Message string
}

func New() Updater {
	return Updater{
		APIURL: defaultAPIURL,
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (u Updater) Update(ctx context.Context, restart bool) (Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return Result{}, err
	}
	if strings.Contains(strings.ToLower(exe), `\go-build`) {
		return Result{}, ErrGoRun
	}

	latest, err := github.LatestRelease(ctx, u.client(), u.apiURL())
	if errors.Is(err, github.ErrNotFound) {
		return Result{}, ErrNoRelease
	}
	if err != nil {
		return Result{}, err
	}
	if isCurrent(latest.TagName) {
		return Result{Version: latest.TagName, Message: "already up to date"}, ErrCurrent
	}
	asset, ok := compatibleAsset(latest)
	if !ok {
		return Result{Version: latest.TagName}, ErrNoAsset
	}

	tmp, err := u.download(ctx, asset.DownloadURL)
	if err != nil {
		return Result{}, err
	}

	if err := installUpdate(exe, tmp, restart); err != nil {
		os.Remove(tmp)
		return Result{}, err
	}

	return Result{
		Updated: true,
		Version: latest.TagName,
		Message: "update scheduled",
	}, nil
}

func (u Updater) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sieve")
	github.AddAuthHeader(req)

	resp, err := u.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "sieve-update-*.exe")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}

	return tmpName, nil
}

func (u Updater) apiURL() string {
	if u.APIURL != "" {
		return u.APIURL
	}

	return defaultAPIURL
}

func (u Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}

	return http.DefaultClient
}

func compatibleAsset(r github.Release) (github.Asset, bool) {
	preferred := []string{
		"sieve-windows-amd64.exe",
		"sieve_windows_amd64.exe",
		"sieve.exe",
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return github.Asset{}, false
	}

	for _, want := range preferred {
		for _, asset := range r.Assets {
			if strings.EqualFold(asset.Name, want) && asset.DownloadURL != "" {
				return asset, true
			}
		}
	}
	for _, asset := range r.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".exe") && strings.Contains(name, "sieve") && asset.DownloadURL != "" {
			return asset, true
		}
	}

	return github.Asset{}, false
}

func currentVersion() string {
	if version.IsRelease() {
		return version.Version
	}

	return ""
}

// fileHash returns the SHA-256 of a file, used to verify a freshly written
// executable matches the bytes that were downloaded.
func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// copyFile writes src over dst, creating or truncating dst. It keeps the
// executable bit so the installed binary stays runnable on non-Windows hosts.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}

func isCurrent(latest string) bool {
	current := currentVersion()
	if current == "" {
		return false
	}
	if current == latest {
		return true
	}

	currentVersion, okCurrent := parseVersion(current)
	latestVersion, okLatest := parseVersion(latest)
	if !okCurrent || !okLatest {
		return false
	}

	return currentVersion.compare(latestVersion) >= 0
}

type semanticVersion struct {
	major int
	minor int
	patch int
}

func parseVersion(value string) (semanticVersion, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semanticVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semanticVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semanticVersion{}, false
	}

	return semanticVersion{major: major, minor: minor, patch: patch}, true
}

func (v semanticVersion) compare(other semanticVersion) int {
	switch {
	case v.major != other.major:
		return v.major - other.major
	case v.minor != other.minor:
		return v.minor - other.minor
	default:
		return v.patch - other.patch
	}
}
