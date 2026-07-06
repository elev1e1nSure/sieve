package runner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Runner struct {
	mu        sync.Mutex
	active    *Process
	winwsPath string
	clean     bool
}

type StopResult struct {
	Active bool
	Forced bool
	Legacy bool
}

type processGroup interface {
	Assign(pid int) error
	Terminate() error
	Close() error
}

type ProcessRunner interface {
	Prepare(winwsPath string) error
	Start(winwsPath string, args []string) (*Process, error)
	Stop() error
}

type Process struct {
	cmd         *exec.Cmd
	group       processGroup
	logs        chan string
	done        chan struct{}
	stopCh      chan struct{}
	scansWg     sync.WaitGroup
	stopOnce    sync.Once
	groupOnce   sync.Once
	mu          sync.Mutex
	waitErr     error
	stopErr     error
	stopping    bool
	groupClosed bool
}

func New() *Runner {
	return &Runner{clean: true}
}

func (r *Runner) Prepare(winwsPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.winwsPath = winwsPath
	r.clean = false
	if _, err := terminateLegacyProcesses(winwsPath); err != nil {
		return err
	}
	if err := cleanupSystem(); err != nil {
		return err
	}
	r.clean = true
	return nil
}

func (r *Runner) Start(winwsPath string, args []string) (*Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active != nil {
		select {
		case <-r.active.done:
			r.active = nil
		default:
			return nil, errors.New("winws process is already running")
		}
	}

	group, err := newProcessGroup()
	if err != nil {
		return nil, fmt.Errorf("create winws process group: %w", err)
	}

	cmd := exec.Command(winwsPath, args...)
	configureCommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		group.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		group.Close()
		return nil, err
	}

	process := &Process{
		cmd:    cmd,
		group:  group,
		logs:   make(chan string, 256),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		group.Close()
		return nil, err
	}
	r.clean = false
	if err := group.Assign(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		group.Close()
		_, legacyErr := terminateLegacyProcesses(winwsPath)
		cleanupErr := cleanupSystem()
		if legacyErr == nil && cleanupErr == nil {
			r.clean = true
		}
		return nil, errors.Join(fmt.Errorf("assign winws process group: %w", err), legacyErr, cleanupErr)
	}

	process.scansWg.Add(2)
	go process.scan(stdout)
	go process.scan(stderr)
	go process.wait()

	r.active = process
	r.winwsPath = winwsPath
	r.clean = false
	return process, nil
}

func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active == nil && r.clean {
		return nil
	}

	var stopErr error
	if r.active != nil {
		stopErr = r.active.stop()
		r.active = nil
	}
	_, legacyErr := terminateLegacyProcesses(r.winwsPath)
	cleanupErr := cleanupSystem()
	if legacyErr == nil && cleanupErr == nil {
		r.clean = true
	}

	return errors.Join(stopErr, legacyErr, cleanupErr)
}

func (p *Process) Logs() <-chan string {
	return p.logs
}

func (p *Process) Done() <-chan struct{} {
	return p.done
}

func (p *Process) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

func (p *Process) stop() error {
	select {
	case <-p.done:
		return p.Wait()
	default:
	}

	p.stopOnce.Do(func() {
		close(p.stopCh)
		// The mutex is held across Terminate so wait() cannot close the group
		// handle mid-call; a closed handle value may be recycled by the OS and
		// TerminateJobObject would then kill an unrelated job.
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.groupClosed {
			return
		}
		p.stopping = true

		if err := p.group.Terminate(); err != nil {
			p.stopErr = err
			if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				p.stopErr = errors.Join(p.stopErr, killErr)
			}
		}
	})

	waitErr := p.Wait()
	p.mu.Lock()
	stopErr := p.stopErr
	p.mu.Unlock()
	return errors.Join(stopErr, waitErr)
}

func (p *Process) scan(reader io.Reader) {
	defer p.scansWg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case p.logs <- scanner.Text():
		case <-p.stopCh:
			return
		default:
			// Process output must never block process shutdown.
		}
	}
}

func (p *Process) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.groupClosed = true
	p.groupOnce.Do(func() { _ = p.group.Close() })
	p.mu.Unlock()
	p.scansWg.Wait()

	p.mu.Lock()
	if p.stopping {
		err = nil
	}
	p.waitErr = err
	p.mu.Unlock()

	close(p.logs)
	close(p.done)
}
