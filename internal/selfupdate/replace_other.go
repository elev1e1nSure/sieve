//go:build !windows

package selfupdate

import "os"

func replaceCurrentExecutable(exe, replacement string, restart bool) error {
	if err := os.Rename(replacement, exe); err != nil {
		return err
	}

	return nil
}
