package maintenance

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/elev1e1nSure/sieve/internal/assets"
	"github.com/elev1e1nSure/sieve/internal/cache"
	"github.com/elev1e1nSure/sieve/internal/runner"
	"github.com/elev1e1nSure/sieve/internal/selfupdate"
	"github.com/elev1e1nSure/sieve/internal/settings"
)

type Item struct {
	Status  string
	Name    string
	Message string
}

type Report struct {
	Title     string
	Items     []Item
	QuitAfter bool
}

type Service struct {
	assets assets.Manager
}

func NewService() Service {
	return Service{assets: assets.NewManager()}
}

func (s Service) Stop() (Report, error) {
	result, err := runner.StopAll(filepath.Join(s.assets.BinDir(), "winws.exe"))
	if err != nil {
		return Report{}, fmt.Errorf("sieve could not stop cleanly: %w", err)
	}

	message := "sieve is not running"
	status := "ok"
	switch {
	case result.Forced:
		message, status = "sieve did not respond and was force-stopped", "warn"
	case result.Active:
		message = "sieve stopped cleanly"
	case result.Legacy:
		message, status = "stopped leftover sieve processes", "warn"
	}

	return single("Stop sieve", status, "sieve", message), nil
}

func (Service) ResetCache() (Report, error) {
	store := cache.NewStore()
	if err := store.Reset(); err != nil {
		return Report{}, fmt.Errorf("failed to reset cache: %w", err)
	}

	return single("Reset cache", "ok", "cache", "config results removed"), nil
}

func (s Service) UpdateIPSet(ctx context.Context) (Report, error) {
	info, err := s.assets.Ensure(ctx, func(assets.Progress) {})
	if err != nil {
		return Report{}, err
	}
	report, err := settings.UpdateIPSet(ctx, info.ListsDir)
	if err != nil {
		return Report{}, err
	}

	return fromListReport("Update IPSet", report), nil
}

func (s Service) Diagnostics(fix bool) Report {
	return fromDiagnostics("Diagnostics", settings.RunDiagnostics(s.assets.BinDir(), fix))
}

func (Service) ClearDiscordCache() Report {
	return fromDiagnostics("Clear Discord cache", settings.ClearDiscordCache())
}

func (Service) Update(ctx context.Context) (Report, error) {
	result, err := selfupdate.New().Update(ctx, true)
	if err != nil {
		switch {
		case errors.Is(err, selfupdate.ErrNoRelease):
			return single("Update sieve", "warn", "update", "no release found"), nil
		case errors.Is(err, selfupdate.ErrNoAsset):
			return single("Update sieve", "warn", "update", "latest release has no compatible binary"), nil
		case errors.Is(err, selfupdate.ErrGoRun):
			return single("Update sieve", "warn", "update", "self-update is disabled under go run"), nil
		case errors.Is(err, selfupdate.ErrCurrent):
			return single("Update sieve", "ok", "update", "already up to date"), nil
		default:
			return Report{}, fmt.Errorf("update failed: %w", err)
		}
	}

	report := single("Update sieve", "ok", "update", "update scheduled: "+result.Version)
	report.QuitAfter = result.Updated
	return report, nil
}

func single(title, status, name, message string) Report {
	return Report{Title: title, Items: []Item{{Status: status, Name: name, Message: message}}}
}

func fromListReport(title string, source settings.ListReport) Report {
	report := Report{Title: title, Items: make([]Item, 0, len(source.Items))}
	for _, item := range source.Items {
		report.Items = append(report.Items, Item{Status: "ok", Name: item.Kind, Message: item.Message})
	}
	return report
}

func fromDiagnostics(title string, source settings.DiagnosticsReport) Report {
	report := Report{Title: title, Items: make([]Item, 0, len(source.Items))}
	for _, item := range source.Items {
		report.Items = append(report.Items, Item{Status: item.Status, Name: item.Name, Message: item.Message})
	}
	return report
}
