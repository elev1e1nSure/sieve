//go:build !windows

package settings

type DiagnosticsReport struct {
	Items []DiagnosticItem
}

type DiagnosticItem struct {
	Status  string
	Name    string
	Message string
}

func RunDiagnostics(_ string, _ bool) DiagnosticsReport {
	return DiagnosticsReport{Items: []DiagnosticItem{{
		Status:  "warn",
		Name:    "platform",
		Message: "diagnostics are only available on Windows",
	}}}
}

func ClearDiscordCache() DiagnosticsReport {
	return DiagnosticsReport{Items: []DiagnosticItem{{
		Status:  "warn",
		Name:    "platform",
		Message: "Discord cache cleanup is only available on Windows",
	}}}
}
