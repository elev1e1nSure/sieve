package configs

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elev1e1nSure/sieve/internal/settings"
)

const (
	binPlaceholder     = "{BIN}"
	listsPlaceholder   = "{LISTS}"
	gameTCPPlaceholder = "{GAME_TCP}"
	gameUDPPlaceholder = "{GAME_UDP}"

	strategyPattern = "general*.bat"
)

// Config is one winws strategy. Name doubles as the cache key in
// internal/cache, and comes from the Flowseal script's file name.
type Config struct {
	Name string
	Args []string
}

// Set is the strategy list sifting runs through, plus notices about how it
// was assembled that the TUI shows before testing starts.
type Set struct {
	Configs []Config
	Notices []string
}

// snapshot holds the strategy scripts of the Flowseal release named in
// flowseal/version.txt (refresh with `just sync-strategies`). It only covers
// installs that have no release strategies on disk yet.
//
//go:embed flowseal/*.bat
var snapshot embed.FS

// Load prefers the strategies shipped with the installed Flowseal release:
// they always match the bin and lists files next to them, whereas a frozen
// copy drifts as upstream renames or drops fake payloads. Strategies that
// reference a missing file are skipped — winws would exit on them at once.
func Load(strategiesDir, binDir, listsDir string) (Set, error) {
	configs, failed := parseStrategies(os.DirFS(strategiesDir), ".")
	var notices []string
	switch {
	case len(configs) == 0:
		reason := "no strategies in the Flowseal release"
		if len(failed) > 0 {
			reason = "could not parse Flowseal strategies (" + failed[0] + ")"
		}
		// The snapshot is covered by tests, so its parse failures are not
		// worth a runtime notice.
		configs, _ = parseStrategies(snapshot, "flowseal")
		notices = append(notices, reason+" — using the built-in snapshot")
	case len(failed) > 0:
		notices = append(notices, fmt.Sprintf("skipped %d unparsable strategies: %s", len(failed), strings.Join(failed, "; ")))
	}

	usable := make([]Config, 0, len(configs))
	var skipped, missing []string
	for _, config := range configs {
		files := config.missingFiles(binDir, listsDir)
		if len(files) == 0 {
			usable = append(usable, config)
			continue
		}
		skipped = append(skipped, config.Name)
		for _, file := range files {
			if !slices.Contains(missing, file) {
				missing = append(missing, file)
			}
		}
	}

	if len(usable) == 0 {
		if len(missing) > 0 {
			return Set{}, fmt.Errorf("no usable strategies: missing %s", strings.Join(missing, ", "))
		}
		return Set{}, errors.New("no usable strategies found")
	}
	if len(skipped) > 0 {
		notices = append(notices, fmt.Sprintf("skipped %d strategies with missing files: %s", len(skipped), strings.Join(missing, ", ")))
	}

	return Set{Configs: usable, Notices: notices}, nil
}

// parseStrategies returns the parsed strategies in natural name order and a
// "name: error" entry for each script that could not be parsed.
func parseStrategies(fsys fs.FS, dir string) ([]Config, []string) {
	// Glob only fails on a malformed pattern; a missing directory just
	// yields no matches, which Load treats as "no release strategies".
	matches, _ := fs.Glob(fsys, path.Join(dir, strategyPattern))

	configs := make([]Config, 0, len(matches))
	var failed []string
	for _, match := range matches {
		name := strings.TrimSuffix(path.Base(match), path.Ext(match))
		config, err := readStrategy(fsys, match, name)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		configs = append(configs, config)
	}

	slices.SortFunc(configs, func(a, b Config) int { return naturalCompare(a.Name, b.Name) })
	return configs, failed
}

func readStrategy(fsys fs.FS, file, name string) (Config, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Config{}, err
	}
	return parseBat(name, data)
}

func (c Config) Resolve(binDir, listsDir string, opts settings.RuntimeOptions) []string {
	game := opts.Game()
	replacer := strings.NewReplacer(
		binPlaceholder, filepath.Clean(binDir),
		listsPlaceholder, filepath.Clean(listsDir),
		gameTCPPlaceholder, game.TCP,
		gameUDPPlaceholder, game.UDP,
	)

	args := make([]string, len(c.Args))
	for i, arg := range c.Args {
		args[i] = replacer.Replace(arg)
	}
	return args
}

// missingFiles lists referenced bin/lists files that do not exist, relative
// to the install dir for readable notices.
func (c Config) missingFiles(binDir, listsDir string) []string {
	roots := []struct{ placeholder, dir, label string }{
		{binPlaceholder + `\`, binDir, "bin"},
		{listsPlaceholder + `\`, listsDir, "lists"},
	}

	var missing []string
	for _, arg := range c.Args {
		for _, root := range roots {
			_, rel, ok := strings.Cut(arg, root.placeholder)
			if !ok {
				continue
			}
			if _, err := os.Stat(filepath.Join(root.dir, rel)); err != nil {
				missing = append(missing, root.label+`\`+rel)
			}
		}
	}
	return missing
}

// naturalCompare orders names the way people number them, so
// "general (ALT2)" comes before "general (ALT10)".
func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			startA, startB := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			numA := strings.TrimLeft(a[startA:i], "0")
			numB := strings.TrimLeft(b[startB:j], "0")
			if len(numA) != len(numB) {
				return cmp.Compare(len(numA), len(numB))
			}
			if numA != numB {
				return strings.Compare(numA, numB)
			}
			continue
		}
		if a[i] != b[j] {
			return cmp.Compare(a[i], b[j])
		}
		i++
		j++
	}
	return cmp.Compare(len(a)-i, len(b)-j)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
