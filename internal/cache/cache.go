package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/your-name/sieve/internal/configs"
	"github.com/your-name/sieve/internal/paths"
)

const cacheFileName = "cache.json"

type Store struct {
	Path string
	Data Data
}

type Data struct {
	LastWorking string            `json:"last_working,omitempty"`
	Configs     map[string]Record `json:"configs,omitempty"`
}

type Record struct {
	SuccessCount int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	LastSuccess  time.Time `json:"last_success,omitempty"`
	LastFailure  time.Time `json:"last_failure,omitempty"`
}

func NewStore() Store {
	return Store{
		Path: filepath.Join(paths.InstallDir(), cacheFileName),
		Data: Data{
			Configs: map[string]Record{},
		},
	}
}

func (s *Store) Load() error {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		s.ensureData()
		return nil
	}
	if err != nil {
		return err
	}

	var loaded Data
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	s.Data = loaded
	s.ensureData()
	return nil
}

func (s Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	data := s.Data
	if data.Configs == nil {
		data.Configs = map[string]Record{}
	}

	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".cache-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		os.Remove(tmpName)
		return err
	}

	return nil
}

func (s *Store) RecordResult(name string, success bool, at time.Time) error {
	s.ensureData()

	record := s.Data.Configs[name]
	if success {
		record.SuccessCount++
		record.LastSuccess = at
		s.Data.LastWorking = name
	} else {
		record.FailCount++
		record.LastFailure = at
	}
	s.Data.Configs[name] = record

	return s.Save()
}

func (s Store) SortedConfigs(all []configs.Config) []configs.Config {
	sorted := append([]configs.Config(nil), all...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return s.rank(sorted[i]).Less(s.rank(sorted[j]))
	})

	return sorted
}

func (s Store) rank(config configs.Config) sortRank {
	if config.Name == s.Data.LastWorking {
		return sortRank{group: 0}
	}

	record, ok := s.Data.Configs[config.Name]
	if !ok || (record.SuccessCount == 0 && record.FailCount == 0) {
		return sortRank{group: 2}
	}
	if record.SuccessCount > 0 {
		return sortRank{group: 1, at: record.LastSuccess}
	}

	return sortRank{group: 3, at: record.LastFailure}
}

func (s *Store) ensureData() {
	if s.Data.Configs == nil {
		s.Data.Configs = map[string]Record{}
	}
}

type sortRank struct {
	group int
	at    time.Time
}

func (r sortRank) Less(other sortRank) bool {
	if r.group != other.group {
		return r.group < other.group
	}

	return r.at.After(other.at)
}
