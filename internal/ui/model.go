package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/your-name/sieve/internal/admin"
	"github.com/your-name/sieve/internal/assets"
	"github.com/your-name/sieve/internal/cache"
	"github.com/your-name/sieve/internal/configs"
	"github.com/your-name/sieve/internal/runner"
	"github.com/your-name/sieve/internal/tester"
)

const winwsWarmup = 1500 * time.Millisecond

type State int

const (
	StateUpdating State = iota
	StateTesting
	StateRunning
	StateNoLuck
)

type Options struct {
	TestTimeout time.Duration
}

type App struct {
	Admin   admin.Service
	Assets  assets.Manager
	Cache   cache.Store
	Configs []configs.Config
	Runner  runner.Runner
	Tester  tester.Tester
	Options Options
}

type Model struct {
	app       App
	state     State
	spinner   spinner.Model
	viewport  viewport.Model
	assets    assets.Info
	progress  assets.Progress
	err       error
	progressC chan assetUpdateMsg
	flowC     chan flowUpdateMsg
	ctx       context.Context
	cancel    context.CancelFunc

	currentConfig string
	configIndex   int
	configTotal   int
	runningConfig string
	process       *runner.Process
	logs          []string
}

func NewModel(app App) Model {
	vp := viewport.New(80, 12)
	vp.SetContent("Starting.")
	spin := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(spinnerStyle),
	)
	ctx, cancel := context.WithCancel(context.Background())

	return Model{
		app:         app,
		state:       StateUpdating,
		spinner:     spin,
		viewport:    vp,
		progressC:   make(chan assetUpdateMsg),
		ctx:         ctx,
		cancel:      cancel,
		configTotal: len(app.Configs),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.ensureAssets(), waitForAssetUpdate(m.progressC), m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Sequence(m.stopRunning(), tea.Quit)
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = max(1, msg.Width-4)
		m.viewport.Height = max(1, msg.Height-7)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.viewport.SetContent(m.body())
		if m.state == StateRunning {
			m.viewport.GotoBottom()
		}
		return m, cmd
	case assetUpdateMsg:
		var cmd tea.Cmd
		m, cmd = m.handleAssetUpdate(msg)
		return m, cmd
	case flowUpdateMsg:
		m = m.handleFlowUpdate(msg)
		if msg.done {
			return m, nil
		}
		return m, waitForFlowUpdate(m.flowC)
	}

	m.viewport.SetContent(m.body())

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("sieve"), " ", m.stateBadge())
	footer := m.footer()
	panel := panelStyle.Width(m.viewport.Width + 2).Render(m.viewport.View())

	return header + "\n" + panel + "\n" + footer
}

type assetUpdateMsg struct {
	progress assets.Progress
	info     assets.Info
	err      error
	done     bool
}

type flowKind int

const (
	flowTesting flowKind = iota
	flowRunning
	flowNoLuck
	flowLog
	flowDone
)

type flowUpdateMsg struct {
	kind          flowKind
	currentConfig string
	index         int
	total         int
	process       *runner.Process
	log           string
	err           error
	done          bool
}

func (m Model) ensureAssets() tea.Cmd {
	return func() tea.Msg {
		go func() {
			info, err := m.app.Assets.Ensure(m.ctx, func(progress assets.Progress) {
				m.progressC <- assetUpdateMsg{progress: progress}
			})
			m.progressC <- assetUpdateMsg{info: info, err: err, done: true}
			close(m.progressC)
		}()

		return nil
	}
}

func waitForAssetUpdate(updates <-chan assetUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-updates
		if !ok {
			return assetUpdateMsg{done: true}
		}

		return msg
	}
}

func waitForFlowUpdate(updates <-chan flowUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-updates
		if !ok {
			return flowUpdateMsg{kind: flowDone, done: true}
		}

		return msg
	}
}

func (m Model) handleAssetUpdate(msg assetUpdateMsg) (Model, tea.Cmd) {
	m.progress = msg.progress
	m.assets = msg.info
	m.err = msg.err

	if !msg.done {
		m.viewport.SetContent(m.body())
		return m, waitForAssetUpdate(m.progressC)
	}
	if msg.err != nil {
		m.state = StateNoLuck
		m.viewport.SetContent(m.body())
		return m, nil
	}

	m.state = StateTesting
	m.currentConfig = "starting"
	m.flowC = make(chan flowUpdateMsg)
	m.viewport.SetContent(m.body())

	return m, tea.Batch(m.runFlow(m.flowC), waitForFlowUpdate(m.flowC))
}

