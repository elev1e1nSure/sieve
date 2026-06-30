//go:build windows

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func replaceCurrentExecutable(exe, replacement string, restart bool) error {
	script, err := os.CreateTemp("", "sieve-update-*.cmd")
	if err != nil {
		return err
	}
	scriptPath := script.Name()

	content := "@echo off\r\n" +
		"ping 127.0.0.1 -n 2 > nul\r\n" +
		fmt.Sprintf("move /Y %s %s > nul\r\n", quote(replacement), quote(exe))
	if restart {
		content += fmt.Sprintf("start \"\" %s\r\n", quote(exe))
	}
	// del "%~f0" alone (or chained with "& exit /b") makes cmd.exe print
	// "The batch file cannot be found." after deleting itself, even though
	// the script already completed successfully. (goto) 2>nul is the
	// standard idiom for a clean, silent self-delete.
	content += "(goto) 2>nul & del \"%~f0\"\r\n"

	if _, err := script.WriteString(content); err != nil {
		script.Close()
		os.Remove(scriptPath)
		return err
	}
	if err := script.Close(); err != nil {
		os.Remove(scriptPath)
		return err
	}

	cmd := exec.Command("cmd", "/C", filepath.Clean(scriptPath))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
