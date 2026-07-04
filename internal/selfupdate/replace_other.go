//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func installUpdate(exe, downloaded string, restart bool) error {
	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}

	if err := copyFile(downloaded, exe); err != nil {
		if restoreErr := os.Rename(backup, exe); restoreErr != nil {
			return fmt.Errorf("install update: %w (and restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("install update: %w", err)
	}
	_ = os.Remove(downloaded)
	_ = os.Remove(backup)

	if !restart {
		return nil
	}

	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch sieve: %w", err)
	}

	return cmd.Process.Release()
}

// CleanupStale is a no-op off Windows, where the ".old" backup is removed inline.
func CleanupStale() {}
