//go:build !windows

package runner

import "os"

type directProcessGroup struct {
	process *os.Process
}

func newProcessGroup() (processGroup, error) { return &directProcessGroup{}, nil }

func (g *directProcessGroup) Assign(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	g.process = process
	return nil
}

func (g *directProcessGroup) Terminate() error {
	if g.process == nil {
		return nil
	}
	return g.process.Kill()
}

func (*directProcessGroup) Close() error { return nil }
