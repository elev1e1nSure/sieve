//go:build !windows

package maintenance

// EnsureTCPTimestamps is a no-op off Windows, where winws does not run.
func EnsureTCPTimestamps() (bool, error) {
	return false, nil
}

func diagnosticsItems(_ string, _ bool) []Item {
	return []Item{{
		Status:  "warn",
		Name:    "platform",
		Message: "diagnostics are only available on Windows",
	}}
}

func statusItems(_ string) []Item {
	return []Item{{
		Status:  "warn",
		Name:    "platform",
		Message: "status is only available on Windows",
	}}
}

func clearDiscordCacheItems() []Item {
	return []Item{{
		Status:  "warn",
		Name:    "platform",
		Message: "Discord cache cleanup is only available on Windows",
	}}
}
