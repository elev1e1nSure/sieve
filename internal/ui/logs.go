package ui

import (
	"path/filepath"
	"strings"
)

func tail(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}

	return lines[len(lines)-limit:]
}

func formatFriendlyLogs(lines []string) []string {
	events := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, line := range lines {
		event, ok := friendlyLogLine(line)
		if !ok || seen[event] {
			continue
		}

		seen[event] = true
		events = append(events, event)
	}
	if len(events) == 0 {
		return []string{mutedStyle.Render("waiting for runtime events")}
	}

	return events
}

func friendlyLogLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "github version"):
		return cleanLog("engine", strings.TrimPrefix(trimmed, "github version ")), true
	case strings.HasPrefix(lower, "we have "):
		return cleanLog("profiles", "desync profiles ready"), true
	case strings.HasPrefix(lower, "loaded ") && strings.Contains(lower, " hosts from "):
		return cleanLog("hosts", loadedFileSummary(trimmed, " hosts from ")), true
	case strings.HasPrefix(lower, "loaded ") && strings.Contains(lower, " ip/subnets from "):
		return cleanLog("ipset", loadedFileSummary(trimmed, " ip/subnets from ")), true
	case strings.Contains(lower, "windivert initialized"):
		return cleanLog("capture", "traffic capture started"), true
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return cleanLogError(trimmed), true
	default:
		return "", false
	}
}

func loadedFileSummary(line, marker string) string {
	before, after, ok := strings.Cut(line, marker)
	if !ok {
		return line
	}

	count := strings.TrimPrefix(before, "Loaded ")
	return filepath.Base(after) + "  " + count
}

func cleanLog(kind, message string) string {
	return logKindStyle.Render(kind) + " " + logMessageStyle.Render(message)
}

func cleanLogError(message string) string {
	return errorStyle.Render("error") + " " + logMessageStyle.Render(message)
}
