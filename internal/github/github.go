// Package github fetches release metadata from the GitHub API. It backs both
// the Flowseal asset downloads (internal/assets) and sieve's self-update.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// ErrNotFound is returned when the latest release does not exist or carries
// no tag.
var ErrNotFound = errors.New("latest release not found")

type Release struct {
	TagName    string  `json:"tag_name"`
	ZipballURL string  `json:"zipball_url"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

// LatestRelease fetches the latest release from a
// /repos/<owner>/<repo>/releases/latest endpoint.
func LatestRelease(ctx context.Context, client *http.Client, apiURL string) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sieve")
	AddAuthHeader(req)

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github release request failed: %s", resp.Status)
	}

	var latest Release
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return Release{}, err
	}
	if latest.TagName == "" {
		return Release{}, ErrNotFound
	}

	return latest, nil
}

// AddAuthHeader sets a bearer token from GH_TOKEN or GITHUB_TOKEN when
// present, which raises GitHub's API rate limits.
func AddAuthHeader(req *http.Request) {
	token := strings.TrimSpace(os.Getenv("GH_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
}
