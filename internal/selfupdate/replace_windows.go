//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
	"strings"
)

func replaceCurrentExecutable(exe, replacement string, restart bool) error {
	args := []string{
		"/C",
		"ping 127.0.0.1 -n 2 > nul && move /Y " + quote(replacement) + " " + quote(exe) + " > nul",
	}
	if restart {
		args[2] += " && " + quote(exe)
	}

	cmd := exec.Command("cmd", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Start()
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
