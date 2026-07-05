package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Example.COM", "example.com"},
		{"https://example.com/path?q=1", "example.com"},
		{"http://example.com", "example.com"},
		{" example.com. ", "example.com"},
		{".example.com", "example.com"},
		{"exa*mple.com", ""},
		{"exa<mple.com", ""},
		{"", ""},
		{"https://", ""},
	}

	for _, c := range cases {
		if got := normalizeDomain(c.in); got != c.want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitDomains(t *testing.T) {
	got := splitDomains("a.com, b.com;c.com\nd.com\te.com bad*char.com")
	want := []string{"a.com", "b.com", "c.com", "d.com", "e.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitDomains = %v, want %v", got, want)
	}
}

func TestCollectDomainsDedupesAndSorts(t *testing.T) {
	got, err := collectDomains([]string{"b.com, a.com", "a.com"}, nil)
	if err != nil {
		t.Fatalf("collectDomains: %v", err)
	}
	want := []string{"a.com", "b.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectDomains = %v, want %v", got, want)
	}
}

func TestIsEmptyOrSentinel(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"  \n ", true},
		{ipsetSentinel + "\n", true},
		{"1.2.3.4/32\n", false},
		{ipsetSentinel + "\n1.2.3.4/32\n", false},
	}

	for _, c := range cases {
		if got := isEmptyOrSentinel([]byte(c.in)); got != c.want {
			t.Errorf("isEmptyOrSentinel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestReplaceManagedDomainsCreatesBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list-general-user.txt")
	if err := os.WriteFile(path, []byte("user-line.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceManagedDomains(path, []string{"a.com", "b.com"}); err != nil {
		t.Fatalf("replaceManagedDomains: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	for _, want := range []string{"user-line.com", managedStart, "a.com", "b.com", managedEnd} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q:\n%s", want, content)
		}
	}
}

func TestReplaceManagedDomainsReplacesAndRemoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.txt")
	initial := strings.Join([]string{
		"user-line.com",
		"",
		managedStart,
		"stale.com",
		managedEnd,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	// Replacing swaps the managed block contents and keeps user lines.
	if err := replaceManagedDomains(path, []string{"fresh.com"}); err != nil {
		t.Fatalf("replaceManagedDomains: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "stale.com") || !strings.Contains(string(data), "fresh.com") {
		t.Fatalf("managed block not replaced:\n%s", data)
	}

	// An empty domain list removes the managed block entirely.
	if err := replaceManagedDomains(path, nil); err != nil {
		t.Fatalf("replaceManagedDomains: %v", err)
	}
	data, _ = os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, managedStart) || strings.Contains(content, "fresh.com") {
		t.Fatalf("managed block not removed:\n%s", content)
	}
	if !strings.Contains(content, "user-line.com") {
		t.Fatalf("user content lost:\n%s", content)
	}
}
