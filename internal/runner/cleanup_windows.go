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
	// The driver only leaves its pending states once the kernel has finished
	// closing every WinDivert handle, which can outlast the process that held
	// them; give it more room than the plain service-deletion wait.
	serviceStopTimeout = 15 * time.Second
	pollInterval       = 50 * time.Millisecond
	maxWindowsPath     = 32768
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

func cleanupSystem() (cleanupErr error) {
	// Registered first so it runs last: every failure below leaves the driver
	// loaded, which callers that are merely preparing to start winws can treat
	// as a warning instead of a hard stop.
	defer func() {
		if cleanupErr != nil {
			cleanupErr = &CleanupError{Err: cleanupErr}
		}
	}()

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

	if err := stopService(service, serviceStopTimeout); err != nil {
		service.Close()
		return err
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

// stopService drives the WinDivert driver service to Stopped, judging success
// purely from Query()'s reported state rather than from Control(Stop)'s own
// return value. The SCM rejects a stop control (ERROR_SERVICE_CANNOT_ACCEPT_CTRL)
// while the driver sits in a pending state — exactly the window right after
// winws.exe is killed and the kernel is still tearing its handle down — and
// that rejection reason isn't reliably distinguishable at this layer from a
// foreign app genuinely holding the driver open. Treating every non-Stopped
// outcome the same and letting the poll loop run out the clock avoids
// misreporting one as the other.
func stopService(service *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query %s service while stopping: %w", winDivertService, err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		// A stop is already in flight; sending another control would only draw
		// the same rejection.
		if status.State != svc.StopPending {
			if _, err := service.Control(svc.Stop); err != nil && errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
				return nil
			}
		}

		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("WinDivert did not stop within %s; it may still be in use by another VPN, DPI bypass, or traffic-filtering app", timeout)
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
