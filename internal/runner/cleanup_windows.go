//go:build windows

package runner

import (
	"errors"
	"os/exec"
	"time"
)

var windivertServices = []string{
	"WinDivert",
	"WinDivert14",
	"WinDivert1.4",
	"WinDivert2.2",
}

func killExistingProcess() error {
	cmd := exec.Command("taskkill", "/IM", "winws.exe", "/F", "/T")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return err
		}
	}

	return nil
}

func cleanupSystem() {
	killExistingProcess()
	time.Sleep(200 * time.Millisecond)

	for _, service := range windivertServices {
		runCleanupCommand("sc", "stop", service)
		runCleanupCommand("net", "stop", service)
	}
	time.Sleep(100 * time.Millisecond)

	for _, service := range windivertServices {
		runCleanupCommand("sc", "delete", service)
	}
}

func runCleanupCommand(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}
