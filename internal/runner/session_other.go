//go:build !windows

package runner

import "errors"

type Session struct {
	stop chan struct{}
}

func BeginSession() (*Session, error) {
	return nil, errors.New("sieve process sessions are only supported on Windows")
}

func (s *Session) StopRequested() <-chan struct{} { return s.stop }

func (s *Session) KeepAlive() {}

func StopAll(string) (StopResult, error) {
	return StopResult{}, errors.New("--stop is only supported on Windows")
}
