package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/elev1e1nSure/sieve/internal/paths"
)

const settingsFileName = "settings.json"

type Store struct {
	Path string
}

func NewStore() Store {
	return Store{Path: filepath.Join(paths.InstallDir(), settingsFileName)}
}

func (s Store) Load() (RuntimeOptions, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return RuntimeOptions{}.Normalized(), nil
	}
	if err != nil {
		return RuntimeOptions{}, err
	}

	var opts RuntimeOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return RuntimeOptions{}, err
	}

	return opts.Normalized(), nil
}

func (s Store) Save(opts RuntimeOptions) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(opts.Normalized(), "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".settings-*.json")
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
