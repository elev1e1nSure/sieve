package runner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const filterClearDelay = 800 * time.Millisecond

var windivertServices = []string{
	"WinDivert",
	"WinDivert14",
}

type Runner struct{}

type Process struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	logs    chan string
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	waitErr error
	stopped bool
}

func New() Runner {
	return Runner{}
}

func (r Runner) KillExisting() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	if err := killWinWS(); err != nil {
		return err
	}
	cleanupWinDivert()

	time.Sleep(filterClearDelay)
	return nil
}

func (r Runner) Cleanup() {
	if runtime.GOOS != "windows" {
		return
	}

	_ = killWinWS()
	cleanupWinDivert()
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
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

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
		cleanupWinDivert()
		time.Sleep(filterClearDelay)
		return err
	default:
	}

	p.once.Do(func() {
		p.mu.Lock()
		p.stopped = true
		p.mu.Unlock()
		p.cancel()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})

	err := p.Wait()
	cleanupWinDivert()
	time.Sleep(filterClearDelay)
	if errors.Is(err, context.Canceled) || errors.Is(err, os.ErrProcessDone) || isKilled(err) {
		return nil
	}

	return err
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

func (p *Process) scan(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		p.logs <- scanner.Text()
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
	close(p.logs)
}

func isKilled(err error) bool {
	if err == nil {
		return false
	}

	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func killWinWS() error {
	cmd := exec.Command("taskkill", "/IM", "winws.exe", "/F", "/T")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}

		return err
	}

	return nil
}

func cleanupWinDivert() {
	if runtime.GOOS != "windows" {
		return
	}

	for _, service := range windivertServices {
		runCleanupCommand("sc", "stop", service)
		runCleanupCommand("sc", "delete", service)
	}
}

func runCleanupCommand(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}
