package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

const (
	winwsReadyMarker          = "windivert initialized"
	candidateValidationPasses = 2
)

// noLuck and flowFinished are the two terminal messages Run can emit.
func noLuck(err error) flowUpdateMsg {
	return flowUpdateMsg{kind: flowNoLuck, err: err, done: true}
}

func flowFinished() flowUpdateMsg {
	return flowUpdateMsg{kind: flowDone, done: true}
}

func cleanupNotice(err error) flowUpdateMsg {
	return flowUpdateMsg{kind: flowNotice, log: err.Error()}
}

func (f Flow) Run(updates chan<- flowUpdateMsg) {
	defer func() {
		if r := recover(); r != nil {
			updates <- noLuck(fmt.Errorf("internal error: %v", r))
		}
		close(updates)
	}()

	store := f.Cache
	if err := store.Load(); err != nil {
		updates <- noLuck(err)
		return
	}
	sorted := store.SortedConfigs(f.Configs)
	total := len(sorted)
	winwsPath := filepath.Join(f.Assets.BinDir, "winws.exe")
	if err := f.Runner.Prepare(winwsPath); err != nil {
		if !runner.IsCleanupOnly(err) {
			updates <- noLuck(err)
			return
		}
		updates <- cleanupNotice(err)
	}
	startFailures := 0
	var firstStartErr error

	for i, config := range sorted {
		select {
		case <-f.Ctx.Done():
			updates <- flowFinished()
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
			startFailures++
			if firstStartErr == nil {
				firstStartErr = err
			}
			if cacheErr := store.RecordResult(config.Name, false, time.Now()); cacheErr != nil {
				updates <- noLuck(cacheErr)
				return
			}
			continue
		}

		startupOutput, err := waitForProcessReady(f.Ctx, process, winwsReadinessTimeout)
		if errors.Is(err, context.Canceled) {
			_ = f.Runner.Stop()
			updates <- flowFinished()
			return
		}
		if err == nil {
			ok := true
			var testErr error
			for range candidateValidationPasses {
				result, terr := f.testProcess(process)
				if terr != nil {
					testErr = terr
					ok = false
					break
				}
				if !result.Discord || !result.YouTube || result.Err != nil {
					ok = false
					break
				}
			}
			if errors.Is(testErr, context.Canceled) {
				_ = f.Runner.Stop()
				updates <- flowFinished()
				return
			}
			err = testErr
			if err == nil {
				if cacheErr := store.RecordResult(config.Name, ok, time.Now()); cacheErr != nil {
					_ = f.Runner.Stop()
					updates <- noLuck(cacheErr)
					return
				}
				if ok {
					f.streamLogs(updates, config.Name, process, startupOutput)
					return
				}
			}
		}
		if err != nil {
			// winws died before or during validation — a per-config failure,
			// not a reason to abandon the remaining configs.
			startFailures++
			if firstStartErr == nil {
				firstStartErr = withProcessOutput(err, startupOutput)
			}
			if cacheErr := store.RecordResult(config.Name, false, time.Now()); cacheErr != nil {
				_ = f.Runner.Stop()
				updates <- noLuck(cacheErr)
				return
			}
		}

		if stopErr := f.Runner.Stop(); stopErr != nil {
			if !runner.IsCleanupOnly(stopErr) {
				updates <- noLuck(withProcessOutput(stopErr, drainProcessOutput(process, 3)))
				return
			}
			updates <- cleanupNotice(stopErr)
		}
	}

	if startFailures == total && firstStartErr != nil {
		updates <- noLuck(fmt.Errorf("winws could not start or stay up for any config: %w", firstStartErr))
		return
	}
	updates <- flowUpdateMsg{kind: flowNoLuck, done: true}
}

// streamLogs relays winws output for the accepted config until the process
// exits on its own (an error) or the flow context is cancelled (a shutdown).
func (f Flow) streamLogs(updates chan<- flowUpdateMsg, configName string, process *runner.Process, startupOutput []string) {
	updates <- flowUpdateMsg{kind: flowRunning, currentConfig: configName, process: process}
	for _, line := range startupOutput {
		updates <- flowUpdateMsg{kind: flowLog, log: line}
	}

	recentOutput := make([]string, 0, 3)
	for {
		select {
		case line, more := <-process.Logs():
			if !more {
				err := process.Wait()
				if err == nil {
					err = errors.New("winws exited unexpectedly")
				}
				err = withProcessOutput(err, recentOutput)
				if cleanupErr := f.Runner.Stop(); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
				}
				updates <- noLuck(err)
				return
			}
			recentOutput = appendRecent(recentOutput, line, 3)
			updates <- flowUpdateMsg{kind: flowLog, log: line}
		case <-f.Ctx.Done():
			_ = f.Runner.Stop()
			updates <- flowFinished()
			return
		}
	}
}

func (f Flow) testProcess(process *runner.Process) (tester.TestResult, error) {
	ctx, cancel := context.WithCancel(f.Ctx)
	defer cancel()

	results := make(chan tester.TestResult, 1)
	go func() {
		results <- f.Tester.Test(ctx)
	}()

	select {
	case result := <-results:
		select {
		case <-process.Done():
			return tester.TestResult{}, processExitError(process)
		default:
			return result, nil
		}
	case <-process.Done():
		return tester.TestResult{}, processExitError(process)
	case <-f.Ctx.Done():
		return tester.TestResult{}, f.Ctx.Err()
	}
}

func waitForProcessReady(ctx context.Context, process *runner.Process, timeout time.Duration) ([]string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	output := make([]string, 0, 8)
	for {
		select {
		case line, more := <-process.Logs():
			if !more {
				return output, processExitError(process)
			}
			output = append(output, line)
			if strings.Contains(strings.ToLower(line), winwsReadyMarker) {
				return output, nil
			}
		case <-timer.C:
			return output, nil
		case <-ctx.Done():
			return output, ctx.Err()
		}
	}
}

func processExitError(process *runner.Process) error {
	err := process.Wait()
	if err == nil {
		return errors.New("winws exited unexpectedly")
	}
	return err
}

func drainProcessOutput(process *runner.Process, limit int) []string {
	lines := make([]string, 0, limit)
	for line := range process.Logs() {
		lines = appendRecent(lines, line, limit)
	}
	return lines
}

func appendRecent(lines []string, line string, limit int) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return lines
	}
	lines = append(lines, line)
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func withProcessOutput(err error, lines []string) error {
	if len(lines) == 0 {
		return fmt.Errorf("winws exited: %w", err)
	}
	return fmt.Errorf("winws exited: %w; last output: %s", err, strings.Join(lines, " | "))
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
	// A cancelled asset download reports an error after ctrl+c; it must not
	// yank the state machine out of the shutdown sequence.
	if m.ui.state == StateClosing || m.ui.state == StateBye {
		return m, nil
	}
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
	if msg.info.Notice != "" {
		m.ui.startupNotices = append(m.ui.startupNotices, msg.info.Notice)
	}
	report, err := m.app.Settings.Apply(m.ctx, msg.info.ListsDir)
	if err != nil {
		m.ui.state = StateNoLuck
		m.flow.err = err
		m.refreshBody()
		return m, nil
	}
	m.ui.startupNotices = append(m.ui.startupNotices, report...)

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
	case flowNotice:
		m.ui.startupNotices = append(m.ui.startupNotices, msg.log)
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
