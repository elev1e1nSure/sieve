package ui

import (
	"context"
	"errors"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elev1e1nSure/sieve/internal/assets"
	"github.com/elev1e1nSure/sieve/internal/cache"
	"github.com/elev1e1nSure/sieve/internal/configs"
	"github.com/elev1e1nSure/sieve/internal/runner"
	"github.com/elev1e1nSure/sieve/internal/settings"
	"github.com/elev1e1nSure/sieve/internal/tester"
	"github.com/elev1e1nSure/sieve/internal/tray"
)

const winwsReadinessTimeout = 1500 * time.Millisecond
const maxLogLines = 2000

type State int

const (
	StateUpdating State = iota
	StateTesting
	StateRunning
	StateNoLuck
	StateClosing
	StateBye
)

type App struct {
	Assets         assets.AssetManager
	Cache          cache.CacheStore
	Configs        []configs.Config
	Runner         runner.ProcessRunner
	Tester         tester.ConnectivityTester
	StartupNotices []string
	Settings       settings.RuntimeOptions
	// Tray is optional. When non-nil and tray.IsAvailable() was true,
	// pressing T while running minimises to the system tray.
	Tray *tray.Manager
}

type Model struct {
	app    App
	ui     uiState
	flow   flowState
	ctx    context.Context
	cancel context.CancelFunc
}

type uiState struct {
	state          State
	spinner        spinner.Model
	viewport       viewport.Model
	rawLogMode     bool
	startupNotices []string
	exitReason     string
	exitErr        error
}

type flowState struct {
	assets        assets.Info
	progress      assets.Progress
	err           error
	progressC     chan assetUpdateMsg
	flowC         chan flowUpdateMsg
	currentConfig string
	configIndex   int
	configTotal   int
	runningConfig string
	runStartedAt  time.Time
	process       *runner.Process
	logs          []string
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

type cleanupDoneMsg struct {
	err error
}

type StopRequestedMsg struct{}

// TrayRestoreMsg is sent by the tray callback when the user clicks
// "Открыть" in the notification-area context menu.
type TrayRestoreMsg struct{}

func NewModel(app App) Model {
	vp := viewport.New(80, 12)
	vp.SetContent("warming up the sieve.")
	spin := spinner.New(
		spinner.WithSpinner(spinner.Points),
		spinner.WithStyle(spinnerStyle),
	)
	ctx, cancel := context.WithCancel(context.Background())

	return Model{
		app:    app,
		ctx:    ctx,
		cancel: cancel,
		ui: uiState{
			state:          StateUpdating,
			spinner:        spin,
			viewport:       vp,
			startupNotices: append([]string(nil), app.StartupNotices...),
		},
		flow: flowState{
			progressC:   make(chan assetUpdateMsg),
			configTotal: len(app.Configs),
		},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.ensureAssets(), waitForAssetUpdate(m.flow.progressC), m.ui.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			var exitErr error
			if m.ui.state == StateNoLuck {
				exitErr = m.flow.err
			}
			return m.beginShutdown("stopped by user", exitErr)
		case "ctrl+o":
			m.ui.rawLogMode = !m.ui.rawLogMode
			m.refreshBody()
			return m, nil
		case "t":
			// Minimise to tray only when winws is actively running and
			// a tray manager is available (own console, not PowerShell).
			if m.ui.state == StateRunning && m.app.Tray != nil {
				m.app.Tray.Show()
				return m, nil
			}
		}
	case tea.WindowSizeMsg:
		m.ui.viewport.Width = max(1, msg.Width-4)
		m.ui.viewport.Height = max(1, msg.Height-7)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.ui.spinner, cmd = m.ui.spinner.Update(msg)
		m.refreshBody()
		return m, cmd
	case assetUpdateMsg:
		var cmd tea.Cmd
		m, cmd = m.handleAssetUpdate(msg)
		if m.ui.state == StateNoLuck && m.flow.err != nil {
			return m.beginShutdown("startup failed", m.flow.err)
		}
		return m, cmd
	case flowUpdateMsg:
		m = m.handleFlowUpdate(msg)
		if msg.kind == flowNoLuck && msg.err != nil {
			return m.beginShutdown("winws stopped with an error", msg.err)
		}
		if msg.done {
			return m, nil
		}
		return m, waitForFlowUpdate(m.flow.flowC)
	case StopRequestedMsg:
		return m.beginShutdown("stopped by --stop", nil)
	case TrayRestoreMsg:
		// Console window has already been shown by the tray callback;
		// ask BubbleTea to repaint the whole screen.
		return m, tea.ClearScreen
	case cleanupDoneMsg:
		m.ui.exitErr = errors.Join(m.ui.exitErr, msg.err)
		m.ui.state = StateBye
		return m, tea.Quit
	}

	m.refreshBody()

	var cmd tea.Cmd
	m.ui.viewport, cmd = m.ui.viewport.Update(msg)
	return m, cmd
}

func (m Model) ShutdownError() error {
	return m.ui.exitErr
}

func (m Model) beginShutdown(reason string, err error) (Model, tea.Cmd) {
	if m.ui.state == StateClosing || m.ui.state == StateBye {
		return m, nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.ui.exitReason = reason
	m.ui.exitErr = errors.Join(m.ui.exitErr, err)
	m.ui.state = StateClosing
	m.refreshBody()
	return m, m.stopRunning()
}

func (m *Model) refreshBody() {
	m.ui.viewport.SetContent(m.body())
	if m.ui.state == StateRunning {
		m.ui.viewport.GotoBottom()
	}
}
