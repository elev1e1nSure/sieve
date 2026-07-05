package selfupdate

import (
	"runtime"
	"testing"

	"github.com/elev1e1nSure/sieve/internal/github"
	"github.com/elev1e1nSure/sieve/internal/version"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want semanticVersion
		ok   bool
	}{
		{"1.2.3", semanticVersion{1, 2, 3}, true},
		{"v1.2.3", semanticVersion{1, 2, 3}, true},
		{" v0.5.2 ", semanticVersion{0, 5, 2}, true},
		{"1.2", semanticVersion{}, false},
		{"1.2.3.4", semanticVersion{}, false},
		{"a.b.c", semanticVersion{}, false},
		{"", semanticVersion{}, false},
	}

	for _, c := range cases {
		got, ok := parseVersion(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseVersion(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSemanticVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		sign int // -1, 0, 1
	}{
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.0", "1.3.0", -1},
		{"1.2.3", "1.2.4", -1},
		{"1.2.3", "1.2.3", 0},
	}

	for _, c := range cases {
		a, _ := parseVersion(c.a)
		b, _ := parseVersion(c.b)
		got := a.compare(b)
		switch {
		case c.sign < 0 && got >= 0, c.sign > 0 && got <= 0, c.sign == 0 && got != 0:
			t.Errorf("compare(%s, %s) = %d, want sign %d", c.a, c.b, got, c.sign)
		}
	}
}

func TestIsCurrent(t *testing.T) {
	restore := version.Version
	defer func() { version.Version = restore }()

	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"dev", "v1.0.0", false},     // dev builds never count as current
		{"v1.0.0", "v1.0.0", true},   // exact match
		{"v1.0.0", "v1.0.1", false},  // patch behind
		{"v1.1.0", "v1.0.9", true},   // ahead of latest
		{"v2.0.0", "v10.0.0", false}, // numeric, not lexicographic
		{"custom", "v1.0.0", false},  // unparsable current, no match
		{"custom", "custom", true},   // unparsable but equal strings
	}

	for _, c := range cases {
		version.Version = c.current
		if got := isCurrent(c.latest); got != c.want {
			t.Errorf("isCurrent(%q) with current %q = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestCompatibleAsset(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("compatibleAsset only matches on windows/amd64")
	}

	release := github.Release{Assets: []github.Asset{
		{Name: "sieve.exe", DownloadURL: "https://example.com/legacy"},
		{Name: "sieve-windows-amd64.exe", DownloadURL: "https://example.com/canonical"},
		{Name: "sieve-linux-amd64", DownloadURL: "https://example.com/linux"},
	}}

	asset, ok := compatibleAsset(release)
	if !ok || asset.Name != "sieve-windows-amd64.exe" {
		t.Fatalf("asset = %+v, %v; want canonical name preferred", asset, ok)
	}

	legacy := github.Release{Assets: []github.Asset{
		{Name: "SIEVE.EXE", DownloadURL: "https://example.com/legacy"},
	}}
	asset, ok = compatibleAsset(legacy)
	if !ok || asset.DownloadURL != "https://example.com/legacy" {
		t.Fatalf("legacy asset = %+v, %v; want case-insensitive sieve.exe fallback", asset, ok)
	}

	fuzzy := github.Release{Assets: []github.Asset{
		{Name: "some-other.zip", DownloadURL: "https://example.com/zip"},
		{Name: "sieve-v2-windows.exe", DownloadURL: "https://example.com/fuzzy"},
	}}
	asset, ok = compatibleAsset(fuzzy)
	if !ok || asset.DownloadURL != "https://example.com/fuzzy" {
		t.Fatalf("fuzzy asset = %+v, %v; want *sieve*.exe fallback", asset, ok)
	}

	if _, ok := compatibleAsset(github.Release{}); ok {
		t.Fatal("empty release should have no compatible asset")
	}
}
