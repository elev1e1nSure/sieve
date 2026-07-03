package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/elev1e1nSure/sieve/internal/admin"
	"github.com/elev1e1nSure/sieve/internal/assets"
	"github.com/elev1e1nSure/sieve/internal/cache"
	"github.com/elev1e1nSure/sieve/internal/configs"
	"github.com/elev1e1nSure/sieve/internal/envpath"
	"github.com/elev1e1nSure/sieve/internal/runner"
	"github.com/elev1e1nSure/sieve/internal/selfupdate"
	"github.com/elev1e1nSure/sieve/internal/settings"
	"github.com/elev1e1nSure/sieve/internal/tester"
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
	flags.BoolVar(&opts.runtime.NoAddPath, "no-add-path", false, "save PATH auto-add disabled/enabled")
	flags.StringVar(&opts.runtime.IPSetMode, "ipset", settings.IPSetUnchanged, "save ipset mode: loaded, none, any")
	flags.BoolVar(&opts.updateIPSet, "update-ipset", false, "download the latest Flowseal ipset list and exit")
	flags.StringSliceVar(&opts.runtime.Domains, "domain", nil, "save explicit domain(s); can be repeated or comma-separated")
	flags.StringSliceVar(&opts.runtime.DomainFiles, "domain-file", nil, "save a file with explicit domains")
	flags.StringVar(&opts.runtime.GameMode, "game", "", "save game filter mode: off, all, tcp, udp")
	flags.BoolVar(&opts.clearDiscordCache, "clear-discord-cache", false, "close Discord, delete cache dirs, and exit")
	flags.BoolVar(&opts.diagnostics, "diagnostics", false, "run Windows diagnostics and exit")
	flags.BoolVar(&opts.fix, "fix", false, "allow diagnostics to fix known service/TCP timestamp issues")

	applyStyledTemplates(root)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, failStyle.Render("✗")+" "+err.Error())
		os.Exit(1)
	}
}

// applyStyledTemplates wires sieve's palette into cobra's --help/--version
// output, which otherwise renders as plain, uncolored text.
func applyStyledTemplates(root *cobra.Command) {
	cobra.AddTemplateFunc("heading", func(s string) string {
		return titleStyle.Render(s)
	})
	cobra.AddTemplateFunc("dim", func(s string) string {
		return mutedStyle.Render(s)
	})

	root.SetUsageTemplate(`{{heading "Usage:"}}
  {{.UseLine}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`)

	root.SetHelpTemplate(`{{.Long}}

{{.UsageString}}`)

	root.SetVersionTemplate(titleStyle.Render(root.Use) + " " + mutedStyle.Render(root.Version) + "\n")
}

func runCommandMode(ctx context.Context, flags *pflag.FlagSet, opts options) error {
	if opts.fix && !opts.diagnostics {
		return fmt.Errorf("--fix only works together with --diagnostics")
	}
	if opts.stop {
		if changedFlagCount(flags) != 1 {
			return fmt.Errorf("--stop cannot be combined with other flags")
		}
		adminService := admin.NewService()
		if !adminService.IsAdmin() {
			if err := adminService.ElevateAndRestart(); err != nil {
				return fmt.Errorf("failed to request admin rights: %w", err)
			}
			return nil
		}

		manager := assets.NewManager()
		stopped, err := runner.StopAll(filepath.Join(manager.BinDir(), "winws.exe"))
		if err != nil {
			return fmt.Errorf("failed to stop sieve: %w", err)
		}
		if stopped {
			fmt.Println(ok("sieve stopped"))
		} else {
			fmt.Println(ok("sieve is not running"))
		}
		return nil
	}

	store := settings.NewStore()
	runtime, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if err := applyRuntimeFlags(flags, &runtime, opts.runtime); err != nil {
		return err
	}
	if err := validateSettings(runtime); err != nil {
		return err
	}

	changedRuntime := runtimeFlagsChanged(flags)
	if changedRuntime {
		if err := store.Save(runtime); err != nil {
			return fmt.Errorf("failed to save settings: %w", err)
		}
		fmt.Println(ok("settings saved") + "  " + mutedStyle.Render(store.Path))
		printSavedRuntime(runtime)
	}

	if opts.resetCache {
		cacheStore := cache.NewStore()
		if err := cacheStore.Reset(); err != nil {
			return fmt.Errorf("failed to reset cache: %w", err)
		}
		fmt.Println(ok("cache reset"))
	}

	manager := assets.NewManager()
	if opts.updateIPSet {
		info, err := ensureAssetsQuiet(ctx, manager)
		if err != nil {
			return err
		}
		report, err := settings.UpdateIPSet(ctx, info.ListsDir)
		if err != nil {
			return err
		}
		printListReport("IPSet", report)
	}
	if opts.diagnostics {
		printDiagnostics("Diagnostics", settings.RunDiagnostics(manager.BinDir(), opts.fix))
	}
	if opts.clearDiscordCache {
		printDiagnostics("Discord Cache", settings.ClearDiscordCache())
	}
	if opts.update {
		return runSelfUpdate(ctx, false)
	}

	return nil
}

