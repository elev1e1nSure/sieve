package paths

import (
	"os"
	"path/filepath"
)

func InstallDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(os.TempDir(), "sieve")
	}

	return filepath.Join(configDir, "sieve")
}
