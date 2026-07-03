//go:build !windows

package selfupdate

import "os"

func replaceCurrentExecutable(exe, replacement, version string, restart bool) error {
	if err := os.Rename(replacement, exe); err != nil {
		return err
	}

	return nil
}

func RunHelper(_ []string) (int, bool) {
	return 0, false
}