func runApp(ctx context.Context) (runErr error) {
	adminService := admin.NewService()
	if !adminService.IsAdmin() {
		if err := adminService.ElevateAndRestart(); err != nil {
			return fmt.Errorf("failed to request admin rights: %w", err)
		}
		return nil
	}

	startupNotices := make([]string, 0, 6)

	if updated, err := autoUpdate(ctx); updated {
		return nil
	} else if err != nil {
		startupNotices = append(startupNotices, "update check skipped: "+err.Error())
	}

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

	if !runtime.NoAddPath {
		result, err := envpath.EnsureExecutableDir()
		startupNotices = appendPathNotice(startupNotices, result, err)
	}

	cacheStore := cache.NewStore()
	if runtime.NoCache {
		cacheStore.Disabled = true
		startupNotices = append(startupNotices, "cache disabled")
	}

	app := ui.App{
		Assets:         assets.NewManager(),
		Cache:          &cacheStore,
		Configs:        configs.All(),
		Runner:         processRunner,
		Tester:         tester.New(time.Duration(runtime.TestTimeout) * time.Second),
		StartupNotices: startupNotices,
		Settings:       runtime,
	}

	program := tea.NewProgram(ui.NewModel(app))
	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}
	if model, ok := finalModel.(ui.Model); ok && model.ShutdownError() != nil {
		return fmt.Errorf("failed to stop sieve: %w", model.ShutdownError())
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

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
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

func runSelfUpdate(ctx context.Context, restart bool) error {
	result, err := selfupdate.New().Update(ctx, restart)
	if err != nil {
		switch {
		case errors.Is(err, selfupdate.ErrNoRelease):
			fmt.Println(warn("no release found") + "  create a GitHub release with a sieve binary first")
			return nil
		case errors.Is(err, selfupdate.ErrNoAsset):
			fmt.Println(warn("no compatible asset") + "  latest release has no sieve binary attached")
			return nil
		case errors.Is(err, selfupdate.ErrGoRun):
			fmt.Println(warn("update skipped") + "  self-update is disabled under go run")
			return nil
		case errors.Is(err, selfupdate.ErrCurrent):
			fmt.Println(ok("already up to date"))
			return nil
		default:
			return fmt.Errorf("update failed: %w", err)
		}
	}
	if result.Updated {
		fmt.Println(ok("update scheduled") + "  " + mutedStyle.Render(result.Version))
	}

	return nil
}

func ensureAssetsQuiet(ctx context.Context, manager assets.Manager) (assets.Info, error) {
	info, err := manager.Ensure(ctx, func(assets.Progress) {})
	if err != nil {
		return assets.Info{}, err
	}

	return info, nil
}

func applyRuntimeFlags(flags *pflag.FlagSet, dst *settings.RuntimeOptions, values settings.RuntimeOptions) error {
	if flags.Changed("test-timeout") {
		if values.TestTimeout <= 0 {
			return fmt.Errorf("--test-timeout must be greater than 0")
		}
		dst.TestTimeout = values.TestTimeout
	}
	if flags.Changed("no-cache") {
		dst.NoCache = values.NoCache
	}
	if flags.Changed("no-add-path") {
		dst.NoAddPath = values.NoAddPath
	}
	if flags.Changed("ipset") {
		dst.IPSetMode = values.IPSetMode
	}
	if flags.Changed("domain") {
		dst.Domains = appendUnique(dst.Domains, values.Domains...)
	}
	if flags.Changed("domain-file") {
		dst.DomainFiles = appendUnique(dst.DomainFiles, values.DomainFiles...)
	}
	if flags.Changed("game") {
		dst.GameMode = values.GameMode
	}

	*dst = dst.Normalized()
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

func hasChangedFlags(flags *pflag.FlagSet) bool {
	changed := false
	flags.Visit(func(*pflag.Flag) {
		changed = true
	})

	return changed
}

func changedFlagCount(flags *pflag.FlagSet) int {
	count := 0
	flags.Visit(func(*pflag.Flag) {
		count++
	})
	return count
}

func runtimeFlagsChanged(flags *pflag.FlagSet) bool {
	for _, name := range []string{"test-timeout", "no-cache", "no-add-path", "ipset", "domain", "domain-file", "game"} {
		if flags.Changed(name) {
			return true
		}
	}

	return false
}

func appendUnique(current []string, incoming ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(current)+len(incoming))
	for _, value := range append(current, incoming...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}

	return out
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

func printSavedRuntime(opts settings.RuntimeOptions) {
	rows := [][2]string{
		{"test-timeout", fmt.Sprintf("%ds", opts.TestTimeout)},
		{"cache", boolStatus(!opts.NoCache)},
		{"PATH auto-add", boolStatus(!opts.NoAddPath)},
		{"ipset", fallback(opts.IPSetMode, "unchanged")},
		{"game", fallback(opts.GameMode, settings.GameOff)},
	}
	if len(opts.Domains) > 0 {
		rows = append(rows, [2]string{"domains", strings.Join(opts.Domains, ", ")})
	}
	if len(opts.DomainFiles) > 0 {
		rows = append(rows, [2]string{"domain files", strings.Join(opts.DomainFiles, ", ")})
	}
	printRows(rows)
}

func printListReport(title string, report settings.ListReport) {
	fmt.Println(section(title))
	for _, item := range report.Items {
		fmt.Println(dotStyle.Render("·") + "  " + mutedStyle.Render(item.Kind) + "  " + item.Message)
	}
}

func printDiagnostics(title string, report settings.DiagnosticsReport) {
	fmt.Println(section(title))
	width := 0
	for _, item := range report.Items {
		width = max(width, len(item.Name))
	}
	for _, item := range report.Items {
		glyph, style := diagnosticGlyph(item.Status)
		name := nameStyle.Render(fmt.Sprintf("%-*s", width, item.Name))
		fmt.Println(style.Render(glyph) + "  " + name + "  " + mutedStyle.Render(item.Message))
	}
}

// diagnosticGlyph mirrors the dot/badge language internal/ui already uses
// for run status, so flag-mode output and the TUI read as the same voice.
func diagnosticGlyph(status string) (string, lipgloss.Style) {
	switch status {
	case "fail":
		return "✗", failStyle
	case "warn":
		return "!", warnStyle
	case "fixed":
		return "↻", fixedStyle
	default:
		return "✓", successStyle
	}
}

// printRows renders aligned key/value pairs with a rust dot marker,
// matching the "· mm:ss" style internal/ui uses elsewhere.
func printRows(rows [][2]string) {
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		key := nameStyle.Render(fmt.Sprintf("%-*s", width, row[0]))
		fmt.Println(dotStyle.Render("·") + "  " + key + "  " + row[1])
	}
}

func section(title string) string {
	return titleStyle.Render(title)
}

func ok(message string) string {
	return successStyle.Render("✓") + " " + message
}

func warn(message string) string {
	return warnStyle.Render("!") + " " + message
}

func boolStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}

	return value
}

// Palette mirrors internal/ui's view.go, which mirrors the sieve.dev site:
// warm dark grays with a rust accent. Kept duplicated rather than exported
// from internal/ui to avoid coupling the flag-mode CLI to the TUI package.
var (
	colorFg      = lipgloss.Color("#D8D4C8")
	colorFgDim   = lipgloss.Color("#8A8478")
	colorFgFaint = lipgloss.Color("#4A4843")
	colorRustHi  = lipgloss.Color("#B08458")
	colorSuccess = lipgloss.Color("#8FA878")
	colorWarn    = lipgloss.Color("#C2A668")
	colorError   = lipgloss.Color("#B5533C")
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorRustHi)
	nameStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorFg)
	dotStyle     = lipgloss.NewStyle().Foreground(colorRustHi)
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	fixedStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorRustHi)
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorWarn)
	failStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorError)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorFgFaint)
)
