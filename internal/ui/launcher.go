package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elev1e1nSure/sieve/internal/maintenance"
	"github.com/elev1e1nSure/sieve/internal/settings"
	"github.com/elev1e1nSure/sieve/internal/version"
)

type LauncherChoice int

const (
	LauncherQuit LauncherChoice = iota
	LauncherRun
)

type launcherPage int

const (
	launcherMenu launcherPage = iota
	launcherSettings
	launcherEdit
	launcherConfirm
	launcherWorking
	launcherResult
)

type maintenanceAction int

const (
	actionUpdate maintenanceAction = iota
	actionStop
	actionResetCache
	actionUpdateIPSet
	actionDiagnostics
	actionDiagnosticsFix
	actionClearDiscordCache
)

const (
	rowTimeout = iota
	rowCache
	rowPath
	rowIPSet
	rowDomains
	rowDomainFiles
	rowGame
	rowUpdate
	rowStop
	rowResetCache
	rowUpdateIPSet
	rowDiagnostics
	rowDiagnosticsFix
	rowClearDiscordCache
	settingsRowCount
)

type LauncherModel struct {
	ctx         context.Context
	store       settings.Store
	maintenance maintenance.Service
	page        launcherPage
	choice      LauncherChoice
	menuCursor  int
	rowCursor   int
	width       int
	height      int
	saved       settings.RuntimeOptions
	draft       settings.RuntimeOptions
	input       textinput.Model
	editRow     int
	action      maintenanceAction
	spinner     spinner.Model
	report      maintenance.Report
	err         error
}

type settingsSavedMsg struct {
	opts settings.RuntimeOptions
	err  error
}

type maintenanceDoneMsg struct {
	report maintenance.Report
	err    error
}

func NewLauncher(ctx context.Context, store settings.Store, runtime settings.RuntimeOptions, service maintenance.Service) LauncherModel {
	input := textinput.New()
	input.CharLimit = 4096
	input.Width = 60
	spin := spinner.New(spinner.WithSpinner(spinner.Points), spinner.WithStyle(spinnerStyle))

	return LauncherModel{
		ctx:         ctx,
		store:       store,
		maintenance: service,
		page:        launcherMenu,
		saved:       runtime.Normalized(),
		draft:       runtime.Normalized(),
		input:       input,
		spinner:     spin,
	}
}

func (m LauncherModel) Init() tea.Cmd { return nil }

func (m LauncherModel) Choice() LauncherChoice { return m.choice }

func (m LauncherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.input.Width = max(20, min(70, size.Width-12))
		return m, nil
	}

	if m.page == launcherEdit {
		return m.updateEditor(msg)
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.page != launcherWorking {
			return m, nil
		}
		return m, cmd
	case settingsSavedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.draft = msg.opts
		m.saved = msg.opts
		return m, nil
	case maintenanceDoneMsg:
		m.report, m.err = msg.report, msg.err
		if msg.err == nil && msg.report.QuitAfter {
			return m, tea.Quit
		}
		m.page = launcherResult
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	return m, nil
}

func (m LauncherModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		if m.page == launcherWorking {
			return m, nil
		}
		return m, tea.Quit
	}

	switch m.page {
	case launcherMenu:
		switch key {
		case "up", "down":
			m.menuCursor = (m.menuCursor + 1) % 2
		case "enter":
			if m.menuCursor == 0 {
				m.choice = LauncherRun
				return m, tea.Quit
			}
			m.draft = m.saved
			m.err = nil
			m.page = launcherSettings
		case "esc":
			return m, tea.Quit
		}
	case launcherSettings:
		switch key {
		case "up":
			m.rowCursor = (m.rowCursor - 1 + settingsRowCount) % settingsRowCount
		case "down":
			m.rowCursor = (m.rowCursor + 1) % settingsRowCount
		case "enter":
			return m.activateRow()
		case "esc":
			m.page = launcherMenu
			m.menuCursor = 1
		}
	case launcherConfirm:
		switch key {
		case "enter":
			return m.startAction()
		case "esc":
			m.page = launcherSettings
		}
	case launcherResult:
		if key == "enter" || key == "esc" {
			m.page = launcherSettings
			m.err = nil
			m.report = maintenance.Report{}
		}
	}

	return m, nil
}

func (m LauncherModel) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if err := m.commitInput(); err != nil {
				m.err = err
				return m, nil
			}
			m.err = nil
			m.page = launcherSettings
			m.input.Blur()
			return m, m.persistDraft()
		case "esc":
			m.err = nil
			m.page = launcherSettings
			m.input.Blur()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *LauncherModel) activateRow() (tea.Model, tea.Cmd) {
	switch m.rowCursor {
	case rowTimeout, rowDomains, rowDomainFiles:
		m.openEditor(m.rowCursor)
		return *m, textinput.Blink
	case rowCache, rowPath, rowIPSet, rowGame:
		m.changeSetting(1)
		return *m, m.persistDraft()
	default:
		m.action = actionForRow(m.rowCursor)
		if m.action == actionDiagnostics {
			return m.startAction()
		}
		m.page = launcherConfirm
		return *m, nil
	}
}

