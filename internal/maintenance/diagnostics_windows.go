//go:build windows

package maintenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elev1e1nSure/sieve/internal/paths"
)

func diagnosticsItems(binDir string, autoFix bool) []Item {
	var items []Item
	add := func(status, name, message string) {
		items = append(items, Item{Status: status, Name: name, Message: message})
	}

	items = append(items, okIf(serviceRunning("BFE"), "Base Filtering Engine", "required Windows filtering service is running", "required Windows filtering service is not running"))
	items = append(items, okIf(!systemProxyEnabled(), "System proxy", "system proxy is disabled", "system proxy is enabled; disable it if it is not intentional"))

	if tcpTimestampsEnabled() {
		add("ok", "TCP timestamps", "enabled")
	} else if autoFix && run("netsh", "interface", "tcp", "set", "global", "timestamps=enabled") == nil {
		add("fixed", "TCP timestamps", "enabled through netsh")
	} else {
		add("warn", "TCP timestamps", "disabled; run diagnostics with --fix to enable")
	}

	items = append(items, okIf(!processRunning("AdguardSvc.exe"), "Adguard", "process not found", "AdguardSvc.exe may conflict with Discord traffic"))
	items = append(items, okIf(!serviceNameContains("Killer"), "Killer services", "not found", "Killer services can conflict with WinDivert"))
	items = append(items, okIf(!intelConnectivityFound(), "Intel Connectivity", "not found", "Intel Connectivity Network Service can conflict with WinDivert"))
	items = append(items, okIf(!checkpointFound(), "Check Point", "not found", "Check Point services can conflict with WinDivert"))
	items = append(items, okIf(!serviceNameContains("SmartByte"), "SmartByte", "not found", "SmartByte can conflict with WinDivert"))

	if matches, _ := filepath.Glob(filepath.Join(binDir, "*.sys")); len(matches) > 0 {
		add("ok", "WinDivert driver", "driver file found")
	} else {
		add("fail", "WinDivert driver", "WinDivert64.sys file was not found in bin")
	}

	if vpn := matchingServiceNames("VPN"); len(vpn) > 0 {
		add("warn", "VPN services", "found: "+strings.Join(vpn, ", "))
	} else {
		add("ok", "VPN services", "not found")
	}

	if secureDNSConfigured() {
		add("ok", "Secure DNS", "Windows DoH configuration found")
	} else {
		add("warn", "Secure DNS", "configure browser or Windows secure DNS if DNS blocking is suspected")
	}

	if hostsContainsYouTube() {
		add("warn", "hosts file", "youtube.com or youtu.be entries found")
	} else {
		add("ok", "hosts file", "no YouTube entries found")
	}

	items = append(items, winDivertConflictItem(autoFix))
	items = append(items, conflictingServiceItems(autoFix)...)

	return items
}

func statusItems(binDir string) []Item {
	var items []Item

	if processRunning("winws.exe") {
		items = append(items, Item{Status: "ok", Name: "winws.exe", Message: "process is running"})
	} else {
		items = append(items, Item{Status: "warn", Name: "winws.exe", Message: "process is not running"})
	}

	switch {
	case serviceRunning("WinDivert"):
		items = append(items, Item{Status: "ok", Name: "WinDivert driver", Message: "service is running"})
	case matchingServiceNames("WinDivert") != nil:
		items = append(items, Item{Status: "warn", Name: "WinDivert driver", Message: "service is installed but not running"})
	default:
		items = append(items, Item{Status: "warn", Name: "WinDivert driver", Message: "service is not installed"})
	}

	if matches, _ := filepath.Glob(filepath.Join(binDir, "*.sys")); len(matches) == 0 {
		items = append(items, Item{Status: "warn", Name: "WinDivert driver file", Message: "not found in bin directory"})
	}

	return items
}

func clearDiscordCacheItems() []Item {
	var items []Item
	add := func(status, name, message string) {
		items = append(items, Item{Status: status, Name: name, Message: message})
	}

	if processRunning("Discord.exe") {
		if err := run("taskkill", "/IM", "Discord.exe", "/F"); err != nil {
			add("fail", "Discord", "failed to close Discord.exe: "+err.Error())
		} else {
			add("fixed", "Discord", "closed Discord.exe")
		}
	}

	appData := os.Getenv("APPDATA")
	if strings.TrimSpace(appData) == "" {
		add("fail", "Discord cache", "APPDATA is not set")
		return items
	}

	base := filepath.Join(appData, "discord")
	for _, name := range []string{"Cache", "Code Cache", "GPUCache"} {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			add("warn", name, "cache directory does not exist")
			continue
		} else if err != nil {
			add("fail", name, err.Error())
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			add("fail", name, "failed to delete: "+err.Error())
		} else {
			add("fixed", name, "deleted "+path)
		}
	}

	return items
}

