//go:build !windows

package runner

func terminateLegacyProcesses(string) (bool, error) { return false, nil }

func terminateProcessesAtPath(string, uint32) (bool, error) { return false, nil }
