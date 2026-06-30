package paths

import (
	"os"
	"os/exec"
	"path/filepath"
)

func InstallDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(os.TempDir(), "sieve")
	}

	return filepath.Join(configDir, "sieve")
}

func SystemRoot() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root
}

func SystemCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(filepath.Join(SystemRoot(), "System32", name+".exe"), args...)
}
