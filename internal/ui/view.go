package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/elev1e1nSure/sieve/internal/version"
)

func (m Model) View() string {
	if m.ui.state == StateBye {
		if m.ui.exitErr != nil {
			message := strings.ReplaceAll(formatExitError(m.ui.exitErr), "\n", "\n    ")
			return "\n  " + errorStyle.Render("✗ sieve stopped with an error") + "\n    " + mutedStyle.Render(message) + "\n"
		}
		return "\n  " + successStyle.Render("✓ "+fallback(m.ui.exitReason, "sieve stopped cleanly")) + "\n"
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		dotStyle.Render("●")+" "+titleStyle.Render("sieve"),
		" ",
		versionStyle.Render(version.Version),
		" ",
		m.stateBadge(),
	)
	panel := panelStyle.Width(m.ui.viewport.Width + 2).Render(m.ui.viewport.View())

	return header + "\n" + panel + "\n" + m.footer()
}

func formatExitError(err error) string {
	seen := make(map[string]bool)
	lines := make([]string, 0, 4)
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) body() string {
	switch m.ui.state {
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
	title := sectionTitleStyle.Render(m.ui.spinner.View() + " Updating assets")
	if m.flow.progress.Total > 0 {
		lines := []string{
			title,
			keyValue("phase", m.flow.progress.Phase),
			keyValue("status", m.flow.progress.Message),
			progressLine(m.flow.progress.Current, m.flow.progress.Total),
		}

		return strings.Join(m.withStartupNotices(lines), "\n")
	}

	lines := []string{
		title,
		keyValue("phase", fallback(m.flow.progress.Phase, "starting")),
		keyValue("status", fallback(m.flow.progress.Message, "preparing local cache")),
	}

	return strings.Join(m.withStartupNotices(lines), "\n")
}

func (m Model) testingContent() string {
	lines := []string{
		sectionTitleStyle.Render(m.ui.spinner.View() + " Testing configs"),
		keyValue("current", fallback(m.flow.currentConfig, "starting")),
		keyValue("progress", fmt.Sprintf("%d/%d", m.flow.configIndex, m.flow.configTotal)),
		progressLine(int64(m.flow.configIndex), int64(m.flow.configTotal)),
	}

	return strings.Join(m.withStartupNotices(lines), "\n")
}

func (m Model) logContent() string {
	header := successStyle.Render("running") + " " + valueStyle.Render(m.flow.runningConfig) + " " + mutedStyle.Render(m.uptime())

	if len(m.flow.logs) == 0 {
		return strings.Join([]string{
			sectionTitleStyle.Render(header),
			mutedStyle.Render("waiting for winws output"),
		}, "\n")
	}
	if m.ui.rawLogMode {
		return strings.Join([]string{
			sectionTitleStyle.Render(header + " " + mutedStyle.Render("raw")),
			logStyle.Render(strings.Join(tail(m.flow.logs, 200), "\n")),
		}, "\n")
	}

	return strings.Join([]string{
		sectionTitleStyle.Render(header),
		strings.Join(formatFriendlyLogs(tail(m.flow.logs, 200)), "\n"),
	}, "\n")
}

func (m Model) uptime() string {
	if m.flow.runStartedAt.IsZero() {
		return ""
	}

	d := time.Since(m.flow.runStartedAt)

	return fmt.Sprintf("· %02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func (m Model) noLuckContent() string {
	if m.flow.err != nil {
		return strings.Join([]string{
			sectionTitleStyle.Render(errorStyle.Render("failed")),
			errorStyle.Render(m.flow.err.Error()),
		}, "\n")
	}

	return strings.Join([]string{
		sectionTitleStyle.Render(warnStyle.Render("no working config")),
		mutedStyle.Render("every strategy got filtered too — try again later, or feed sieve more configs"),
	}, "\n")
}

func (m Model) closingContent() string {
	return strings.Join([]string{
		sectionTitleStyle.Render(m.ui.spinner.View() + " Cleaning up"),
		keyValue("reason", fallback(m.ui.exitReason, "shutdown requested")),
		cleanLog("winws", "stopping process"),
		cleanLog("filters", "removing WinDivert services"),
		cleanLog("exit", "closing session"),
	}, "\n")
}

func (m Model) footer() string {
	if m.ui.state == StateClosing {
		return mutedStyle.Render("cleanup in progress — please wait")
	}

	logMode := "raw"
	if m.ui.rawLogMode {
		logMode = "clean"
	}

	sep := mutedStyle.Render(" · ")

	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		hint("ctrl+c", "quit"),
		sep,
		hint("ctrl+o", logMode),
	)
}

func (m Model) stateBadge() string {
	switch m.ui.state {
	case StateUpdating:
		return badgeStyle.Copy().Foreground(colorRust).Render("updating")
	case StateTesting:
		return badgeStyle.Copy().Foreground(colorWarn).Render("testing")
	case StateRunning:
		return badgeStyle.Copy().Foreground(colorSuccess).Render("running")
	case StateNoLuck:
		return badgeStyle.Copy().Foreground(colorError).Render("stopped")
	case StateClosing:
		return badgeStyle.Copy().Foreground(colorCleanup).Render("cleanup")
	default:
		return badgeStyle.Render("idle")
	}
}

func keyValue(key, value string) string {
	return labelStyle.Render(key) + " " + valueStyle.Render(value)
}

func (m Model) withStartupNotices(lines []string) []string {
	for _, notice := range m.ui.startupNotices {
		lines = append(lines, keyValue("notice", notice))
	}

	return lines
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

// eighthBlocks holds the partial-fill glyphs for 1/8 to 7/8 of a cell;
// index 0 is unused (no partial fill).
var eighthBlocks = []rune{0, '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

func progressLine(current, total int64) string {
	if total <= 0 {
		return labelStyle.Render("progress") + " " + mutedStyle.Render("waiting")
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}

	const width = 24
	eighths := int(current * int64(width*8) / total)
	full, partial := eighths/8, eighths%8

	filled := strings.Repeat("█", full)
	if partial > 0 && full < width {
		filled += string(eighthBlocks[partial])
		full++
	}

	bar := progressComet(filled) + progressEmptyStyle.Render(strings.Repeat("░", width-full))

	return labelStyle.Render("progress") + " " + bar + mutedStyle.Render(fmt.Sprintf(" %3d%%", current*100/total))
}

// progressComet renders the filled portion of the bar with a brighter
// leading edge, like a comet head, to read as motion rather than a static fill.
func progressComet(filled string) string {
	if filled == "" {
		return ""
	}

	runes := []rune(filled)
	body, head := string(runes[:len(runes)-1]), string(runes[len(runes)-1])

	return progressFilledStyle.Render(body) + progressCometStyle.Render(head)
}

// Palette mirrors the sieve.dev site: warm dark grays with a rust accent.
var (
	colorFg      = lipgloss.Color("#D8D4C8")
	colorFgDim   = lipgloss.Color("#8A8478")
	colorFgFaint = lipgloss.Color("#4A4843")
	colorWire    = lipgloss.Color("#26261F")
	colorRust    = lipgloss.Color("#8C6B52")
	colorRustHi  = lipgloss.Color("#B08458")
	colorSuccess = lipgloss.Color("#8FA878")
	colorWarn    = lipgloss.Color("#C2A668")
	colorError   = lipgloss.Color("#B5533C")
	colorCleanup = lipgloss.Color("#9C7B8C")
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg)
	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFgDim)
	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorRust)
	progressFilledStyle = lipgloss.NewStyle().
				Foreground(colorRust)
	progressCometStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorRustHi)
	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(colorWire)
	dotStyle = lipgloss.NewStyle().
			Foreground(colorRust)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWire).
			Padding(1, 2).
			MarginTop(1).
			MarginBottom(1)
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorFg).
				MarginBottom(1)
	labelStyle = lipgloss.NewStyle().
			Foreground(colorFgDim).
			Width(10)
	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg)
	mutedStyle = lipgloss.NewStyle().
			Foreground(colorFgFaint)
	hintStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorRustHi).
			Background(colorWire).
			Padding(0, 1)
	hintTextStyle = lipgloss.NewStyle().
			Foreground(colorFgFaint)
	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSuccess)
	warnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWarn)
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorError)
	logStyle = lipgloss.NewStyle().
			Foreground(colorFgDim)
	logKindStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorRust)
	logMessageStyle = lipgloss.NewStyle().
			Foreground(colorFg)
	versionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFgFaint)
)