func okIf(ok bool, name, okMessage, badMessage string) Item {
	if ok {
		return Item{Status: "ok", Name: name, Message: okMessage}
	}

	return Item{Status: "warn", Name: name, Message: badMessage}
}

func serviceRunning(name string) bool {
	out, err := paths.SystemCommand("sc", "query", name).CombinedOutput()
	return err == nil && strings.Contains(strings.ToUpper(string(out)), "RUNNING")
}

func serviceNameContains(needle string) bool {
	return len(matchingServiceNames(needle)) > 0
}

func matchingServiceNames(needle string) []string {
	out, err := paths.SystemCommand("sc", "query").CombinedOutput()
	if err != nil {
		return nil
	}

	needle = strings.ToLower(needle)
	re := regexp.MustCompile(`(?m)^SERVICE_NAME:\s*(.+)$`)
	var names []string
	for _, match := range re.FindAllStringSubmatch(string(out), -1) {
		name := strings.TrimSpace(match[1])
		if strings.Contains(strings.ToLower(name), needle) {
			names = append(names, name)
		}
	}

	return names
}

func processRunning(image string) bool {
	out, err := paths.SystemCommand("tasklist", "/FI", "IMAGENAME eq "+image).CombinedOutput()
	return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(image))
}

func systemProxyEnabled() bool {
	out, err := paths.SystemCommand("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable").CombinedOutput()
	return err == nil && strings.Contains(string(out), "0x1")
}

func tcpTimestampsEnabled() bool {
	out, err := paths.SystemCommand("netsh", "interface", "tcp", "show", "global").CombinedOutput()
	lower := strings.ToLower(string(out))
	return err == nil && strings.Contains(lower, "timestamps") && strings.Contains(lower, "enabled")
}

func intelConnectivityFound() bool {
	names := matchingServiceNames("Intel")
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "connectivity") && strings.Contains(lower, "network") {
			return true
		}
	}

	return false
}

func checkpointFound() bool {
	return serviceNameContains("TracSrvWrapper") || serviceNameContains("EPWD")
}

func secureDNSConfigured() bool {
	cmd := paths.SystemCommand("powershell.exe", "-NoProfile", "-Command", `(Get-ChildItem -Recurse -Path 'HKLM:System\CurrentControlSet\Services\Dnscache\InterfaceSpecificParameters\' -ErrorAction SilentlyContinue | Get-ItemProperty | Where-Object { $_.DohFlags -gt 0 } | Measure-Object).Count`)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != "0" && strings.TrimSpace(string(out)) != ""
}

func hostsContainsYouTube() bool {
	data, err := os.ReadFile(filepath.Join(paths.SystemRoot(), "System32", "drivers", "etc", "hosts"))
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be")
}

func winDivertConflictItem(_ bool) Item {
	if processRunning("winws.exe") {
		return Item{Status: "ok", Name: "WinDivert", Message: "driver is in use by winws.exe"}
	}
	if serviceRunning("WinDivert") {
		return Item{Status: "ok", Name: "WinDivert", Message: "driver is loaded and may be shared; left untouched"}
	}

	return Item{Status: "ok", Name: "WinDivert", Message: "driver is not active"}
}

func conflictingServiceItems(autoFix bool) []Item {
	conflicts := []string{"GoodbyeDPI", "discordfix_zapret", "winws1", "winws2"}
	var found []string
	for _, name := range conflicts {
		if _, err := paths.SystemCommand("sc", "query", name).CombinedOutput(); err == nil {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return []Item{{Status: "ok", Name: "Bypass services", Message: "no known conflicting services found"}}
	}
	if !autoFix {
		return []Item{{Status: "warn", Name: "Bypass services", Message: "found conflicting services: " + strings.Join(found, ", ")}}
	}

	var items []Item
	for _, name := range found {
		_ = run("net", "stop", name)
		if err := run("sc", "delete", name); err != nil {
			items = append(items, Item{Status: "fail", Name: name, Message: "failed to delete: " + err.Error()})
		} else {
			items = append(items, Item{Status: "fixed", Name: name, Message: "deleted conflicting service"})
		}
	}
	return items
}

func run(name string, args ...string) error {
	out, err := paths.SystemCommand(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return nil
}
