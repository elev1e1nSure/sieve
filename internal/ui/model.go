package ui

import (
	"context"
	"fmt"
	"path/filepath"
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
	ctx, cancel := context.WithCancel(context.Background())

	return Model{
		app:         app,
		state:       StateUpdating,
		viewport:    vp,
		progressC:   make(chan assetUpdateMsg),
		ctx:         ctx,
		cancel:      cancel,
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
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Sequence(m.stopRunning(), tea.Quit)
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = max(1, msg.Height-4)
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
		return "Run failed:\n" + m.err.Error()
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

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)
