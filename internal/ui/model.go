package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

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
	StateClosing
)

type App struct {
	Assets  assets.Manager
	Cache   cache.Store
	Configs []configs.Config
	Runner  runner.Runner
	Tester  tester.Tester
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
	rawLogMode    bool
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

type cleanupDoneMsg struct{}

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
			m.state = StateClosing
			m.refreshBody()
			return m, m.stopRunning()
		case "ctrl+o":
			m.rawLogMode = !m.rawLogMode
			m.refreshBody()
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = max(1, msg.Width-4)
		m.viewport.Height = max(1, msg.Height-7)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.refreshBody()
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
	case cleanupDoneMsg:
		return m, tea.Quit
	}

	m.refreshBody()

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) refreshBody() {
	m.viewport.SetContent(m.body())
	if m.state == StateRunning {
		m.viewport.GotoBottom()
	}
}
