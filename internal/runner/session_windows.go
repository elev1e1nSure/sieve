//go:build windows

//nolint:errcheck
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
	sessionMutexName     = `Global\Sieve.Session.v1`
	sessionJobName       = `Global\Sieve.Processes.v1`
	sessionStopEventName = `Global\Sieve.Stop.v1`
	jobTerminate         = 0x0008
	gracefulStopTimeout  = 20 * time.Second
)

var openJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")

type Session struct {
	mutex     windows.Handle
	job       windows.Handle
	stopEvent windows.Handle
	stop      chan struct{}
}

func BeginSession() (*Session, error) {
	mutexName, err := windows.UTF16PtrFromString(sessionMutexName)
	if err != nil {
		return nil, err
	}
	mutex, err := windows.CreateMutex(nil, true, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(mutex)
		return nil, errors.New("another sieve is already running (check the system tray) — close it or run 'sieve --stop' first")
	}
	if err != nil {
		return nil, fmt.Errorf("create session lock: %w", err)
	}

	jobName, err := windows.UTF16PtrFromString(sessionJobName)
	if err != nil {
		_ = windows.CloseHandle(mutex)
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

	stopEventName, err := windows.UTF16PtrFromString(sessionStopEventName)
	if err != nil {
		windows.CloseHandle(job)
		windows.CloseHandle(mutex)
		return nil, err
	}
	stopEvent, err := windows.CreateEvent(nil, 1, 0, stopEventName)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(job)
		windows.CloseHandle(mutex)
		return nil, fmt.Errorf("create stop signal: %w", err)
	}
	if err := windows.ResetEvent(stopEvent); err != nil {
		windows.CloseHandle(stopEvent)
		windows.CloseHandle(job)
		windows.CloseHandle(mutex)
		return nil, fmt.Errorf("reset stop signal: %w", err)
	}

	session := &Session{
		mutex:     mutex,
		job:       job,
		stopEvent: stopEvent,
		stop:      make(chan struct{}),
	}
	go session.waitForStop()
	return session, nil
}

func (s *Session) StopRequested() <-chan struct{} {
	return s.stop
}

func (s *Session) waitForStop() {
	status, err := windows.WaitForSingleObject(s.stopEvent, windows.INFINITE)
	if err == nil && status == windows.WAIT_OBJECT_0 {
		close(s.stop)
	}
}

// KeepAlive makes the lifetime dependency explicit. The handles intentionally
// remain open until process exit so Windows can terminate the job atomically.
func (s *Session) KeepAlive() {
	runtime.KeepAlive(s)
}

// SessionActive reports whether another sieve instance currently holds the
// session lock, without acquiring or otherwise disturbing it.
func SessionActive() (bool, error) {
	mutex, active, err := openSessionMutex()
	if err != nil {
		return false, err
	}
	if active {
		defer windows.CloseHandle(mutex)
	}
	return active, nil
}

func StopAll(winwsPath string) (StopResult, error) {
	result, sessionErr := stopActiveSession()

	executable, executableErr := os.Executable()
	legacySieveStopped := false
	var legacySieveErr error
	if executableErr == nil {
		legacySieveStopped, legacySieveErr = terminateProcessesAtPath(executable, uint32(os.Getpid()))
	} else {
		legacySieveErr = fmt.Errorf("resolve sieve executable: %w", executableErr)
	}
	legacyWinwsStopped, legacyWinwsErr := terminateLegacyProcesses(winwsPath)
	result.Legacy = legacySieveStopped || legacyWinwsStopped

	cleanupErr := cleanupSystem()
	return result, errors.Join(sessionErr, legacySieveErr, legacyWinwsErr, cleanupErr)
}

func stopActiveSession() (StopResult, error) {
	mutex, active, err := openSessionMutex()
	if err != nil || !active {
		return StopResult{}, err
	}
	defer windows.CloseHandle(mutex)

	signaled, signalErr := signalActiveSession()
	var gracefulErr error
	if signalErr == nil && signaled {
		if exited, waitErr := waitForSessionExit(mutex, gracefulStopTimeout); waitErr != nil {
			gracefulErr = waitErr
		} else if exited {
			return StopResult{Active: true}, nil
		}
	}

	forceErr := terminateSessionJob()
	if forceErr != nil {
		return StopResult{Active: true, Forced: true}, errors.Join(signalErr, gracefulErr, forceErr)
	}
	if exited, waitErr := waitForSessionExit(mutex, cleanupTimeout); waitErr != nil {
		return StopResult{Active: true, Forced: true}, waitErr
	} else if !exited {
		return StopResult{Active: true, Forced: true}, fmt.Errorf("sieve did not exit after forced termination")
	}

	return StopResult{Active: true, Forced: true}, nil
}

func openSessionMutex() (windows.Handle, bool, error) {
	name, err := windows.UTF16PtrFromString(sessionMutexName)
	if err != nil {
		return 0, false, err
	}
	mutex, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("open sieve session: %w", err)
	}
	return mutex, true, nil
}

func signalActiveSession() (bool, error) {
	name, err := windows.UTF16PtrFromString(sessionStopEventName)
	if err != nil {
		return false, err
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open sieve stop signal: %w", err)
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		return false, fmt.Errorf("send sieve stop signal: %w", err)
	}
	return true, nil
}

func terminateSessionJob() error {
	name, err := windows.UTF16PtrFromString(sessionJobName)
	if err != nil {
		return err
	}
	handle, _, callErr := openJobObject.Call(jobTerminate, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		if errors.Is(callErr, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		return fmt.Errorf("open sieve process group: %w", callErr)
	}
	job := windows.Handle(handle)
	defer windows.CloseHandle(job)
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return fmt.Errorf("terminate sieve process group: %w", err)
	}
	return nil
}

func waitForSessionExit(mutex windows.Handle, timeout time.Duration) (bool, error) {
	status, err := windows.WaitForSingleObject(mutex, uint32(timeout/time.Millisecond))
	if err != nil {
		return false, fmt.Errorf("wait for sieve to stop: %w", err)
	}
	return status == windows.WAIT_OBJECT_0 || status == windows.WAIT_ABANDONED, nil
}