func (m *LauncherModel) openEditor(row int) {
	m.editRow = row
	m.input.Prompt = ""
	switch row {
	case rowTimeout:
		m.input.Placeholder = "seconds"
		m.input.SetValue(strconv.Itoa(m.draft.TestTimeout))
	case rowDomains:
		m.input.Placeholder = "example.com, media.example.com"
		m.input.SetValue(strings.Join(m.draft.Domains, ", "))
	case rowDomainFiles:
		m.input.Placeholder = `C:\path\domains.txt; C:\path\more.txt`
		m.input.SetValue(strings.Join(m.draft.DomainFiles, "; "))
	}
	m.input.CursorEnd()
	m.input.Focus()
	m.page = launcherEdit
}

func (m *LauncherModel) commitInput() error {
	value := strings.TrimSpace(m.input.Value())
	switch m.editRow {
	case rowTimeout:
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds <= 0 {
			return fmt.Errorf("timeout must be a positive integer")
		}
		m.draft.TestTimeout = seconds
	case rowDomains:
		m.draft.Domains = splitValues(value, ",;")
	case rowDomainFiles:
		m.draft.DomainFiles = splitValues(value, ";\n")
	}
	return nil
}

func (m *LauncherModel) changeSetting(direction int) {
	switch m.rowCursor {
	case rowCache:
		m.draft.NoCache = !m.draft.NoCache
	case rowPath:
		m.draft.NoAddPath = !m.draft.NoAddPath
	case rowIPSet:
		values := []string{settings.IPSetUnchanged, settings.IPSetLoaded, settings.IPSetNone, settings.IPSetAny}
		m.draft.IPSetMode = cycle(values, m.draft.IPSetMode, direction)
	case rowGame:
		values := []string{settings.GameOff, settings.GameAll, settings.GameTCP, settings.GameUDP}
		m.draft.GameMode = cycle(values, m.draft.GameMode, direction)
	}
}

func (m LauncherModel) saveSettings(draft settings.RuntimeOptions) tea.Cmd {
	return func() tea.Msg {
		return settingsSavedMsg{opts: draft, err: m.store.Save(draft)}
	}
}

func (m LauncherModel) persistDraft() tea.Cmd {
	draft := m.draft.Normalized()
	if err := draft.Validate(); err != nil {
		return func() tea.Msg { return settingsSavedMsg{err: err} }
	}
	return m.saveSettings(draft)
}

func (m *LauncherModel) startAction() (tea.Model, tea.Cmd) {
	action := m.action
	m.page = launcherWorking
	m.err = nil
	return *m, tea.Batch(func() tea.Msg {
		var report maintenance.Report
		var err error
		switch action {
		case actionUpdate:
			report, err = m.maintenance.Update(m.ctx)
		case actionStop:
			report, err = m.maintenance.Stop()
		case actionResetCache:
			report, err = m.maintenance.ResetCache()
		case actionUpdateIPSet:
			report, err = m.maintenance.UpdateIPSet(m.ctx)
		case actionDiagnostics:
			report = m.maintenance.Diagnostics(false)
		case actionDiagnosticsFix:
			report = m.maintenance.Diagnostics(true)
		case actionClearDiscordCache:
			report = m.maintenance.ClearDiscordCache()
		}
		return maintenanceDoneMsg{report: report, err: err}
	}, m.spinner.Tick)
}

func (m LauncherModel) View() string {
	header := lipgloss.JoinHorizontal(lipgloss.Center,
		dotStyle.Render("●")+" "+titleStyle.Render("sieve"), " ", versionStyle.Render(version.Version))
	body := ""
	switch m.page {
	case launcherMenu:
		body = m.menuView()
	case launcherSettings:
		body = m.settingsView()
	case launcherEdit:
		body = m.editorView()
	case launcherConfirm:
		body = m.confirmView()
	case launcherWorking:
		body = sectionTitleStyle.Render(m.spinner.View()+" Working") + "\n" + mutedStyle.Render("please wait")
	case launcherResult:
		body = m.resultView()
	}

	panelWidth := max(50, min(88, m.width-4))
	return header + "\n" + panelStyle.Width(panelWidth).Render(body) + "\n" + m.launcherFooter()
}

func (m LauncherModel) menuView() string {
	rows := []string{"Start sifting", "Settings"}
	lines := []string{sectionTitleStyle.Render("Choose an action")}
	for i, row := range rows {
		lines = append(lines, selectableRow(i == m.menuCursor, row, ""))
	}
	return strings.Join(lines, "\n")
}

