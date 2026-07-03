//go:build windows

package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	helperFlag       = "--sieve-update-helper"
	parentWait       = 30 * time.Second
	replacementWait  = 30 * time.Second
	replacementDelay = 250 * time.Millisecond
)

func replaceCurrentExecutable(exe, replacement, version string, restart bool) error {
	helper, err := copyHelper(exe)
	if err != nil {
		return fmt.Errorf("create update helper: %w", err)
	}

	if err := clearUpdateState(); err != nil {
		os.Remove(helper)
		return fmt.Errorf("clear previous update state: %w", err)
	}

	cmd := exec.Command(
		helper,
		helperFlag,
		strconv.Itoa(os.Getpid()),
		exe,
		replacement,
		version,
		strconv.FormatBool(restart),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		os.Remove(helper)
		return fmt.Errorf("start update helper: %w", err)
	}
	_ = cmd.Process.Release()

	return nil
}

func copyHelper(exe string) (string, error) {
	source, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer source.Close()

	helper, err := os.CreateTemp("", "sieve-updater-*.exe")
	if err != nil {
		return "", err
	}
	helperPath := helper.Name()
	clean := func() {
		helper.Close()
		os.Remove(helperPath)
	}

	if _, err := io.Copy(helper, source); err != nil {
		clean()
		return "", err
	}
	if err := helper.Sync(); err != nil {
		clean()
		return "", err
	}
	if err := helper.Close(); err != nil {
		os.Remove(helperPath)
		return "", err
	}

	return helperPath, nil
}

func RunHelper(args []string) (int, bool) {
	if len(args) == 0 || args[0] != helperFlag {
		return 0, false
	}
	if len(args) != 6 {
		return 1, true
	}

	parentPID, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		return 1, true
	}
	restart, err := strconv.ParseBool(args[5])
	if err != nil {
		return 1, true
	}

	err = applyUpdate(uint32(parentPID), args[2], args[3], args[4], restart)
	if err != nil {
		_ = writeUpdateFailure(args[4], err)
		_ = os.Remove(args[3])
		scheduleSelfDelete()
		return 1, true
	}

	_ = os.Remove(args[3])
	scheduleSelfDelete()
	return 0, true
}

func applyUpdate(parentPID uint32, target, replacement, version string, restart bool) error {
	expectedHash, err := fileHash(replacement)
	if err != nil {
		return fmt.Errorf("hash downloaded executable: %w", err)
	}
	if err := waitForProcess(parentPID, parentWait); err != nil {
		return err
	}
	if err := replaceWithRetry(replacement, target, replacementWait); err != nil {
		return err
	}

	installedHash, err := fileHash(target)
	if err != nil {
		return fmt.Errorf("verify installed executable: %w", err)
	}
	if installedHash != expectedHash {
		return fmt.Errorf("installed executable hash mismatch")
	}
	if err := writeUpdateSuccess(version, installedHash); err != nil {
		return fmt.Errorf("save update receipt: %w", err)
	}
	if !restart {
		return nil
	}

	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart sieve: %w", err)
	}
	_ = cmd.Process.Release()

	return nil
}

func waitForProcess(pid uint32, timeout time.Duration) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open parent process: %w", err)
	}
	defer windows.CloseHandle(process)

	result, err := windows.WaitForSingleObject(process, uint32(timeout/time.Millisecond))
	if err != nil {
		return fmt.Errorf("wait for parent process: %w", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("parent process did not exit within %s", timeout)
	}

	return nil
}

func replaceWithRetry(replacement, target string, timeout time.Duration) error {
	staged, err := stageReplacement(replacement, target)
	if err != nil {
		return fmt.Errorf("stage replacement: %w", err)
	}
	defer os.Remove(staged)

	from, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	var replaceErr error
	for {
		replaceErr = windows.MoveFileEx(
			from,
			to,
			windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
		)
		if replaceErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replace executable within %s: %w", timeout, replaceErr)
		}
		time.Sleep(replacementDelay)
	}
}

func stageReplacement(replacement, target string) (string, error) {
	source, err := os.Open(replacement)
	if err != nil {
		return "", err
	}
	defer source.Close()

	staged, err := os.CreateTemp(filepath.Dir(target), ".sieve-update-*.exe")
	if err != nil {
		return "", err
	}
	stagedPath := staged.Name()
	clean := func() {
		staged.Close()
		os.Remove(stagedPath)
	}

	if _, err := io.Copy(staged, source); err != nil {
		clean()
		return "", err
	}
	if err := staged.Sync(); err != nil {
		clean()
		return "", err
	}
	if err := staged.Close(); err != nil {
		os.Remove(stagedPath)
		return "", err
	}
	_ = os.Remove(replacement)

	return stagedPath, nil
}

func scheduleSelfDelete() {
	helper, err := os.Executable()
	if err != nil {
		return
	}
	path, err := windows.UTF16PtrFromString(helper)
	if err != nil {
		return
	}
	_ = windows.MoveFileEx(path, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}
