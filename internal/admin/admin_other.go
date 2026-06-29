//go:build !windows

package admin

import "errors"

func isAdmin() bool {
	return true
}

func elevateAndRestart() error {
	return errors.New("admin elevation is only supported on Windows")
}
