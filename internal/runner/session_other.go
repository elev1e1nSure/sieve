//go:build !windows

package runner

import "errors"

type Session struct{}

func BeginSession() (*Session, error) {
	return nil, errors.New("sieve process sessions are only supported on Windows")
}

func (s *Session) KeepAlive() {}

func StopAll(string) (bool, error) {
	return false, errors.New("--stop is only supported on Windows")
}
