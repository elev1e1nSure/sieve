package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/elev1e1nSure/sieve/internal/admin"
	"github.com/elev1e1nSure/sieve/internal/assets"
	"github.com/elev1e1nSure/sieve/internal/cache"
	"github.com/elev1e1nSure/sieve/internal/configs"
	"github.com/elev1e1nSure/sieve/internal/maintenance"
	"github.com/elev1e1nSure/sieve/internal/runner"
	"github.com/elev1e1nSure/sieve/internal/selfupdate"
	"github.com/elev1e1nSure/sieve/internal/settings"
	"github.com/elev1e1nSure/sieve/internal/tester"
	"github.com/elev1e1nSure/sieve/internal/tray"
	"github.com/elev1e1nSure/sieve/internal/ui"
	"github.com/elev1e1nSure/sieve/internal/version"
)

type options struct {
	update            bool
	stop              bool
	updateIPSet       bool
	resetCache        bool
	clearDiscordCache bool
	diagnostics       bool
	fix               bool
	status            bool
	runtime           settings.RuntimeOptions
}

func Execute() {
	cobra.MousetrapHelpText = ""

	opts := options{}
	root := &cobra.Command{
		Use:   "sieve",
		Short: "Sifts through configs until something works",
		Long: "DPI doesn't negotiate, so sieve doesn't either.\n" +
			"It runs every bundled Discord and YouTube winws config in turn,\n" +
			"keeps the first one that gets traffic through, and remembers it for next time.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hasChangedFlags(cmd.Flags()) {
				return runCommandMode(cmd.Context(), cmd.Flags(), opts)
			}

			return runApp(cmd.Context())
		},
	}

	flags := root.Flags()
	flags.BoolVar(&opts.update, "update", false, "update sieve from the latest GitHub release and exit")
	flags.BoolVar(&opts.stop, "stop", false, "force-stop the active sieve instance and its processes")
	flags.IntVar(&opts.runtime.TestTimeout, "test-timeout", 0, "save connection test timeout in seconds")
	flags.BoolVar(&opts.resetCache, "reset-cache", false, "delete cached config results and exit")
	flags.BoolVar(&opts.runtime.NoCache, "no-cache", false, "save config cache disabled/enabled")
	flags.StringVar(&opts.runtime.IPSetMode, "ipset", settings.IPSetUnchanged, "save ipset mode: loaded, none, any")
	flags.BoolVar(&opts.updateIPSet, "update-ipset", false, "download the latest Flowseal ipset list and exit")
	flags.StringSliceVar(&opts.runtime.Domains, "domain", nil, "save explicit domain(s); can be repeated or comma-separated")
	flags.StringSliceVar(&opts.runtime.DomainFiles, "domain-file", nil, "save a file with explicit domains")
	flags.StringVar(&opts.runtime.GameMode, "game", "", "save game filter mode: off, all, tcp, udp")
	flags.BoolVar(&opts.clearDiscordCache, "clear-discord-cache", false, "close Discord, delete cache dirs, and exit")
	flags.BoolVar(&opts.diagnostics, "diagnostics", false, "run Windows diagnostics and exit")
	flags.BoolVar(&opts.fix, "fix", false, "allow diagnostics to fix known service/TCP timestamp issues")
	flags.BoolVar(&opts.status, "status", false, "report whether sieve/winws is running and exit")

	applyStyledTemplates(root)

	if err := root.Execute(); err != nil {
		// The TUI runs in the alt screen, so anything it rendered is gone by
		// now — this print is the only durable record of what went wrong.
		fmt.Fprintln(os.Stderr, failStyle.Render("✗")+" "+dedupErrorLines(err))
		holdOwnConsole()
		os.Exit(1)
	}
}

// dedupErrorLines flattens a joined error into unique, indented lines;
// cleanup paths often join the same failure from several layers.
func dedupErrorLines(err error) string {
	seen := make(map[string]bool)
	lines := make([]string, 0, 4)
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n  ")
}

