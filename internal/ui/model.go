package ui

import (
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
	app      App
	viewport viewport.Model
}

func NewModel(app App) Model {
	vp := viewport.New(80, 8)
	vp.SetContent("Phase 1 skeleton ready.\nRuntime flow starts in later phases.")

	return Model{
		app:      app,
		viewport: vp,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
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
