package configs

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/elev1e1nSure/sieve/internal/settings"
)

const sampleBat = "\ufeff@echo off\r\n" +
	":: winws.exe is started below\r\n" +
	"set \"BIN=%~dp0bin\\\"\r\n" +
	"start \"zapret: %~n0\" /min \"%BIN%winws.exe\" --wf-tcp=80,443,%GameFilterTCP% --wf-udp=443,%GameFilterUDP% ^\r\n" +
	"--filter-tcp=443 --hostlist=\"%LISTS%list general.txt\" --dpi-desync-fake-tls=^! --new ^\r\n" +
	"--filter-udp=%gamefilterudp% --dpi-desync-fake-quic=\"%BIN%quic.bin\" --dpi-desync-cutoff=n%%2\r\n" +
	"echo done\r\n"

func TestParseBat(t *testing.T) {
	config, err := parseBat("general (ALT)", []byte(sampleBat))
	if err != nil {
		t.Fatalf("parseBat() error = %v", err)
	}

	want := []string{
		"--wf-tcp=80,443,{GAME_TCP}",
		"--wf-udp=443,{GAME_UDP}",
		"--filter-tcp=443",
		`--hostlist={LISTS}\list general.txt`,
		"--dpi-desync-fake-tls=!",
		"--new",
		"--filter-udp={GAME_UDP}",
		`--dpi-desync-fake-quic={BIN}\quic.bin`,
		"--dpi-desync-cutoff=n%2",
	}
	if config.Name != "general (ALT)" || !reflect.DeepEqual(config.Args, want) {
		t.Fatalf("parseBat() = %q %q, want %q", config.Name, config.Args, want)
	}
}

func TestParseBatRejectsUnknownSyntax(t *testing.T) {
	tests := map[string]string{
		"no command":       "@echo off\r\necho nothing\r\n",
		"no arguments":     "start \"\" \"%BIN%winws.exe\"\r\n",
		"unknown variable": "\"%BIN%winws.exe\" --hostlist=%USERLIST%\r\n",
		"unterminated":     "\"%BIN%winws.exe\" --hostlist=%BIN\r\n",
		"redirection":      "\"%BIN%winws.exe\" --new > nul\r\n",
	}

	for name, bat := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseBat("x", []byte(bat)); err == nil {
				t.Fatal("parseBat() error = nil, want rejection")
			}
		})
	}
}

func TestSnapshotParses(t *testing.T) {
	configs, failed := parseStrategies(snapshot, "flowseal")
	if len(failed) > 0 {
		t.Fatalf("snapshot strategies failed to parse: %v", failed)
	}
	if len(configs) < 20 {
		t.Fatalf("snapshot has %d strategies, want the full Flowseal set", len(configs))
	}
	if configs[0].Name != "general" {
		t.Fatalf("first strategy = %q, want general", configs[0].Name)
	}

	for _, config := range configs {
		for _, arg := range config.Args {
			if !strings.HasPrefix(arg, "--") || strings.ContainsAny(arg, `%"^`) {
				t.Fatalf("%s: unresolved cmd syntax in %q", config.Name, arg)
			}
		}
	}
}

func TestLoadPrefersReleaseStrategies(t *testing.T) {
	dirs := newInstall(t)
	writeFile(t, filepath.Join(dirs.bin, "fake.bin"))
	writeFile(t, filepath.Join(dirs.lists, "list.txt"))
	writeBat(t, dirs.strategies, "general (ALT2)", `--hostlist="%LISTS%list.txt" --dpi-desync-fake-tls="%BIN%fake.bin"`)
	writeBat(t, dirs.strategies, "general (ALT10)", "--new")
	writeBat(t, dirs.strategies, "general", "--new")

	set, err := Load(dirs.strategies, dirs.bin, dirs.lists)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := configNames(set.Configs), []string{"general", "general (ALT2)", "general (ALT10)"}; !slices.Equal(got, want) {
		t.Fatalf("Load() names = %q, want %q", got, want)
	}
	if len(set.Notices) != 0 {
		t.Fatalf("Load() notices = %q, want none", set.Notices)
	}
}

