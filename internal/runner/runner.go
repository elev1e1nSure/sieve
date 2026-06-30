package runner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const filterClearDelay = 800 * time.Millisecond

type Runner struct{}

type ProcessRunner interface {
	KillExisting() error
	Cleanup()
	Start(winwsPath string, args []string) (*Process, error)
}

type Process struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	logs    chan string
	done    chan struct{}
	stopCh  chan struct{}
	scansWg sync.WaitGroup
	once    sync.Once
	mu      sync.Mutex
	waitErr error
	stopped bool
}

func New() Runner {
	return Runner{}
}

func (r Runner) KillExisting() error {
	if err := killExistingProcess(); err != nil {
		return err
	}
	cleanupSystem()

	time.Sleep(filterClearDelay)
	return nil
}

func (r Runner) Cleanup() {
	_ = killExistingProcess()
	cleanupSystem()
	time.Sleep(filterClearDelay)
}

func (r Runner) Start(winwsPath string, args []string) (*Process, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, winwsPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	process := &Process{
		cmd:    cmd,
		cancel: cancel,
		logs:   make(chan string, 256),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	process.scansWg.Add(2)
	go process.scan(stdout)
	go process.scan(stderr)
	go process.wait()

	return process, nil
}

func (p *Process) Logs() <-chan string {
	return p.logs
}

func (p *Process) Stop() error {
	select {
	case <-p.done:
		err := p.Wait()
		cleanupSystem()
		time.Sleep(filterClearDelay)
		return err
	default:
	}

	p.once.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		p.stopped = true
		p.mu.Unlock()
		p.cancel()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})

	err := p.Wait()
	cleanupSystem()
	time.Sleep(filterClearDelay)
	if errors.Is(err, context.Canceled) || errors.Is(err, os.ErrProcessDone) || isKilled(err) {
		return nil
	}

	return err
}

func (p *Process) Wait() error {
	<-p.done

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.waitErr
}

func (p *Process) scan(reader io.Reader) {
	defer p.scansWg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case p.logs <- scanner.Text():
		case <-p.stopCh:
			return
		}
	}
}

func (p *Process) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	if p.stopped && isKilled(err) {
		err = nil
	}
	p.waitErr = err
	p.mu.Unlock()

	close(p.done)
	p.scansWg.Wait()
	close(p.logs)
}

func isKilled(err error) bool {
	if err == nil {
		return false
	}

	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
