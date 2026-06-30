//go:build !windows

package runner

func killExistingProcess() error {
	return nil
}

func cleanupSystem() {}
