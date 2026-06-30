package selfupdate

import (
	"context"
	"encoding/json"
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

	"github.com/elev1e1nSure/sieve/internal/paths"
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

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
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

	latest, err := u.fetchLatest(ctx)
	if err != nil {
		return Result{}, err
	}
	if isCurrent(latest.TagName) {
		return Result{Version: latest.TagName, Message: "already up to date"}, ErrCurrent
	}
	asset, ok := latest.compatibleAsset()
	if !ok {
		return Result{Version: latest.TagName}, ErrNoAsset
	}

	tmp, err := u.download(ctx, asset.DownloadURL)
	if err != nil {
		return Result{}, err
	}

	if err := replaceCurrentExecutable(exe, tmp, restart); err != nil {
		os.Remove(tmp)
		return Result{}, err
	}
	_ = writeCurrentVersion(latest.TagName)

	return Result{
		Updated: true,
		Version: latest.TagName,
		Message: "update scheduled",
	}, nil
}

func (u Updater) fetchLatest(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.apiURL(), nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sieve")
	setAuthHeader(req)

	resp, err := u.client().Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return release{}, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("github release request failed: %s", resp.Status)
	}

	var latest release
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return release{}, err
	}
	if latest.TagName == "" {
		return release{}, ErrNoRelease
	}

	return latest, nil
}

func (u Updater) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sieve")
	setAuthHeader(req)

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

func setAuthHeader(req *http.Request) {
	token := strings.TrimSpace(os.Getenv("GH_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
}

func (r release) compatibleAsset() (releaseAsset, bool) {
	preferred := []string{
		"sieve-windows-amd64.exe",
		"sieve_windows_amd64.exe",
		"sieve.exe",
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return releaseAsset{}, false
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

	return releaseAsset{}, false
}

func currentVersion() string {
	if version.IsRelease() {
		return version.Version
	}

	return ""
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

func writeCurrentVersion(version string) error {
	if err := os.MkdirAll(filepath.Dir(versionFile()), 0o755); err != nil {
		return err
	}

	return os.WriteFile(versionFile(), []byte(strings.TrimSpace(version)+"\n"), 0o644)
}

func versionFile() string {
	return filepath.Join(paths.InstallDir(), "app-version.txt")
}
