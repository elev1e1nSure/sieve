//go:build windows

package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// installUpdate swaps the running executable in place and, when restart is set,
// relaunches sieve in the SAME console so the update finishes before the TUI.
//
// Windows lets a running image be renamed but not overwritten, so the current
// exe is moved aside, the new bytes are copied into its path, and the result is
// verified against the downloaded hash. Everything runs in the foreground; there
// is no hidden helper process and no stray console window.
func installUpdate(exe, downloaded string, restart bool) error {
	expected, err := fileHash(downloaded)
	if err != nil {
		return fmt.Errorf("hash downloaded executable: %w", err)
	}

	backup := exe + ".old"
	_ = os.Remove(backup) // clear a leftover from a previous update
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}

	if err := copyFile(downloaded, exe); err != nil {
		// Restore the original so the user is never left without a binary.
		if restoreErr := os.Rename(backup, exe); restoreErr != nil {
			return fmt.Errorf("install update: %w (and restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("install update: %w", err)
	}
	_ = os.Remove(downloaded)

	installed, err := fileHash(exe)
	if err == nil && installed != expected {
		err = errors.New("installed executable hash mismatch")
	}
	if err != nil {
		// The freshly written exe is suspect — put the original back so the
		// user is never left with a broken binary on disk.
		_ = os.Remove(exe)
		if restoreErr := os.Rename(backup, exe); restoreErr != nil {
			return fmt.Errorf("verify installed executable: %w (and restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("verify installed executable: %w", err)
	}

	scheduleBackupCleanup(backup)

	if !restart {
		return nil
	}

	return relaunch(exe)
}

// relaunch starts the freshly installed binary attached to the current console
// (real std handles, no CREATE_NEW_CONSOLE), so the child renders normally while
// this process exits. The child inherits the elevated token, so no second UAC.
func relaunch(exe string) error {
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

// scheduleBackupCleanup removes the old image. It is still mapped by this running
// process and cannot be deleted now, so removal is deferred to reboot; the next
// start also best-effort deletes it earlier via CleanupStale.
func scheduleBackupCleanup(backup string) {
	path, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return
	}
	_ = windows.MoveFileEx(path, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}

// CleanupStale removes a leftover ".old" backup from a previous update. By the
// next start the old process has exited, so the file is no longer in use.
func CleanupStale() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}
