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
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	winDivertService = "WinDivert"
	cleanupTimeout   = 5 * time.Second
	pollInterval     = 50 * time.Millisecond
	maxWindowsPath   = 32768
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
	defer windows.CloseHandle(snapshot)

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
	defer windows.CloseHandle(process)

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

func cleanupSystem() (cleanupErr error) {
	manager, err := mgr.Connect()
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return errors.New("windows denied access to service cleanup; run sieve as administrator")
		}
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer func() {
		cleanupErr = errors.Join(cleanupErr, manager.Disconnect())
	}()

	service, err := manager.OpenService(winDivertService)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			return waitForServiceDeletion(manager, cleanupTimeout)
		}
		return fmt.Errorf("open %s service: %w", winDivertService, err)
	}

	status, err := service.Query()
	if err != nil {
		service.Close()
		return fmt.Errorf("query %s service: %w", winDivertService, err)
	}
	if status.State != svc.Stopped {
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			service.Close()
			return fmt.Errorf("WinDivert refused to stop; another VPN, DPI bypass, or traffic-filtering app may be using it: %w", err)
		}
		if err := waitForServiceState(service, svc.Stopped, cleanupTimeout); err != nil {
			service.Close()
			return err
		}
	}

	deleteErr := service.Delete()
	closeErr := service.Close()
	if deleteErr != nil && !errors.Is(deleteErr, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("delete %s service: %w", winDivertService, deleteErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s service: %w", winDivertService, closeErr)
	}

	return waitForServiceDeletion(manager, cleanupTimeout)
}

func waitForServiceState(service *mgr.Service, want svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query %s service while stopping: %w", winDivertService, err)
		}
		if status.State == want {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("WinDivert is still in use after %s; close other VPN, DPI bypass, or traffic-filtering apps and run sieve --stop again", timeout)
}

func waitForServiceDeletion(manager *mgr.Mgr, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		service, err := manager.OpenService(winDivertService)
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if err == nil {
			service.Close()
		} else if !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			return fmt.Errorf("verify %s service deletion: %w", winDivertService, err)
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("WinDivert remained loaded after %s; another application is still holding the driver", timeout)
}
