package ui

import (
	"testing"

	"github.com/elev1e1nSure/sieve/internal/settings"
)

// The settings page relies on settingsRows being settings-first, actions-last,
// with unique actions; activateRow addresses the first six rows by the row*
// constants.
func TestSettingsRowsTableInvariants(t *testing.T) {
	if len(settingsRows) <= rowGame+1 {
		t.Fatalf("settingsRows has %d rows, want settings rows plus actions", len(settingsRows))
	}

	for i, row := range settingsRows {
		isSetting := i <= rowGame
		if isSetting && row.value == nil {
			t.Errorf("row %d (%s) is in the settings range but has no value renderer", i, row.label)
		}
		if !isSetting && row.value != nil {
			t.Errorf("row %d (%s) is in the action range but renders a value", i, row.label)
		}
		if row.label == "" {
			t.Errorf("row %d has no label", i)
		}
	}

	if firstActionRow != rowGame+1 {
		t.Errorf("firstActionRow = %d, want %d", firstActionRow, rowGame+1)
	}

	seen := map[maintenanceAction]bool{}
	for _, row := range settingsRows[firstActionRow:] {
		if seen[row.action] {
			t.Errorf("duplicate action %d (%s)", row.action, row.label)
		}
		seen[row.action] = true
		if actionLabel(row.action) != row.label {
			t.Errorf("actionLabel(%d) = %q, want %q", row.action, actionLabel(row.action), row.label)
		}
	}
}

// Value renderers must not panic on zero options and should render something.
func TestSettingsRowsValueRenderers(t *testing.T) {
	opts := settings.RuntimeOptions{}.Normalized()
	for i := 0; i <= rowGame; i++ {
		if got := settingsRows[i].value(opts); got == "" {
			t.Errorf("row %d (%s) renders empty value", i, settingsRows[i].label)
		}
	}
}
