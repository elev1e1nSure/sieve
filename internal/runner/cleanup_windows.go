//go:build windows

package runner

import (
	"errors"
	"os/exec"
)

var windivertServices = []string{
	"WinDivert",
	"WinDivert14",
}

func killExistingProcess() error {
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

func cleanupSystem() {
	for _, service := range windivertServices {
		runCleanupCommand("sc", "stop", service)
		runCleanupCommand("sc", "delete", service)
	}
}

func runCleanupCommand(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}
