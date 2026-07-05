package assets

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elev1e1nSure/sieve/internal/github"
	"github.com/elev1e1nSure/sieve/internal/paths"
)

const (
	defaultGitHubAPI = "https://api.github.com/repos/Flowseal/zapret-discord-youtube/releases/latest"
	versionFileName  = "version.txt"
)

type Manager struct {
	InstallDir string
	APIURL     string
	Client     *http.Client
}

// AssetManager is the slice of Manager the TUI consumes.
type AssetManager interface {
	Ensure(ctx context.Context, progress func(Progress)) (Info, error)
}

type Progress struct {
	Phase   string
	Message string
	Current int64
	Total   int64
}

type Info struct {
	Version    string
	Updated    bool
	InstallDir string
	BinDir     string
	ListsDir   string
}

func NewManager() Manager {
	return Manager{
		InstallDir: paths.InstallDir(),
		APIURL:     defaultGitHubAPI,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (m Manager) BinDir() string {
	return filepath.Join(m.InstallDir, "bin")
}

func (m Manager) ListsDir() string {
	return filepath.Join(m.InstallDir, "lists")
}

func (m Manager) VersionFile() string {
	return filepath.Join(m.InstallDir, versionFileName)
}

func (m Manager) Ensure(ctx context.Context, progress func(Progress)) (Info, error) {
	progress(Progress{Phase: "checking", Message: "checking flowseal release"})

	if err := os.MkdirAll(m.InstallDir, 0o755); err != nil {
		return Info{}, err
	}

	latest, err := github.LatestRelease(ctx, m.client(), m.APIURL)
	if err != nil {
		return Info{}, fmt.Errorf("flowseal release: %w", err)
	}

	localVersion, _ := os.ReadFile(m.VersionFile())
	if strings.TrimSpace(string(localVersion)) == latest.TagName && m.hasRequiredDirs() {
		if err := ensureUserListFiles(m.ListsDir()); err != nil {
			return Info{}, err
		}
		return m.info(latest.TagName, false), nil
	}

	downloadURL, expectedSize, err := zipTarget(latest)
	if err != nil {
		return Info{}, err
	}

	progress(Progress{Phase: "downloading", Message: "downloading flowseal assets", Total: expectedSize})
	zipPath, err := m.download(ctx, downloadURL, expectedSize, progress)
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(zipPath)

	progress(Progress{Phase: "extracting", Message: "extracting bin and lists"})
	if err := extractBinLists(zipPath, m.InstallDir); err != nil {
		return Info{}, err
	}
	if err := ensureUserListFiles(m.ListsDir()); err != nil {
		return Info{}, err
	}

	if err := os.WriteFile(m.VersionFile(), []byte(latest.TagName+"\n"), 0o644); err != nil {
		return Info{}, err
	}

	return m.info(latest.TagName, true), nil
}

func (m Manager) download(ctx context.Context, url string, expectedSize int64, progress func(Progress)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "sieve")

	resp, err := m.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("flowseal asset download failed: %s", resp.Status)
	}

	total := expectedSize
	if total <= 0 {
		total = resp.ContentLength
	}

	tmp, err := os.CreateTemp("", "sieve-flowseal-*.zip")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	writer := &progressWriter{
		report: func(current int64) {
			progress(Progress{
				Phase:   "downloading",
				Message: "downloading flowseal assets",
				Current: current,
				Total:   total,
			})
		},
	}

	if _, err := io.Copy(tmp, io.TeeReader(resp.Body, writer)); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

func (m Manager) hasRequiredDirs() bool {
	return dirExists(m.BinDir()) && dirExists(m.ListsDir())
}

func (m Manager) info(version string, updated bool) Info {
	return Info{
		Version:    version,
		Updated:    updated,
		InstallDir: m.InstallDir,
		BinDir:     m.BinDir(),
		ListsDir:   m.ListsDir(),
	}
}

func (m Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}

	return http.DefaultClient
}

func zipTarget(r github.Release) (string, int64, error) {
	for _, asset := range r.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") && asset.DownloadURL != "" {
			return asset.DownloadURL, asset.Size, nil
		}
	}

	if r.ZipballURL != "" {
		return r.ZipballURL, 0, nil
	}

	return "", 0, errors.New("latest flowseal release has no zip asset")
}

type progressWriter struct {
	current int64
	report  func(current int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.current += int64(n)
	w.report(w.current)

	return n, nil
}

func extractBinLists(zipPath, installDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	stagingDir, err := os.MkdirTemp(installDir, ".extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingDir)

	extracted := 0
	for _, file := range reader.File {
		targetRel, ok := binOrListsPath(file.Name)
		if !ok {
			continue
		}
		if err := extractFile(file, filepath.Join(stagingDir, targetRel)); err != nil {
			return err
		}
		extracted++
	}

	if extracted == 0 {
		return errors.New("flowseal archive does not contain bin or lists")
	}
	if !dirExists(filepath.Join(stagingDir, "bin")) || !dirExists(filepath.Join(stagingDir, "lists")) {
		return errors.New("flowseal archive must contain both bin and lists")
	}

	return replaceDirs(stagingDir, installDir)
}

func extractFile(file *zip.File, target string) error {
	cleanTarget := filepath.Clean(target)
	if file.FileInfo().IsDir() {
		return os.MkdirAll(cleanTarget, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
		return err
	}

	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func binOrListsPath(name string) (string, bool) {
	normalized := strings.TrimLeft(filepath.ToSlash(name), "/")
	parts := strings.Split(normalized, "/")
	for i, part := range parts {
		if part != "bin" && part != "lists" {
			continue
		}

		relParts := parts[i:]
		for _, relPart := range relParts {
			if relPart == "" || relPart == "." || relPart == ".." || strings.ContainsAny(relPart, `:\`) {
				return "", false
			}
		}

		return filepath.Join(relParts...), true
	}

	return "", false
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func replaceDirs(stagingDir, installDir string) error {
	for _, name := range []string{"bin", "lists"} {
		src := filepath.Join(stagingDir, name)
		if !dirExists(src) {
			continue
		}

		dst := filepath.Join(installDir, name)
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	return nil
}

func ensureUserListFiles(listsDir string) error {
	for _, name := range []string{
		"list-general-user.txt",
		"list-exclude-user.txt",
		"ipset-exclude-user.txt",
	} {
		path := filepath.Join(listsDir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if err := os.WriteFile(path, nil, 0o644); err != nil {
			return err
		}
	}

	return nil
}
