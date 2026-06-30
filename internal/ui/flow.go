package ui

import (
	"context"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/your-name/sieve/internal/assets"
)

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
		m.refreshBody()
		return m, waitForAssetUpdate(m.progressC)
	}
	if msg.err != nil {
		m.state = StateNoLuck
		m.refreshBody()
		return m, nil
	}
	if report, err := m.app.Settings.Apply(m.ctx, msg.info.ListsDir); err != nil {
		m.state = StateNoLuck
		m.err = err
		m.refreshBody()
		return m, nil
	} else {
		for _, item := range report {
			m.startupNotices = append(m.startupNotices, item)
		}
	}

	m.state = StateTesting
	m.currentConfig = "starting"
	m.flowC = make(chan flowUpdateMsg)
	m.refreshBody()

	return m, tea.Batch(m.runFlow(m.flowC), waitForFlowUpdate(m.flowC))
}

func (m Model) handleFlowUpdate(msg flowUpdateMsg) Model {
	if m.state == StateClosing {
		return m
	}

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
		m.rawLogMode = false
	case flowNoLuck:
		m.state = StateNoLuck
		m.err = msg.err
		m.process = nil
	case flowLog:
		m.logs = append(m.logs, msg.log)
	case flowDone:
		m.process = nil
	}

	m.refreshBody()
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

				process, err := m.app.Runner.Start(winwsPath, config.ResolveWithOptions(m.assets.BinDir, m.assets.ListsDir, m.app.Settings))
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

		return cleanupDoneMsg{}
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