func TestLoadSkipsStrategiesWithMissingFiles(t *testing.T) {
	dirs := newInstall(t)
	writeBat(t, dirs.strategies, "general", "--new")
	writeBat(t, dirs.strategies, "general (ALT)", `--dpi-desync-fake-discord="%BIN%gone.bin"`)

	set, err := Load(dirs.strategies, dirs.bin, dirs.lists)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := configNames(set.Configs); !slices.Equal(got, []string{"general"}) {
		t.Fatalf("Load() names = %q, want only general", got)
	}
	if len(set.Notices) != 1 || !strings.Contains(set.Notices[0], `bin\gone.bin`) {
		t.Fatalf("Load() notices = %q, want the missing file named", set.Notices)
	}
}

func TestLoadFailsWhenNothingIsUsable(t *testing.T) {
	dirs := newInstall(t)
	writeBat(t, dirs.strategies, "general", `--dpi-desync-fake-discord="%BIN%gone.bin"`)

	if _, err := Load(dirs.strategies, dirs.bin, dirs.lists); err == nil || !strings.Contains(err.Error(), `bin\gone.bin`) {
		t.Fatalf("Load() error = %v, want missing file reported", err)
	}
}

func TestLoadFallsBackToSnapshot(t *testing.T) {
	dirs := newInstall(t)
	writeFile(t, filepath.Join(dirs.strategies, "general.bat")) // unparsable
	snapshotConfigs, _ := parseStrategies(snapshot, "flowseal")
	for _, config := range snapshotConfigs {
		for _, file := range config.missingFiles(dirs.bin, dirs.lists) {
			writeFile(t, filepath.Join(dirs.root, file))
		}
	}

	set, err := Load(dirs.strategies, dirs.bin, dirs.lists)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(set.Configs) != len(snapshotConfigs) {
		t.Fatalf("Load() returned %d strategies, want the %d snapshot ones", len(set.Configs), len(snapshotConfigs))
	}
	if len(set.Notices) != 1 || !strings.Contains(set.Notices[0], "built-in snapshot") {
		t.Fatalf("Load() notices = %q, want snapshot fallback announced", set.Notices)
	}
}

func TestResolve(t *testing.T) {
	config := Config{Args: []string{
		`--hostlist={LISTS}\list.txt`,
		`--dpi-desync-fake-quic={BIN}\quic.bin`,
		"--wf-tcp=443,{GAME_TCP}",
		"--filter-udp={GAME_UDP}",
	}}

	got := config.Resolve(`C:\sieve\bin`, `C:\sieve\lists`, settings.RuntimeOptions{GameMode: settings.GameTCP})
	want := []string{
		`--hostlist=C:\sieve\lists\list.txt`,
		`--dpi-desync-fake-quic=C:\sieve\bin\quic.bin`,
		"--wf-tcp=443,1024-65535",
		"--filter-udp=12",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestNaturalCompare(t *testing.T) {
	names := []string{"general (ALT10)", "general (EXP)", "general (ALT2)", "general", "general (ALT)", "general (ALT02x)"}
	slices.SortFunc(names, naturalCompare)

	want := []string{"general", "general (ALT)", "general (ALT2)", "general (ALT02x)", "general (ALT10)", "general (EXP)"}
	if !slices.Equal(names, want) {
		t.Fatalf("sorted = %q, want %q", names, want)
	}
}

type install struct {
	root, bin, lists, strategies string
}

func newInstall(t *testing.T) install {
	t.Helper()
	root := t.TempDir()
	dirs := install{
		root:       root,
		bin:        filepath.Join(root, "bin"),
		lists:      filepath.Join(root, "lists"),
		strategies: filepath.Join(root, "strategies"),
	}
	for _, dir := range []string{dirs.bin, dirs.lists, dirs.strategies} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dirs
}

func writeBat(t *testing.T, dir, name, args string) {
	t.Helper()
	content := "start \"zapret\" /min \"%BIN%winws.exe\" " + args + "\r\n"
	if err := os.WriteFile(filepath.Join(dir, name+".bat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func configNames(configs []Config) []string {
	names := make([]string, len(configs))
	for i, config := range configs {
		names[i] = config.Name
	}
	return names
}
