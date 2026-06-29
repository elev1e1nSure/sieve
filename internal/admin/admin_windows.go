//go:build windows

package admin

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func isAdmin() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	return token.IsElevated()
}

func elevateAndRestart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}

	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}

	args, err := windows.UTF16PtrFromString(escapeArgs(os.Args[1:]))
	if err != nil {
		return err
	}

	return windows.ShellExecute(0, verb, file, args, nil, windows.SW_NORMAL)
}

func escapeArgs(args []string) string {
	escaped := make([]string, 0, len(args))
	for _, arg := range args {
		escaped = append(escaped, windows.EscapeArg(arg))
	}

	return strings.Join(escaped, " ")
}
