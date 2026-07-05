package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestRelease(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"sieve.exe","browser_download_url":"https://example.com/dl","size":42}]}`))
	}))
	defer server.Close()

	t.Setenv("GH_TOKEN", "test-token")
	release, err := LatestRelease(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Errorf("TagName = %q", release.TagName)
	}
	if len(release.Assets) != 1 || release.Assets[0].Size != 42 {
		t.Errorf("Assets = %+v", release.Assets)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want bearer token from GH_TOKEN", gotAuth)
	}
}

func TestLatestReleaseNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer server.Close()

	_, err := LatestRelease(context.Background(), server.Client(), server.URL)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLatestReleaseEmptyTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":""}`))
	}))
	defer server.Close()

	_, err := LatestRelease(context.Background(), server.Client(), server.URL)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for empty tag", err)
	}
}

func TestLatestReleaseServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := LatestRelease(context.Background(), server.Client(), server.URL)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want non-ErrNotFound error", err)
	}
}
