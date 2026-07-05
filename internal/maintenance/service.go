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

func (s Service) Status() Report {
	report := Report{Title: "Sieve status", Items: statusItems(s.assets.BinDir())}

	sessionActive, sessionErr := runner.SessionActive()
	sessionItem := Item{Status: "ok", Name: "sieve session", Message: "no other sieve instance is running"}
	switch {
	case sessionErr != nil:
		sessionItem = Item{Status: "warn", Name: "sieve session", Message: sessionErr.Error()}
	case sessionActive:
		sessionItem = Item{Status: "ok", Name: "sieve session", Message: "another sieve instance is running"}
	}
	report.Items = append([]Item{sessionItem}, report.Items...)

	cacheStore := cache.NewStore()
	if err := cacheStore.Load(); err == nil && cacheStore.Data.LastWorking != "" {
		report.Items = append(report.Items, Item{Status: "ok", Name: "last working config", Message: cacheStore.Data.LastWorking})
	} else {
		report.Items = append(report.Items, Item{Status: "warn", Name: "last working config", Message: "none recorded yet"})
	}

	return report
}

func (s Service) Diagnostics(fix bool) Report {
	return Report{Title: "Diagnostics", Items: diagnosticsItems(s.assets.BinDir(), fix)}
}

func (Service) ClearDiscordCache() Report {
	return Report{Title: "Clear Discord cache", Items: clearDiscordCacheItems()}
}

// Update runs the self-updater; restart controls whether the freshly
// installed binary is relaunched in the current console.
func (Service) Update(ctx context.Context, restart bool) (Report, error) {
	result, err := selfupdate.New().Update(ctx, restart)
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