// holdOwnConsole keeps the console window open until Enter is pressed, but
// only when sieve owns the console (double-click / elevated relaunch). Without
// this the window closes with the process and the error is never seen.
func holdOwnConsole() {
	if !tray.IsAvailable() {
		return
	}
	fmt.Fprint(os.Stderr, "press enter to close ")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// ensureAdmin reports whether sieve already runs elevated; when it does not,
// it relaunches sieve with a UAC prompt and the caller should exit quietly.
func ensureAdmin() (bool, error) {
	adminService := admin.NewService()
	if adminService.IsAdmin() {
		return true, nil
	}
	if err := adminService.ElevateAndRestart(); err != nil {
		return false, fmt.Errorf("failed to request admin rights: %w", err)
	}

	return false, nil
}

func runApp(ctx context.Context) (runErr error) {
	if elevated, err := ensureAdmin(); !elevated {
		return err
	}

	selfupdate.CleanupStale()

	store := settings.NewStore()
	runtime, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if err := runtime.Validate(); err != nil {
		return err
	}

	// Self-update runs before the TUI: a plain `sieve` checks for a newer
	// release up front, in this console. A successful update installs in place
	// and relaunches into the same console, so this process just exits.
	var startupNotices []string
	if updated, err := autoUpdate(ctx); updated {
		return nil
	} else if err != nil {
		startupNotices = append(startupNotices, "update check skipped: "+err.Error())
	}

	launcherProgram := tea.NewProgram(
		ui.NewLauncher(ctx, store, runtime, maintenance.NewService()),
		tea.WithAltScreen(),
	)
	finalLauncher, err := launcherProgram.Run()
	if err != nil {
		return fmt.Errorf("failed to run launcher TUI: %w", err)
	}
	launcher, choseRun := finalLauncher.(ui.LauncherModel)
	if !choseRun || launcher.Choice() != ui.LauncherRun {
		return nil
	}

	if err := runSieve(ctx, startupNotices); err != nil {
		return err
	}
	// The TUI's goodbye view lives in the alt screen and vanishes on exit,
	// so the clean-exit confirmation has to be printed here.
	fmt.Println(ok("sieve stopped cleanly"))
	return nil
}

func runSieve(ctx context.Context, startupNotices []string) (runErr error) {
	session, err := runner.BeginSession()
	if err != nil {
		return err
	}
	defer session.KeepAlive()

	processRunner := runner.New()
	defer func() {
		runErr = errors.Join(runErr, processRunner.Stop())
	}()

	store := settings.NewStore()
	runtime, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if err := validateSettings(runtime); err != nil {
		return err
	}

	cacheStore := cache.NewStore()
	if runtime.NoCache {
		cacheStore.Disabled = true
		startupNotices = append(startupNotices, "cache disabled")
	}

	// Build tray manager only when the console belongs to sieve itself
	// (double-click / Start-Process). When running inside PowerShell or
	// cmd.exe we leave trayMgr nil so the UI never shows the hint.
	var trayMgr *tray.Manager
	var programRef *tea.Program // set after program is created, read by tray callbacks
	if tray.IsAvailable() {
		onRestore := func() {
			// Called from the tray's event-loop goroutine.
			// Restore() shows the console; TrayRestoreMsg triggers repaint.
			if trayMgr != nil {
				trayMgr.Restore()
			}
			if programRef != nil {
				programRef.Send(ui.TrayRestoreMsg{})
			}
		}
		onQuit := func() {
			if programRef != nil {
				programRef.Send(ui.StopRequestedMsg{})
			}
		}
		trayMgr = tray.New(onRestore, onQuit)
		defer trayMgr.Stop()
	}

	app := ui.App{
		Assets:         assets.NewManager(),
		Cache:          &cacheStore,
		Configs:        configs.All(),
		Runner:         processRunner,
		Tester:         tester.New(time.Duration(runtime.TestTimeout) * time.Second),
		StartupNotices: startupNotices,
		Settings:       runtime,
		Tray:           trayMgr,
	}

	program := tea.NewProgram(ui.NewModel(app), tea.WithAltScreen())
	// Wire the program reference so tray callbacks can deliver messages.
	// Tray callbacks only fire after program.Run() has started, so there
	// is no data race here.
	programRef = program

	go func() {
		<-session.StopRequested()
		program.Send(ui.StopRequestedMsg{})
	}()
	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}
	if model, ok := finalModel.(ui.Model); ok && model.ShutdownError() != nil {
		return fmt.Errorf("sieve stopped with an error: %w", model.ShutdownError())
	}

	return nil
}

func autoUpdate(ctx context.Context) (updated bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			updated = false
			err = fmt.Errorf("update panic: %v", r)
		}
	}()

	if !version.IsRelease() {
		return false, nil
	}

	// Bounds the release check plus the binary download that runs before the
	// TUI; the updater's own HTTP client (30s) is the tighter real-world limit.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := selfupdate.New().Update(ctx, true)
	if err == nil && result.Updated {
		fmt.Println(ok("updated") + "  restarting")
		return true, nil
	}
	if errors.Is(err, selfupdate.ErrNoRelease) || errors.Is(err, selfupdate.ErrNoAsset) || errors.Is(err, selfupdate.ErrGoRun) || errors.Is(err, selfupdate.ErrCurrent) {
		return false, nil
	}

	return false, err
}
