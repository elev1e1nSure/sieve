package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elev1e1nSure/sieve/internal/assets"
	"github.com/elev1e1nSure/sieve/internal/cache"
	"github.com/elev1e1nSure/sieve/internal/configs"
	"github.com/elev1e1nSure/sieve/internal/runner"
	"github.com/elev1e1nSure/sieve/internal/settings"
	"github.com/elev1e1nSure/sieve/internal/tester"
)

type Flow struct {
	Assets   assets.Info
	Cache    cache.CacheStore
	Runner   runner.ProcessRunner
	Tester   tester.ConnectivityTester
	Configs  []configs.Config
	Settings settings.RuntimeOptions
	Ctx      context.Context
}

func (f Flow) Run(updates chan<- flowUpdateMsg) {
	defer func() {
		if r := recover(); r != nil {
			updates <- flowUpdateMsg{kind: flowNoLuck, err: fmt.Errorf("internal error: %v", r), done: true}
		}
		close(updates)
	}()

	store := f.Cache
	if err := store.Load(); err != nil {
		updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
		return
	}
	sorted := store.SortedConfigs(f.Configs)
	total := len(sorted)
	winwsPath := filepath.Join(f.Assets.BinDir, "winws.exe")
	if err := f.Runner.Prepare(winwsPath); err != nil {
		updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
		return
	}

	for i, config := range sorted {
		select {
		case <-f.Ctx.Done():
			updates <- flowUpdateMsg{kind: flowDone, done: true}
			return
		default:
		}

		process, err := f.Runner.Start(winwsPath, config.ResolveWithOptions(f.Assets.BinDir, f.Assets.ListsDir, f.Settings))
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

		if !sleepContext(f.Ctx, winwsWarmup) {
			_ = f.Runner.Stop()
			updates <- flowUpdateMsg{kind: flowDone, done: true}
			return
		}

		result := f.Tester.Test(f.Ctx)
		ok := result.Discord && result.YouTube && result.Err == nil
		if err := store.RecordResult(config.Name, ok, time.Now()); err != nil {
			_ = f.Runner.Stop()
			updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
			return
		}
		if ok {
			updates <- flowUpdateMsg{kind: flowRunning, currentConfig: config.Name, process: process}
			for {
				select {
				case line, more := <-process.Logs():
					if !more {
						err := process.Wait()
						if err == nil {
							err = errors.New("winws exited unexpectedly")
						}
						if cleanupErr := f.Runner.Stop(); cleanupErr != nil {
							err = errors.Join(err, cleanupErr)
						}
						updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
						return
					}
					updates <- flowUpdateMsg{kind: flowLog, log: line}
				case <-f.Ctx.Done():
					_ = f.Runner.Stop()
					updates <- flowUpdateMsg{kind: flowDone, done: true}
					return
				}
			}
		}

		if err := f.Runner.Stop(); err != nil {
			updates <- flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
			return
		}
	}

	updates <- flowUpdateMsg{kind: flowNoLuck, done: true}
}

func (m Model) ensureAssets() tea.Cmd {
	return func() tea.Msg {
		go func() {
			info, err := m.app.Assets.Ensure(m.ctx, func(progress assets.Progress) {
				m.flow.progressC <- assetUpdateMsg{progress: progress}
			})
			m.flow.progressC <- assetUpdateMsg{info: info, err: err, done: true}
			close(m.flow.progressC)
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
	m.flow.progress = msg.progress
	m.flow.assets = msg.info
	m.flow.err = msg.err

	if !msg.done {
		m.refreshBody()
		return m, waitForAssetUpdate(m.flow.progressC)
	}
	if msg.err != nil {
		m.ui.state = StateNoLuck
		m.refreshBody()
		return m, nil
	}
	if report, err := m.app.Settings.Apply(m.ctx, msg.info.ListsDir); err != nil {
		m.ui.state = StateNoLuck
		m.flow.err = err
		m.refreshBody()
		return m, nil
	} else {
		for _, item := range report {
			m.ui.startupNotices = append(m.ui.startupNotices, item)
		}
	}

	m.ui.state = StateTesting
	m.flow.currentConfig = "starting"
	m.flow.flowC = make(chan flowUpdateMsg)
	m.refreshBody()

	return m, tea.Batch(m.runFlow(m.flow.flowC), waitForFlowUpdate(m.flow.flowC))
}

func (m Model) handleFlowUpdate(msg flowUpdateMsg) Model {
	if m.ui.state == StateClosing {
		return m
	}

	switch msg.kind {
	case flowTesting:
		m.ui.state = StateTesting
		m.flow.currentConfig = msg.currentConfig
		m.flow.configIndex = msg.index
		m.flow.configTotal = msg.total
		m.flow.process = msg.process
	case flowRunning:
		m.ui.state = StateRunning
		m.flow.runningConfig = msg.currentConfig
		m.flow.runStartedAt = time.Now()
		m.flow.process = msg.process
		m.flow.logs = nil
		m.ui.rawLogMode = false
	case flowNoLuck:
		m.ui.state = StateNoLuck
		m.flow.err = msg.err
		m.flow.process = nil
	case flowLog:
		m.flow.logs = append(m.flow.logs, msg.log)
		if len(m.flow.logs) > maxLogLines {
			excess := len(m.flow.logs) - maxLogLines
			m.flow.logs = m.flow.logs[excess:]
		}
	case flowDone:
		m.flow.process = nil
	}

	m.refreshBody()
	return m
}

func (m Model) runFlow(updates chan<- flowUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		flow := Flow{
			Assets:   m.flow.assets,
			Cache:    m.app.Cache,
			Runner:   m.app.Runner,
			Tester:   m.app.Tester,
			Configs:  m.app.Configs,
			Settings: m.app.Settings,
			Ctx:      m.ctx,
		}
		go flow.Run(updates)
		return nil
	}
}

func (m Model) stopRunning() tea.Cmd {
	return func() tea.Msg {
		return cleanupDoneMsg{err: m.app.Runner.Stop()}
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
