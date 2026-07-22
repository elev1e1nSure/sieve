//go:build windows

package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cleanupTimeout = 5 * time.Second
	maxWindowsPath = 32768
)

func terminateLegacyProcesses(winwsPath string) (bool, error) {
	return terminateProcessesAtPath(winwsPath, 0)
}

func terminateProcessesAtPath(executablePath string, excludePID uint32) (bool, error) {
	if executablePath == "" {
		return false, nil
	}

	target, err := filepath.Abs(executablePath)
	if err != nil {
		return false, fmt.Errorf("resolve executable path: %w", err)
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, fmt.Errorf("list processes: %w", err)
	}
	defer windows.CloseHandle(snapshot) //nolint:errcheck

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return false, nil
		}
		return false, fmt.Errorf("read process list: %w", err)
	}

	stopped := false
	var cleanupErr error
	for {
		image := windows.UTF16ToString(entry.ExeFile[:])
		if entry.ProcessID != excludePID && strings.EqualFold(image, filepath.Base(target)) {
			matched, err := terminateProcessAtPath(entry.ProcessID, target)
			stopped = stopped || matched
			if err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return stopped, errors.Join(cleanupErr, fmt.Errorf("read process list: %w", err))
		}
	}

	return stopped, cleanupErr
}

func terminateProcessAtPath(pid uint32, target string) (bool, error) {
	process, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false,
		pid,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process) //nolint:errcheck

	path, err := processImagePath(process)
	if err != nil {
		if errors.Is(err, windows.ERROR_GEN_FAILURE) || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, fmt.Errorf("read process %d path: %w", pid, err)
	}
	if !filepath.IsAbs(path) || !equalPath(path, target) {
		return false, nil
	}

	if err := windows.TerminateProcess(process, 1); err != nil {
		return true, fmt.Errorf("terminate process %d at %s: %w", pid, target, err)
	}
	if status, err := windows.WaitForSingleObject(process, uint32(cleanupTimeout/time.Millisecond)); err != nil {
		return true, fmt.Errorf("wait for process %d at %s: %w", pid, target, err)
	} else if status != windows.WAIT_OBJECT_0 {
		return true, fmt.Errorf("process %d at %s did not stop within %s", pid, target, cleanupTimeout)
	}

	return true, nil
}

func processImagePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, maxWindowsPath)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func equalPath(left, right string) bool {
	leftPath, leftErr := filepath.EvalSymlinks(left)
	rightPath, rightErr := filepath.EvalSymlinks(right)
	if leftErr == nil {
		left = leftPath
	}
	if rightErr == nil {
		right = rightPath
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
