package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elev1e1nSure/sieve/internal/fsutil"
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

	// Unlike the disposable config cache, settings.json holds user-entered
	// choices (domains, ipset mode, ...); a corrupt file must fail loudly
	// rather than silently reset them, so the user can inspect or fix it.
	var opts RuntimeOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return RuntimeOptions{}, fmt.Errorf("%s is corrupted: %w (delete it to reset to defaults)", s.Path, err)
	}

	return opts.Normalized(), nil
}

func (s Store) Save(opts RuntimeOptions) error {
	payload, err := json.MarshalIndent(opts.Normalized(), "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	return fsutil.WriteAtomic(s.Path, payload)
}
