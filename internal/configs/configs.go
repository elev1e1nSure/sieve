package configs

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/elev1e1nSure/sieve/internal/paths"
	"github.com/elev1e1nSure/sieve/internal/settings"
)

const (
	binPlaceholder   = "{BIN}"
	listsPlaceholder = "{LISTS}"
)

type Config struct {
	Name string
	Args []string
}

func (c Config) ResolveWithOptions(binDir, listsDir string, opts settings.RuntimeOptions) []string {
	args := make([]string, len(c.Args))
	for i, arg := range c.Args {
		arg = replacePathPlaceholder(arg, binPlaceholder, binDir)
		arg = replacePathPlaceholder(arg, listsPlaceholder, listsDir)
		arg = replaceGamePorts(arg, opts.Game())
		args[i] = arg
	}
	return args
}

//go:embed configs.json
var configsJSON []byte

func All() []Config {
	raw := configsJSON
	for _, candidate := range externalConfigPaths() {
		if data, err := os.ReadFile(candidate); err == nil {
			raw = data
			break
		}
	}

	var parsed []Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}

	configs := make([]Config, len(parsed))
	for i := range parsed {
		configs[i].Name = parsed[i].Name
		configs[i].Args = append([]string(nil), parsed[i].Args...)
	}
	return configs
}

func externalConfigPaths() []string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "configs.json"))
	}
	candidates = append(candidates, filepath.Join(paths.InstallDir(), "configs.json"))
	return candidates
}

func replacePathPlaceholder(arg, placeholder, dir string) string {
	if !strings.Contains(arg, placeholder) {
		return arg
	}
	replacement := filepath.Clean(dir)
	return strings.ReplaceAll(arg, placeholder, replacement)
}

func replaceGamePorts(arg string, game settings.GamePorts) string {
	switch {
	case arg == "--filter-tcp=12":
		return "--filter-tcp=" + game.TCP
	case arg == "--filter-udp=12":
		return "--filter-udp=" + game.UDP
	case strings.HasPrefix(arg, "--wf-tcp="):
		return replaceTrailingPort(arg, game.TCP)
	case strings.HasPrefix(arg, "--wf-udp="):
		return replaceTrailingPort(arg, game.UDP)
	default:
		return arg
	}
}

func replaceTrailingPort(arg, port string) string {
	if !strings.HasSuffix(arg, ",12") {
		return arg
	}

	return strings.TrimSuffix(arg, ",12") + "," + port
}
