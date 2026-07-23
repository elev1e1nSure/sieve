package cli

import (
	"context"
	"testing"
)

func TestRootCommandDefaultsToLauncher(t *testing.T) {
	called := false
	gotSkipLauncher := true

	runner := func(_ context.Context, skipLauncher bool) error {
		called = true
		gotSkipLauncher = skipLauncher
		return nil
	}

	root := newRootCommand(runner)
	root.SetArgs([]string{})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error executing root: %v", err)
	}

	if !called {
		t.Fatal("expected runner to be called for root command")
	}
	if gotSkipLauncher {
		t.Errorf("got skipLauncher = %v, want false for root command", gotSkipLauncher)
	}
}

func TestRunSubcommandSkipsLauncher(t *testing.T) {
	called := false
	gotSkipLauncher := false

	runner := func(_ context.Context, skipLauncher bool) error {
		called = true
		gotSkipLauncher = skipLauncher
		return nil
	}

	root := newRootCommand(runner)
	root.SetArgs([]string{"run"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error executing run subcommand: %v", err)
	}

	if !called {
		t.Fatal("expected runner to be called for run subcommand")
	}
	if !gotSkipLauncher {
		t.Errorf("got skipLauncher = %v, want true for run subcommand", gotSkipLauncher)
	}
}

func TestMaintenanceFlagsInRunSubcommand(t *testing.T) {
	opts := options{stop: true}
	if !isMaintenanceAction(opts) {
		t.Errorf("expected stop option to be recognized as maintenance action")
	}

	opts = options{}
	if isMaintenanceAction(opts) {
		t.Errorf("expected empty options to not be recognized as maintenance action")
	}
}
