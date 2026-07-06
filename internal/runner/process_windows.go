//go:build windows

package runner

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureCommand detaches winws from sieve's console. A child console
// process attached to the same console can reset its mode (VT processing,
// line input) and corrupt the running TUI; CREATE_NO_WINDOW gives winws its
// own invisible conhost instead. Its stdio is piped, so nothing is lost.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}

type windowsProcessGroup struct {
	handle windows.Handle
}

func newProcessGroup() (processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	if err := configureKillOnClose(job); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &windowsProcessGroup{handle: job}, nil
}

func (g *windowsProcessGroup) Assign(pid int) error {
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(g.handle, process)
}

func (g *windowsProcessGroup) Terminate() error {
	return windows.TerminateJobObject(g.handle, 1)
}

func (g *windowsProcessGroup) Close() error {
	return windows.CloseHandle(g.handle)
}

func configureKillOnClose(job windows.Handle) error {
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	return err
}
