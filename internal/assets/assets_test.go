package assets

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExtractReleaseReplacesStrategies(t *testing.T) {
	installDir := t.TempDir()
	stale := filepath.Join(installDir, strategiesDir, "general (OLD).bat")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := writeZip(t, map[string]string{
		"zapret/bin/winws.exe":          "exe",
		"zapret/lists/list-general.txt": "discord.com",
		"zapret/general.bat":            "bat",
		"zapret/general (ALT).bat":      "bat",
		"zapret/service.bat":            "service",
		"zapret/utils/test zapret.ps1":  "script",
	})

	if err := extractRelease(zipPath, installDir); err != nil {
		t.Fatalf("extractRelease() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(installDir, strategiesDir))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if want := []string{"general (ALT).bat", "general.bat"}; !slices.Equal(got, want) {
		t.Fatalf("strategies = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(installDir, "bin", "winws.exe")); err != nil {
		t.Fatalf("bin not extracted: %v", err)
	}
}

func TestExtractReleaseWithoutStrategiesLeavesEmptyDir(t *testing.T) {
	installDir := t.TempDir()
	zipPath := writeZip(t, map[string]string{
		"bin/winws.exe":          "exe",
		"lists/list-general.txt": "discord.com",
	})

	if err := extractRelease(zipPath, installDir); err != nil {
		t.Fatalf("extractRelease() error = %v", err)
	}
	if !dirExists(filepath.Join(installDir, strategiesDir)) {
		t.Fatal("strategies dir missing; the release would be downloaded again on every start")
	}
}

func TestStrategyPath(t *testing.T) {
	tests := map[string]bool{
		"zapret/general.bat":          true,
		"general (ALT11).bat":         true,
		"zapret/General (EXP).BAT":    true,
		"zapret/service.bat":          false,
		"zapret/general.txt":          false,
		"zapret/utils/check.ps1":      false,
		"zapret/bin/general-help.exe": false,
	}

	for name, want := range tests {
		if _, got := strategyPath(name); got != want {
			t.Errorf("strategyPath(%q) ok = %v, want %v", name, got, want)
		}
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "release.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}