func (m Model) handleFlowUpdate(msg flowUpdateMsg) Model {
	switch msg.kind {
	case flowTesting:
		m.state = StateTesting
		m.currentConfig = msg.currentConfig
		m.configIndex = msg.index
		m.configTotal = msg.total
		m.process = msg.process
	case flowRunning:
		m.state = StateRunning
		m.runningConfig = msg.currentConfig
		m.process = msg.process
		m.logs = nil
	case flowNoLuck:
		m.state = StateNoLuck
		m.err = msg.err
		m.process = nil
	case flowLog:
		m.logs = append(m.logs, msg.log)
	case flowDone:
		m.process = nil
	}

	m.viewport.SetContent(m.body())
	if m.state == StateRunning {
		m.viewport.GotoBottom()
	}
	return m
}

func (m Model) runFlow(updates chan<- flowUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(updates)

			store := m.app.Cache
			if err := store.Load(); err != nil {
				updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
				return
			}
			if err := m.app.Runner.KillExisting(); err != nil {
				updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
				return
			}

			sorted := store.SortedConfigs(m.app.Configs)
			total := len(sorted)
			winwsPath := filepath.Join(m.assets.BinDir, "winws.exe")

			for i, config := range sorted {
				select {
				case <-m.ctx.Done():
					updates <- flowUpdateMsg{kind: flowDone, done: true}
					return
				default:
				}

				process, err := m.app.Runner.Start(winwsPath, config.Resolve(m.assets.BinDir, m.assets.ListsDir))
				updates <- flowUpdateMsg{
					kind:          flowTesting,
					currentConfig: config.Name,
					index:         i + 1,
					total:         total,
					process:       process,
				}
				if err != nil {
					if cacheErr := store.RecordResult(config.Name, false, time.Now()); cacheErr != nil {
						updates <- flowUpdateMsg{kind: flowNoLuck, err: cacheErr, done: true}
						return
					}
					continue
				}

				if !sleepContext(m.ctx, winwsWarmup) {
					_ = process.Stop()
					updates <- flowUpdateMsg{kind: flowDone, done: true}
					return
				}

				result := m.app.Tester.Test(m.ctx)
				ok := result.Discord && result.YouTube && result.Err == nil
				if err := store.RecordResult(config.Name, ok, time.Now()); err != nil {
					_ = process.Stop()
					updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
					return
				}
				if ok {
					updates <- flowUpdateMsg{kind: flowRunning, currentConfig: config.Name, process: process}
					for line := range process.Logs() {
						updates <- flowUpdateMsg{kind: flowLog, log: line}
					}
					return
				}

				_ = process.Stop()
			}

			updates <- flowUpdateMsg{kind: flowNoLuck, done: true}
		}()

		return nil
	}
}

func (m Model) stopRunning() tea.Cmd {
	return func() tea.Msg {
		if m.process != nil {
			_ = m.process.Stop()
		} else {
			m.app.Runner.Cleanup()
		}

		return nil
	}
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

	return strings.Join([]string{
		sectionTitleStyle.Render(successStyle.Render("running") + " " + valueStyle.Render(m.runningConfig)),
		logStyle.Render(strings.Join(tail(m.logs, 200), "\n")),
	}, "\n")
}

func (m Model) noLuckContent() string {
	if m.err != nil {
		return strings.Join([]string{
			sectionTitleStyle.Render(errorStyle.Render("failed")),
			errorStyle.Render(m.err.Error()),
		}, "\n")
	}

	return strings.Join([]string{
		sectionTitleStyle.Render(warnStyle.Render("no working config")),
	}, "\n")
}

func (m Model) statusLine() string {
	return fmt.Sprintf("timeout %s   configs %d", m.app.Options.TestTimeout, len(m.app.Configs))
}

func (m Model) footer() string {
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		subtleStyle.Render(m.statusLine()),
		"  ",
		hint("q", "quit"),
		" ",
		hint("ctrl+c", "cleanup"),
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
	default:
		return badgeStyle.Render("idle")
	}
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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

	const width = 28
	filled := int(current * width / total)
	bar := strings.Repeat("=", filled) + strings.Repeat("-", width-filled)

	return labelStyle.Render("progress") + " " + spinnerStyle.Render(bar) + mutedStyle.Render(fmt.Sprintf(" %d%%", current*100/total))
}

func tail(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}

	return lines[len(lines)-limit:]
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
	subtleStyle = lipgloss.NewStyle().
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
)
