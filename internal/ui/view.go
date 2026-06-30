package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	header := lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("sieve"), " ", m.stateBadge())
	panel := panelStyle.Width(m.viewport.Width + 2).Render(m.viewport.View())

	return header + "\n" + panel + "\n" + m.footer()
}

func (m Model) body() string {
	switch m.state {
	case StateUpdating:
		return m.updatingContent()
	case StateTesting:
		return m.testingContent()
	case StateRunning:
		return m.logContent()
	case StateNoLuck:
		return m.noLuckContent()
	case StateClosing:
		return m.closingContent()
	default:
		return ""
	}
}

func (m Model) updatingContent() string {
	title := sectionTitleStyle.Render(m.spinner.View() + " Updating assets")
	if m.progress.Total > 0 {
		return strings.Join([]string{
			title,
			keyValue("phase", m.progress.Phase),
			keyValue("status", m.progress.Message),
			progressLine(m.progress.Current, m.progress.Total),
		}, "\n")
	}

	return strings.Join([]string{
		title,
		keyValue("phase", fallback(m.progress.Phase, "starting")),
		keyValue("status", fallback(m.progress.Message, "preparing local cache")),
	}, "\n")
}

func (m Model) testingContent() string {
	return strings.Join([]string{
		sectionTitleStyle.Render(m.spinner.View() + " Testing configs"),
		keyValue("current", fallback(m.currentConfig, "starting")),
		keyValue("progress", fmt.Sprintf("%d/%d", m.configIndex, m.configTotal)),
		progressLine(int64(m.configIndex), int64(m.configTotal)),
	}, "\n")
}

func (m Model) logContent() string {
	if len(m.logs) == 0 {
		return strings.Join([]string{
			sectionTitleStyle.Render(successStyle.Render("running") + " " + valueStyle.Render(m.runningConfig)),
			mutedStyle.Render("waiting for winws output"),
		}, "\n")
	}
	if m.rawLogMode {
		return strings.Join([]string{
			sectionTitleStyle.Render(successStyle.Render("running") + " " + valueStyle.Render(m.runningConfig) + " " + mutedStyle.Render("raw")),
			logStyle.Render(strings.Join(tail(m.logs, 200), "\n")),
		}, "\n")
	}

	return strings.Join([]string{
		sectionTitleStyle.Render(successStyle.Render("running") + " " + valueStyle.Render(m.runningConfig)),
		strings.Join(formatFriendlyLogs(tail(m.logs, 200)), "\n"),
	}, "\n")
}

func (m Model) noLuckContent() string {
	if m.err != nil {
		return strings.Join([]string{
			sectionTitleStyle.Render(errorStyle.Render("failed")),
			errorStyle.Render(m.err.Error()),
		}, "\n")
	}

	return sectionTitleStyle.Render(warnStyle.Render("no working config"))
}

func (m Model) closingContent() string {
	return strings.Join([]string{
		sectionTitleStyle.Render(m.spinner.View() + " Cleaning up"),
		cleanLog("winws", "stopping process"),
		cleanLog("filters", "removing WinDivert services"),
		cleanLog("exit", "closing session"),
	}, "\n")
}

func (m Model) footer() string {
	logMode := "raw"
	if m.rawLogMode {
		logMode = "clean"
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		hint("q", "quit"),
		" ",
		hint("ctrl+c", "cleanup"),
		" ",
		hint("ctrl+o", logMode),
	)
}

func (m Model) stateBadge() string {
	switch m.state {
	case StateUpdating:
		return badgeStyle.Copy().Foreground(lipgloss.Color("39")).Render("updating")
	case StateTesting:
		return badgeStyle.Copy().Foreground(lipgloss.Color("220")).Render("testing")
	case StateRunning:
		return badgeStyle.Copy().Foreground(lipgloss.Color("42")).Render("running")
	case StateNoLuck:
		return badgeStyle.Copy().Foreground(lipgloss.Color("196")).Render("stopped")
	case StateClosing:
		return badgeStyle.Copy().Foreground(lipgloss.Color("213")).Render("cleanup")
	default:
		return badgeStyle.Render("idle")
	}
}

func keyValue(key, value string) string {
	return labelStyle.Render(key) + " " + valueStyle.Render(value)
}

func hint(key, label string) string {
	return hintStyle.Render(key) + hintTextStyle.Render(" "+label)
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}

	return value
}

func progressLine(current, total int64) string {
	if total <= 0 {
		return mutedStyle.Render("progress    waiting")
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}

	const width = 24
	filled := int(current * width / total)
	bar := progressFilledStyle.Render(strings.Repeat("■", filled)) + progressEmptyStyle.Render(strings.Repeat("□", width-filled))

	return labelStyle.Render("progress") + " " + bar + mutedStyle.Render(fmt.Sprintf(" %d%%", current*100/total))
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))
	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("245"))
	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))
	progressFilledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39"))
	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("238"))
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("236")).
			Padding(1, 2).
			MarginTop(1).
			MarginBottom(1)
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252")).
				MarginBottom(1)
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Width(10)
	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252"))
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
	hintStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("238")).
			Padding(0, 1)
	hintTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42"))
	warnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("220"))
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))
	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))
	logKindStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))
	logMessageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
)
