package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	viewport  viewport.Model
	assets    assets.Info
	progress  assets.Progress
	err       error
	progressC chan assetUpdateMsg

	currentConfig string
	configIndex   int
	configTotal   int
	runningConfig string
	process       *runner.Process
	logs          []string
}

func NewModel(app App) Model {
	vp := viewport.New(80, 12)

	return Model{
		app:         app,
		state:       StateUpdating,
		viewport:    vp,
		progressC:   make(chan assetUpdateMsg),
		configTotal: len(app.Configs),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.ensureAssets(), waitForAssetUpdate(m.progressC))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Sequence(m.stopRunning(), tea.Quit)
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = max(1, msg.Height-4)
	case assetUpdateMsg:
		m = m.handleAssetUpdate(msg)
		if msg.done {
			return m, nil
		}
		return m, waitForAssetUpdate(m.progressC)
	case logMsg:
		m.logs = append(m.logs, string(msg))
		m.viewport.SetContent(m.logContent())
		return m, nil
	}

	m.viewport.SetContent(m.body())

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := titleStyle.Render("sieve")
	status := statusStyle.Render(m.statusLine())

	return header + "\n" + m.viewport.View() + "\n" + status
}

type assetUpdateMsg struct {
	progress assets.Progress
	info     assets.Info
	err      error
	done     bool
}

type logMsg string

func (m Model) ensureAssets() tea.Cmd {
	return func() tea.Msg {
		go func() {
			info, err := m.app.Assets.Ensure(context.Background(), func(progress assets.Progress) {
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

func (m Model) handleAssetUpdate(msg assetUpdateMsg) Model {
	m.progress = msg.progress
	m.assets = msg.info
	m.err = msg.err

	if msg.done {
		if msg.err != nil {
			m.state = StateNoLuck
		} else {
			m.state = StateTesting
			m.currentConfig = "waiting for test loop"
		}
	}

	m.viewport.SetContent(m.body())
	return m
}

func (m Model) stopRunning() tea.Cmd {
	return func() tea.Msg {
		if m.process != nil {
			_ = m.process.Stop()
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
	if m.progress.Total > 0 {
		return fmt.Sprintf("%s\n%s\n%d / %d bytes", m.progress.Phase, m.progress.Message, m.progress.Current, m.progress.Total)
	}

	return strings.TrimSpace(fmt.Sprintf("%s\n%s", m.progress.Phase, m.progress.Message))
}

func (m Model) testingContent() string {
	return fmt.Sprintf("Testing configs\ncurrent: %s\nprogress: %d/%d", m.currentConfig, m.configIndex, m.configTotal)
}

func (m Model) logContent() string {
	if len(m.logs) == 0 {
		return fmt.Sprintf("Running: %s\nwaiting for winws output", m.runningConfig)
	}

	return fmt.Sprintf("Running: %s\n%s", m.runningConfig, strings.Join(m.logs, "\n"))
}

func (m Model) noLuckContent() string {
	if m.err != nil {
		return "Asset update failed:\n" + m.err.Error()
	}

	return "No working config found."
}

func (m Model) statusLine() string {
	state := "updating"
	switch m.state {
	case StateTesting:
		state = "testing"
	case StateRunning:
		state = "running"
	case StateNoLuck:
		state = "no luck"
	}

	return fmt.Sprintf("%s | timeout %s | configs %d | q quit", state, m.app.Options.TestTimeout, len(m.app.Configs))
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)
