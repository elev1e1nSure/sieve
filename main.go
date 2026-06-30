package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/your-name/sieve/internal/admin"
	"github.com/your-name/sieve/internal/assets"
	"github.com/your-name/sieve/internal/cache"
	"github.com/your-name/sieve/internal/configs"
	"github.com/your-name/sieve/internal/envpath"
	"github.com/your-name/sieve/internal/runner"
	"github.com/your-name/sieve/internal/tester"
	"github.com/your-name/sieve/internal/ui"
)

func main() {
	testTimeout := flag.Int("test-timeout", 5, "connection test timeout in seconds")
	resetCache := flag.Bool("reset-cache", false, "delete cached config results before running")
	noCache := flag.Bool("no-cache", false, "disable config cache for this run")
	noAddPath := flag.Bool("no-add-path", false, "skip adding sieve.exe directory to user PATH")
	flag.Parse()

	if *testTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "--test-timeout must be greater than 0")
		os.Exit(2)
	}

	startupNotices := make([]string, 0, 3)
	if !*noAddPath {
		result, err := envpath.EnsureExecutableDir()
		startupNotices = appendPathNotice(startupNotices, result, err)
	}

	cacheStore := cache.NewStore()
	if *resetCache {
		if err := cacheStore.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to reset cache: %v\n", err)
			os.Exit(1)
		}
		startupNotices = append(startupNotices, "cache reset")
	}
	if *noCache {
		cacheStore.Disabled = true
		startupNotices = append(startupNotices, "cache disabled for this run")
	}

	adminService := admin.NewService()
	if !adminService.IsAdmin() {
		printStartupNotices(startupNotices)
		if err := adminService.ElevateAndRestart(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to request admin rights: %v\n", err)
			os.Exit(1)
		}
		return
	}

	app := ui.App{
		Assets:         assets.NewManager(),
		Cache:          cacheStore,
		Configs:        configs.All(),
		Runner:         runner.New(),
		Tester:         tester.New(time.Duration(*testTimeout) * time.Second),
		StartupNotices: startupNotices,
	}

	program := tea.NewProgram(ui.NewModel(app))
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to run TUI: %v\n", err)
		os.Exit(1)
	}
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
