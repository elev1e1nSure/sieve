package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/elev1e1nSure/sieve/internal/maintenance"
	"github.com/elev1e1nSure/sieve/internal/settings"
)

// applyStyledTemplates wires sieve's palette into cobra's --help/--version
// output, which otherwise renders as plain, uncolored text.
func applyStyledTemplates(root *cobra.Command) {
	cobra.AddTemplateFunc("heading", func(s string) string {
		return titleStyle.Render(s)
	})
	cobra.AddTemplateFunc("dim", func(s string) string {
		return mutedStyle.Render(s)
	})

	root.SetUsageTemplate(`{{heading "Usage:"}}
  {{.UseLine}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`)

	root.SetHelpTemplate(`{{.Long}}

{{.UsageString}}`)

	root.SetVersionTemplate(titleStyle.Render(root.Use) + " " + mutedStyle.Render(root.Version) + "\n")
}

func printSavedRuntime(opts settings.RuntimeOptions) {
	rows := [][2]string{
		{"test-timeout", fmt.Sprintf("%ds", opts.TestTimeout)},
		{"cache", boolStatus(!opts.NoCache)},
		{"ipset", fallback(opts.IPSetMode, "unchanged")},
		{"game", fallback(opts.GameMode, settings.GameOff)},
	}
	if len(opts.Domains) > 0 {
		rows = append(rows, [2]string{"domains", strings.Join(opts.Domains, ", ")})
	}
	if len(opts.DomainFiles) > 0 {
		rows = append(rows, [2]string{"domain files", strings.Join(opts.DomainFiles, ", ")})
	}
	printRows(rows)
}

func printMaintenanceReport(report maintenance.Report) {
	fmt.Println(titleStyle.Render(report.Title))
	width := 0
	for _, item := range report.Items {
		width = max(width, len(item.Name))
	}
	for _, item := range report.Items {
		glyph, style := diagnosticGlyph(item.Status)
		name := nameStyle.Render(fmt.Sprintf("%-*s", width, item.Name))
		fmt.Println(style.Render(glyph) + "  " + name + "  " + mutedStyle.Render(item.Message))
	}
}

// diagnosticGlyph mirrors the dot/badge language internal/ui already uses
// for run status, so flag-mode output and the TUI read as the same voice.
func diagnosticGlyph(status string) (string, lipgloss.Style) {
	switch status {
	case "fail":
		return "✗", failStyle
	case "warn":
		return "!", warnStyle
	case "fixed":
		return "↻", fixedStyle
	default:
		return "✓", successStyle
	}
}

// printRows renders aligned key/value pairs with a rust dot marker,
// matching the "· mm:ss" style internal/ui uses elsewhere.
func printRows(rows [][2]string) {
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		key := nameStyle.Render(fmt.Sprintf("%-*s", width, row[0]))
		fmt.Println(dotStyle.Render("·") + "  " + key + "  " + row[1])
	}
}

func ok(message string) string {
	return successStyle.Render("✓") + " " + message
}

func boolStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}

	return value
}

// Palette mirrors internal/ui's view.go, which mirrors the sieve.dev site:
// warm dark grays with a rust accent. Kept duplicated rather than exported
// from internal/ui to avoid coupling the flag-mode CLI to the TUI package.
var (
	colorFg      = lipgloss.Color("#D8D4C8")
	colorFgFaint = lipgloss.Color("#4A4843")
	colorRustHi  = lipgloss.Color("#B08458")
	colorSuccess = lipgloss.Color("#8FA878")
	colorWarn    = lipgloss.Color("#C2A668")
	colorError   = lipgloss.Color("#B5533C")
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorRustHi)
	nameStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorFg)
	dotStyle     = lipgloss.NewStyle().Foreground(colorRustHi)
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	fixedStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorRustHi)
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorWarn)
	failStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorError)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorFgFaint)
)
