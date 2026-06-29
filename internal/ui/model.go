package ui

import (
	"context"
	"fmt"
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
	viewport  viewport.Model
	assets    assets.Info
	progress  assets.Progress
	err       error
	ready     bool
	progressC chan assetUpdateMsg
}

func NewModel(app App) Model {
	vp := viewport.New(80, 8)
	vp.SetContent("Preparing assets.")

	return Model{
		app:       app,
		viewport:  vp,
		progressC: make(chan assetUpdateMsg),
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
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = max(1, msg.Height-3)
	case assetUpdateMsg:
		m.progress = msg.progress
		m.assets = msg.info
		m.err = msg.err
		m.ready = msg.done && msg.err == nil
		m.viewport.SetContent(m.assetContent())
		if msg.done {
			return m, nil
		}
		return m, waitForAssetUpdate(m.progressC)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := lipgloss.NewStyle().Bold(true).Render("sieve")
	status := fmt.Sprintf("test timeout: %s | configs loaded: %d | q to quit", m.app.Options.TestTimeout, len(m.app.Configs))

	return header + "\n" + m.viewport.View() + "\n" + status
}

type assetUpdateMsg struct {
	progress assets.Progress
	info     assets.Info
	err      error
	done     bool
}

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

func (m Model) assetContent() string {
	if m.err != nil {
		return "Asset update failed:\n" + m.err.Error()
	}

	if m.ready {
		action := "Assets already current."
		if m.assets.Updated {
			action = "Assets updated."
		}

		return fmt.Sprintf("%s\nversion: %s\ninstall dir: %s", action, m.assets.Version, m.assets.InstallDir)
	}

	if m.progress.Total > 0 {
		return fmt.Sprintf("%s\n%s\n%d / %d bytes", m.progress.Phase, m.progress.Message, m.progress.Current, m.progress.Total)
	}

	return fmt.Sprintf("%s\n%s", m.progress.Phase, m.progress.Message)
}
