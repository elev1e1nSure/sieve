package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesFileAndDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "file.json")

	if err := WriteAtomic(path, []byte("hello")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q, want %q", data, "hello")
	}
}

func TestWriteAtomicOverwritesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatalf("content = %q, want %q", data, "new")
	}
}

func TestWriteAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	if err := WriteAtomic(path, []byte("data")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "file.txt" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory contains %v, want only file.txt", names)
	}
}
