//go:build windows

package runner

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sessionMutexName = `Global\Sieve.Session.v1`
	sessionJobName   = `Global\Sieve.Processes.v1`
	jobTerminate     = 0x0008
)

var openJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

type Session struct {
	mutex windows.Handle
	job   windows.Handle
}

func BeginSession() (*Session, error) {
	mutexName, err := windows.UTF16PtrFromString(sessionMutexName)
	if err != nil {
		return nil, err
	}
	mutex, err := windows.CreateMutex(nil, true, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(mutex)
		return nil, errors.New("another sieve instance is already running; use --stop first")
	}
	if err != nil {
		return nil, fmt.Errorf("create session lock: %w", err)
	}

	jobName, err := windows.UTF16PtrFromString(sessionJobName)
	if err != nil {
		windows.CloseHandle(mutex)
		return nil, err
	}
	job, err := windows.CreateJobObject(nil, jobName)
	if err != nil {
		windows.CloseHandle(mutex)
		return nil, fmt.Errorf("create session process group: %w", err)
	}
	if err := configureKillOnClose(job); err != nil {
		windows.CloseHandle(job)
		windows.CloseHandle(mutex)
		return nil, fmt.Errorf("configure session process group: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		windows.CloseHandle(mutex)
		return nil, fmt.Errorf("assign sieve to session process group: %w", err)
	}

	return &Session{mutex: mutex, job: job}, nil
}

// KeepAlive makes the lifetime dependency explicit. The handles intentionally
// remain open until process exit so Windows can terminate the job atomically.
func (s *Session) KeepAlive() {
	runtime.KeepAlive(s)
}

func terminateActiveSession() (bool, error) {
	mutexName, err := windows.UTF16PtrFromString(sessionMutexName)
	if err != nil {
		return false, err
	}
	mutex, mutexErr := windows.OpenMutex(windows.SYNCHRONIZE, false, mutexName)
	if mutexErr == nil {
		defer windows.CloseHandle(mutex)
	} else if !errors.Is(mutexErr, syscall.ERROR_FILE_NOT_FOUND) {
		return false, fmt.Errorf("open sieve session lock: %w", mutexErr)
	}

	name, err := windows.UTF16PtrFromString(sessionJobName)
	if err != nil {
		return false, err
	}
	handle, _, callErr := openJobObject.Call(jobTerminate, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		if errors.Is(callErr, syscall.ERROR_FILE_NOT_FOUND) {
			return false, nil
		}
		return false, fmt.Errorf("open sieve process group: %w", callErr)
	}
	job := windows.Handle(handle)
	defer windows.CloseHandle(job)

	if err := windows.TerminateJobObject(job, 1); err != nil {
		return false, fmt.Errorf("terminate sieve process group: %w", err)
	}
	if mutex != 0 {
		status, err := windows.WaitForSingleObject(mutex, uint32(cleanupTimeout/time.Millisecond))
		if err != nil {
			return false, fmt.Errorf("wait for sieve to stop: %w", err)
		}
		if status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
			return false, fmt.Errorf("sieve did not stop within %s", cleanupTimeout)
		}
	}
	return true, nil
}

func StopAll(winwsPath string) (bool, error) {
	stopped, sessionErr := terminateActiveSession()
	executable, executableErr := os.Executable()
	legacySieveStopped := false
	var legacySieveErr error
	if executableErr == nil {
		legacySieveStopped, legacySieveErr = terminateProcessesAtPath(executable, uint32(os.Getpid()))
	} else {
		legacySieveErr = fmt.Errorf("resolve sieve executable: %w", executableErr)
	}
	legacyStopped, legacyErr := terminateLegacyProcesses(winwsPath)
	cleanupErr := cleanupSystem()
	return stopped || legacySieveStopped || legacyStopped, errors.Join(sessionErr, legacySieveErr, legacyErr, cleanupErr)
}
