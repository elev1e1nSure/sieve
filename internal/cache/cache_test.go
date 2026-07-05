package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/elev1e1nSure/sieve/internal/configs"
)

func testStore(t *testing.T) Store {
	t.Helper()
	return Store{
		Path: filepath.Join(t.TempDir(), "cache.json"),
		Data: Data{Configs: map[string]Record{}},
	}
}

func names(list []configs.Config) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.Name)
	}
	return out
}

func TestLoadMissingFile(t *testing.T) {
	store := testStore(t)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store.Data.Configs == nil {
		t.Fatal("Configs map not initialized")
	}
}

func TestRecordResultRoundTrip(t *testing.T) {
	store := testStore(t)
	now := time.Now().Truncate(time.Second)

	if err := store.RecordResult("alpha", true, now); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if err := store.RecordResult("beta", false, now); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}

	fresh := Store{Path: store.Path}
	if err := fresh.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if fresh.Data.LastWorking != "alpha" {
		t.Fatalf("LastWorking = %q, want alpha", fresh.Data.LastWorking)
	}
	alpha := fresh.Data.Configs["alpha"]
	if alpha.SuccessCount != 1 || !alpha.LastSuccess.Equal(now) {
		t.Fatalf("alpha record = %+v", alpha)
	}
	beta := fresh.Data.Configs["beta"]
	if beta.FailCount != 1 || !beta.LastFailure.Equal(now) {
		t.Fatalf("beta record = %+v", beta)
	}
}

func TestSortedConfigsRanking(t *testing.T) {
	now := time.Now()
	store := testStore(t)
	store.Data.LastWorking = "last-working"
	store.Data.Configs = map[string]Record{
		"last-working": {SuccessCount: 1, LastSuccess: now.Add(-2 * time.Hour)},
		"success-old":  {SuccessCount: 2, LastSuccess: now.Add(-1 * time.Hour)},
		"success-new":  {SuccessCount: 1, LastSuccess: now},
		"failed":       {FailCount: 3, LastFailure: now},
	}

	all := []configs.Config{
		{Name: "failed"},
		{Name: "untried"},
		{Name: "success-old"},
		{Name: "last-working"},
		{Name: "success-new"},
	}

	got := names(store.SortedConfigs(all))
	want := []string{"last-working", "success-new", "success-old", "untried", "failed"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSortedConfigsDoesNotMutateInput(t *testing.T) {
	store := testStore(t)
	store.Data.LastWorking = "b"

	all := []configs.Config{{Name: "a"}, {Name: "b"}}
	_ = store.SortedConfigs(all)

	if all[0].Name != "a" || all[1].Name != "b" {
		t.Fatalf("input slice mutated: %v", names(all))
	}
}

func TestDisabledStore(t *testing.T) {
	store := testStore(t)
	store.Disabled = true

	if err := store.RecordResult("alpha", true, time.Now()); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	fresh := Store{Path: store.Path}
	if err := fresh.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(fresh.Data.Configs) != 0 || fresh.Data.LastWorking != "" {
		t.Fatalf("disabled store persisted data: %+v", fresh.Data)
	}

	// Sorting must keep the original order when the cache is off.
	all := []configs.Config{{Name: "b"}, {Name: "a"}}
	got := names(store.SortedConfigs(all))
	if got[0] != "b" || got[1] != "a" {
		t.Fatalf("disabled store reordered configs: %v", got)
	}
}
