package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/your-name/sieve/internal/admin"
	"github.com/your-name/sieve/internal/assets"
	"github.com/your-name/sieve/internal/cache"
	"github.com/your-name/sieve/internal/configs"
	"github.com/your-name/sieve/internal/envpath"
	"github.com/your-name/sieve/internal/runner"
	"github.com/your-name/sieve/internal/settings"
	"github.com/your-name/sieve/internal/tester"
	"github.com/your-name/sieve/internal/ui"
)

type options struct {
	testTimeout int
	resetCache  bool
	noCache     bool
	noAddPath   bool
	settings    settings.RuntimeOptions
}

func Execute() {
	opts := options{}
	root := &cobra.Command{
		Use:          "sieve",
		Short:        "Run zapret configs for Discord and YouTube",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts)
		},
	}

	flags := root.Flags()
	flags.IntVar(&opts.testTimeout, "test-timeout", 5, "connection test timeout in seconds")
	flags.BoolVar(&opts.resetCache, "reset-cache", false, "delete cached config results before running")
	flags.BoolVar(&opts.noCache, "no-cache", false, "disable config cache for this run")
	flags.BoolVar(&opts.noAddPath, "no-add-path", false, "skip adding sieve.exe directory to user PATH")
	flags.StringVar(&opts.settings.IPSetMode, "ipset", settings.IPSetUnchanged, "ipset mode: loaded, none, any")
	flags.BoolVar(&opts.settings.UpdateIPSet, "update-ipset", false, "download the latest Flowseal ipset list")
	flags.StringSliceVar(&opts.settings.Domains, "domain", nil, "explicit domain(s) to add to list-general-user.txt; can be repeated or comma-separated")
	flags.StringSliceVar(&opts.settings.DomainFiles, "domain-file", nil, "file with explicit domains to merge into list-general-user.txt")
	flags.StringVar(&opts.settings.GameMode, "game", settings.GameOff, "game filter mode: off, all, tcp, udp")
	flags.BoolVar(&opts.settings.ClearDiscordCache, "clear-discord-cache", false, "close Discord and delete Cache, Code Cache, and GPUCache")
	flags.BoolVar(&opts.settings.Diagnostics, "diagnostics", false, "run Windows diagnostics before starting")
	flags.BoolVar(&opts.settings.AutoFix, "fix", false, "allow diagnostics to fix known service/TCP timestamp issues")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render(err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	if opts.testTimeout <= 0 {
		return fmt.Errorf("--test-timeout must be greater than 0")
	}
	if err := validateSettings(opts.settings); err != nil {
		return err
	}

	startupNotices := make([]string, 0, 6)
	if !opts.noAddPath {
		result, err := envpath.EnsureExecutableDir()
		startupNotices = appendPathNotice(startupNotices, result, err)
	}

	cacheStore := cache.NewStore()
	if opts.resetCache {
		if err := cacheStore.Reset(); err != nil {
			return fmt.Errorf("failed to reset cache: %w", err)
		}
		startupNotices = append(startupNotices, "cache reset")
	}
	if opts.noCache {
		cacheStore.Disabled = true
		startupNotices = append(startupNotices, "cache disabled for this run")
	}

	adminService := admin.NewService()
	if !adminService.IsAdmin() {
		printStartupNotices(startupNotices)
		if err := adminService.ElevateAndRestart(); err != nil {
			return fmt.Errorf("failed to request admin rights: %w", err)
		}
		return nil
	}

	manager := assets.NewManager()
	maintenanceOnly := opts.settings.Diagnostics || opts.settings.ClearDiscordCache
	runRequested := opts.settings.HasListChanges() || !gameOff(opts.settings.GameMode) || !maintenanceOnly

	if opts.settings.Diagnostics {
		printDiagnostics("Diagnostics", settings.RunDiagnostics(manager.BinDir(), opts.settings.AutoFix))
	}
	if opts.settings.ClearDiscordCache {
		printDiagnostics("Discord Cache", settings.ClearDiscordCache())
	}
	if maintenanceOnly && !runRequested {
		return nil
	}

	app := ui.App{
		Assets:         manager,
		Cache:          cacheStore,
		Configs:        configs.All(),
		Runner:         runner.New(),
		Tester:         tester.New(time.Duration(opts.testTimeout) * time.Second),
		StartupNotices: startupNotices,
		Settings:       opts.settings,
	}

	program := tea.NewProgram(ui.NewModel(app))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}

func validateSettings(opts settings.RuntimeOptions) error {
	switch strings.ToLower(strings.TrimSpace(opts.IPSetMode)) {
	case "", settings.IPSetLoaded, settings.IPSetNone, settings.IPSetAny:
	default:
		return fmt.Errorf("invalid --ipset %q: use loaded, none, or any", opts.IPSetMode)
	}

	switch strings.ToLower(strings.TrimSpace(opts.GameMode)) {
	case "", settings.GameOff, settings.GameAll, settings.GameTCP, settings.GameUDP:
	default:
		return fmt.Errorf("invalid --game %q: use off, all, tcp, or udp", opts.GameMode)
	}

	return nil
}

func gameOff(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "" || mode == settings.GameOff
}

func appendPathNotice(notices []string, result envpath.Result, err error) []string {
	if err != nil {
		return append(notices, fmt.Sprintf("PATH update failed: %v", err))
	}

	switch {
	case result.Added:
		return append(notices, fmt.Sprintf("added %s to user PATH; open a new terminal", result.Dir))
	case result.AlreadyPresent:
		return append(notices, fmt.Sprintf("user PATH already contains %s", result.Dir))
	case result.Skipped && result.Reason != "":
		return append(notices, "PATH skipped: "+result.Reason)
	default:
		return notices
	}
}

func printStartupNotices(notices []string) {
	for _, notice := range notices {
		fmt.Fprintln(os.Stderr, notice)
	}
}

func printDiagnostics(title string, report settings.DiagnosticsReport) {
	fmt.Println(titleStyle.Render(title))
	for _, item := range report.Items {
		style := okStyle
		switch item.Status {
		case "fail":
			style = failStyle
		case "warn":
			style = warnStyle
		case "fixed":
			style = fixedStyle
		}
		fmt.Println(style.Render(strings.ToUpper(item.Status)) + " " + nameStyle.Render(item.Name) + " " + item.Message)
	}
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	nameStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Width(22)
	okStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Width(7)
	fixedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("45")).Width(7)
	warnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220")).Width(7)
	failStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Width(7)
	errorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)
