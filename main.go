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
	"github.com/your-name/sieve/internal/runner"
	"github.com/your-name/sieve/internal/tester"
	"github.com/your-name/sieve/internal/ui"
)

func main() {
	testTimeout := flag.Int("test-timeout", 5, "connection test timeout in seconds")
	flag.Parse()

	if *testTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "--test-timeout must be greater than 0")
		os.Exit(2)
	}

	app := ui.App{
		Admin:   admin.NewService(),
		Assets:  assets.NewManager(),
		Cache:   cache.NewStore(),
		Configs: configs.All(),
		Runner:  runner.New(),
		Tester:  tester.New(time.Duration(*testTimeout) * time.Second),
		Options: ui.Options{
			TestTimeout: time.Duration(*testTimeout) * time.Second,
		},
	}

	program := tea.NewProgram(ui.NewModel(app))
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to run TUI: %v\n", err)
		os.Exit(1)
	}
}
