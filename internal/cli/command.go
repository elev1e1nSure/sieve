package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/elev1e1nSure/sieve/internal/maintenance"
	"github.com/elev1e1nSure/sieve/internal/settings"
)

func runCommandMode(ctx context.Context, flags *pflag.FlagSet, opts options) error {
	if opts.fix && !opts.diagnostics {
		return fmt.Errorf("--fix only works together with --diagnostics")
	}
	if opts.stop {
		if changedFlagCount(flags) != 1 {
			return fmt.Errorf("--stop cannot be combined with other flags")
		}
		if elevated, err := ensureAdmin(); !elevated {
			return err
		}

		report, err := maintenance.NewService().Stop()
		if err != nil {
			return err
		}
		printMaintenanceReport(report)
		return nil
	}
	if opts.update {
		if elevated, err := ensureAdmin(); !elevated {
			return err
		}
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

	if runtimeFlagsChanged(flags) {
		if err := store.Save(runtime); err != nil {
			return fmt.Errorf("failed to save settings: %w", err)
		}
		fmt.Println(ok("settings saved") + "  " + mutedStyle.Render(store.Path))
		printSavedRuntime(runtime)
	}

	service := maintenance.NewService()
	if opts.resetCache {
		report, err := service.ResetCache()
		if err != nil {
			return err
		}
		printMaintenanceReport(report)
	}
	if opts.updateIPSet {
		report, err := service.UpdateIPSet(ctx)
		if err != nil {
			return err
		}
		printMaintenanceReport(report)
	}
	if opts.diagnostics {
		printMaintenanceReport(service.Diagnostics(opts.fix))
	}
	if opts.clearDiscordCache {
		printMaintenanceReport(service.ClearDiscordCache())
	}
	if opts.status {
		printMaintenanceReport(service.Status())
	}
	if opts.update {
		report, err := service.Update(ctx, false)
		if err != nil {
			return err
		}
		printMaintenanceReport(report)
	}

	return nil
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
	return opts.Normalized().Validate()
}

func hasChangedFlags(flags *pflag.FlagSet) bool {
	return changedFlagCount(flags) > 0
}

func changedFlagCount(flags *pflag.FlagSet) int {
	count := 0
	flags.Visit(func(*pflag.Flag) {
		count++
	})
	return count
}

func runtimeFlagsChanged(flags *pflag.FlagSet) bool {
	for _, name := range []string{"test-timeout", "no-cache", "ipset", "domain", "domain-file", "game"} {
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
