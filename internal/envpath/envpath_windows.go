//go:build windows

package envpath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const userEnvironmentKey = `Environment`

const (
	hwndBroadcast   = 0xffff
	wmSettingChange = 0x001a
	smtoAbortIfHung = 0x0002
)

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

type Result struct {
	Dir            string
	Added          bool
	AlreadyPresent bool
	Skipped        bool
	Reason         string
}

func EnsureExecutableDir() (Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}

	dir := filepath.Clean(filepath.Dir(exe))
	result := Result{Dir: dir}
	if isTemporaryGoRunExecutable(exe) {
		result.Skipped = true
		result.Reason = "go run uses a temporary executable"
		return result, nil
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvironmentKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return result, err
	}
	defer key.Close()

	pathValue, valueType, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		valueType = registry.EXPAND_SZ
	} else if err != nil {
		return result, err
	}

	if containsPath(pathValue, dir) {
		result.AlreadyPresent = true
		return result, nil
	}

	updatedPath := appendPath(pathValue, dir)
	if valueType == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", updatedPath)
	} else {
		err = key.SetStringValue("Path", updatedPath)
	}
	if err != nil {
		return result, err
	}

	addCurrentProcessPath(dir)
	broadcastEnvironmentChange()
	result.Added = true
	return result, nil
}

func appendPath(pathValue, dir string) string {
	pathValue = strings.TrimRight(strings.TrimSpace(pathValue), ";")
	if pathValue == "" {
		return dir
	}

	return pathValue + ";" + dir
}

func addCurrentProcessPath(dir string) {
	current := os.Getenv("PATH")
	if containsPath(current, dir) {
		return
	}

	_ = os.Setenv("PATH", appendPath(current, dir))
}

func containsPath(pathValue, dir string) bool {
	target := comparablePath(dir)
	for _, item := range strings.Split(pathValue, ";") {
		item = strings.Trim(strings.TrimSpace(item), `"`)
		if item == "" {
			continue
		}
		item = expandEnvironmentStrings(item)
		if strings.EqualFold(comparablePath(item), target) {
			return true
		}
	}

	return false
}

func comparablePath(path string) string {
	path = filepath.Clean(path)
	path = strings.TrimRight(path, `\/`)
	return path
}

func expandEnvironmentStrings(value string) string {
	source, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return value
	}

	required, err := windows.ExpandEnvironmentStrings(source, nil, 0)
	if err != nil || required == 0 {
		return value
	}

	buffer := make([]uint16, required)
	if _, err := windows.ExpandEnvironmentStrings(source, &buffer[0], uint32(len(buffer))); err != nil {
		return value
	}

	return windows.UTF16ToString(buffer)
}

func broadcastEnvironmentChange() {
	name, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}

	_, _, _ = sendMessageTimeout.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(name)),
		smtoAbortIfHung,
		5000,
		0,
	)
}

func isTemporaryGoRunExecutable(exe string) bool {
	tempDir := comparablePath(os.TempDir())
	exe = comparablePath(exe)
	return strings.HasPrefix(strings.ToLower(exe), strings.ToLower(tempDir)+string(os.PathSeparator)) &&
		strings.Contains(strings.ToLower(exe), "go-build")
}