func (m LauncherModel) settingsView() string {
	rows := []struct{ label, value string }{
		{"Test timeout", fmt.Sprintf("%d seconds", m.draft.TestTimeout)},
		{"Config cache", enabled(!m.draft.NoCache)},
		{"Add to PATH", enabled(!m.draft.NoAddPath)},
		{"IPSet mode", fallback(m.draft.IPSetMode, "unchanged")},
		{"Domains", listSummary(m.draft.Domains)},
		{"Domain files", listSummary(m.draft.DomainFiles)},
		{"Game mode", fallback(m.draft.GameMode, settings.GameOff)},
		{"Update sieve", ""},
		{"Stop active sieve", ""},
		{"Reset config cache", ""},
		{"Update IPSet", ""},
		{"Run diagnostics", ""},
		{"Run diagnostics and fix", ""},
		{"Clear Discord cache", ""},
	}

	lines := []string{sectionTitleStyle.Render("Settings")}
	start, end := visibleRange(m.rowCursor, len(rows), max(6, m.height-10))
	for i := start; i < end; i++ {
		if i == rowUpdate {
			lines = append(lines, "")
		}
		lines = append(lines, selectableRow(i == m.rowCursor, rows[i].label, rows[i].value))
	}
	if m.err != nil {
		lines = append(lines, "", errorStyle.Render(m.err.Error()))
	}
	return strings.Join(lines, "\n")
}

func (m LauncherModel) editorView() string {
	label := map[int]string{rowTimeout: "Test timeout", rowDomains: "Domains", rowDomainFiles: "Domain files"}[m.editRow]
	lines := []string{sectionTitleStyle.Render(label), m.input.View()}
	if m.err != nil {
		lines = append(lines, errorStyle.Render(m.err.Error()))
	}
	return strings.Join(lines, "\n")
}

func (m LauncherModel) confirmView() string {
	return strings.Join([]string{
		sectionTitleStyle.Render("Confirm action"),
		valueStyle.Render(actionLabel(m.action)),
		mutedStyle.Render("This action may change system state."),
	}, "\n")
}

func (m LauncherModel) resultView() string {
	title := fallback(m.report.Title, "Operation failed")
	lines := []string{sectionTitleStyle.Render(title)}
	if m.err != nil {
		return strings.Join(append(lines, errorStyle.Render(m.err.Error())), "\n")
	}
	for _, item := range m.report.Items {
		style, glyph := valueStyle, "·"
		switch item.Status {
		case "ok":
			style, glyph = successStyle, "✓"
		case "fixed":
			style, glyph = successStyle, "↻"
		case "warn":
			style, glyph = warnStyle, "!"
		case "fail":
			style, glyph = errorStyle, "✗"
		}
		lines = append(lines, style.Render(glyph)+" "+valueStyle.Render(item.Name)+" "+mutedStyle.Render(item.Message))
	}
	return strings.Join(lines, "\n")
}

func (m LauncherModel) launcherFooter() string {
	sep := mutedStyle.Render(" · ")
	switch m.page {
	case launcherEdit:
		return hint("enter", "apply") + sep + hint("esc", "cancel")
	case launcherConfirm:
		return hint("enter", "confirm") + sep + hint("esc", "cancel")
	case launcherWorking:
		return mutedStyle.Render("operation in progress")
	case launcherResult:
		return hint("enter", "back")
	default:
		return hint("↑/↓", "select") + sep + hint("enter", "open") + sep + hint("esc", "back/quit")
	}
}

func selectableRow(selected bool, label, value string) string {
	prefix := "  "
	labelStyle := valueStyle
	if selected {
		prefix = dotStyle.Render("› ")
		labelStyle = titleStyle.Copy().Foreground(colorRustHi)
	}
	line := prefix + labelStyle.Render(label)
	if value != "" {
		line += "  " + mutedStyle.Render(value)
	}
	return line
}

func actionForRow(row int) maintenanceAction {
	return maintenanceAction(row - rowUpdate)
}

func actionLabel(action maintenanceAction) string {
	labels := []string{"Update sieve", "Stop active sieve", "Reset config cache", "Update IPSet", "Run diagnostics", "Run diagnostics and fix", "Clear Discord cache"}
	return labels[int(action)]
}

func cycle(values []string, current string, direction int) string {
	index := 0
	for i, value := range values {
		if value == current {
			index = i
			break
		}
	}
	return values[(index+direction+len(values))%len(values)]
}

func splitValues(value, separators string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(separators, r) }) {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func enabled(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func listSummary(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	joined := strings.Join(values, ", ")
	if len([]rune(joined)) > 48 {
		return string([]rune(joined)[:45]) + "..."
	}
	return joined
}

func visibleRange(cursor, total, capacity int) (int, int) {
	capacity = min(total, capacity)
	start := max(0, cursor-capacity/2)
	if start+capacity > total {
		start = total - capacity
	}
	return start, start + capacity
}
