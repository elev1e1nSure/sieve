package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elev1e1nSure/sieve/internal/paths"
)

const (
	updateStatusSuccess = "success"
	updateStatusFailure = "failure"
)

type updateState struct {
	Status    string    `json:"status"`
	Version   string    `json:"version,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func ConsumeFailure() (string, error) {
	state, err := readUpdateState()
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if state.Status != updateStatusFailure {
		return "", nil
	}
	if err := clearUpdateState(); err != nil {
		return "", err
	}

	return fmt.Sprintf("update to %s failed: %s", state.Version, state.Error), nil
}

func verifiedInstalledVersion() string {
	state, err := readUpdateState()
	if err != nil || state.Status != updateStatusSuccess || state.Version == "" || state.SHA256 == "" {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	hash, err := fileHash(exe)
	if err != nil || !strings.EqualFold(hash, state.SHA256) {
		return ""
	}

	return state.Version
}

func writeUpdateSuccess(version, hash string) error {
	return writeUpdateState(updateState{
		Status:    updateStatusSuccess,
		Version:   strings.TrimSpace(version),
		SHA256:    hash,
		Timestamp: time.Now().UTC(),
	})
}

func writeUpdateFailure(version string, updateErr error) error {
	return writeUpdateState(updateState{
		Status:    updateStatusFailure,
		Version:   strings.TrimSpace(version),
		Error:     updateErr.Error(),
		Timestamp: time.Now().UTC(),
	})
}

func readUpdateState() (updateState, error) {
	data, err := os.ReadFile(updateStatePath())
	if err != nil {
		return updateState{}, err
	}

	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return updateState{}, fmt.Errorf("read update state: %w", err)
	}

	return state, nil
}

func writeUpdateState(state updateState) error {
	dir := filepath.Dir(updateStatePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "update-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	clean := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		clean()
		return err
	}
	if err := tmp.Sync(); err != nil {
		clean()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(updateStatePath()); err != nil && !os.IsNotExist(err) {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, updateStatePath()); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func clearUpdateState() error {
	err := os.Remove(updateStatePath())
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

func updateStatePath() string {
	return filepath.Join(paths.InstallDir(), "update-state.json")
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
