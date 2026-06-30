//go:build windows

package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elev1e1nSure/sieve/internal/paths"
)

type DiagnosticsReport struct {
	Items []DiagnosticItem
}

type DiagnosticItem struct {
	Status  string
	Name    string
	Message string
}

func RunDiagnostics(binDir string, autoFix bool) DiagnosticsReport {
	report := DiagnosticsReport{}

	report.addItem(okIf(serviceRunning("BFE"), "Base Filtering Engine", "required Windows filtering service is running", "required Windows filtering service is not running"))
	report.addItem(okIf(!systemProxyEnabled(), "System proxy", "system proxy is disabled", "system proxy is enabled; disable it if it is not intentional"))

	if tcpTimestampsEnabled() {
		report.add("ok", "TCP timestamps", "enabled")
	} else if autoFix && run("netsh", "interface", "tcp", "set", "global", "timestamps=enabled") == nil {
		report.add("fixed", "TCP timestamps", "enabled through netsh")
	} else {
		report.add("warn", "TCP timestamps", "disabled; run diagnostics with --fix to enable")
	}

	report.addItem(okIf(!processRunning("AdguardSvc.exe"), "Adguard", "process not found", "AdguardSvc.exe may conflict with Discord traffic"))
	report.addItem(okIf(!serviceNameContains("Killer"), "Killer services", "not found", "Killer services can conflict with WinDivert"))
	report.addItem(okIf(!intelConnectivityFound(), "Intel Connectivity", "not found", "Intel Connectivity Network Service can conflict with WinDivert"))
	report.addItem(okIf(!checkpointFound(), "Check Point", "not found", "Check Point services can conflict with WinDivert"))
	report.addItem(okIf(!serviceNameContains("SmartByte"), "SmartByte", "not found", "SmartByte can conflict with WinDivert"))

	if matches, _ := filepath.Glob(filepath.Join(binDir, "*.sys")); len(matches) > 0 {
		report.add("ok", "WinDivert driver", "driver file found")
	} else {
		report.add("fail", "WinDivert driver", "WinDivert64.sys file was not found in bin")
	}

	if vpn := matchingServiceNames("VPN"); len(vpn) > 0 {
		report.add("warn", "VPN services", "found: "+strings.Join(vpn, ", "))
	} else {
		report.add("ok", "VPN services", "not found")
	}

	if secureDNSConfigured() {
		report.add("ok", "Secure DNS", "Windows DoH configuration found")
	} else {
		report.add("warn", "Secure DNS", "configure browser or Windows secure DNS if DNS blocking is suspected")
	}

	if hostsContainsYouTube() {
		report.add("warn", "hosts file", "youtube.com or youtu.be entries found")
	} else {
		report.add("ok", "hosts file", "no YouTube entries found")
	}

	reportWinDivertConflict(&report, autoFix)
	reportConflictingServices(&report, autoFix)

	return report
}

func ClearDiscordCache() DiagnosticsReport {
	report := DiagnosticsReport{}
	if processRunning("Discord.exe") {
		if err := run("taskkill", "/IM", "Discord.exe", "/F"); err != nil {
			report.add("fail", "Discord", "failed to close Discord.exe: "+err.Error())
		} else {
			report.add("fixed", "Discord", "closed Discord.exe")
		}
	}

	appData := os.Getenv("APPDATA")
	if strings.TrimSpace(appData) == "" {
		report.add("fail", "Discord cache", "APPDATA is not set")
		return report
	}

	base := filepath.Join(appData, "discord")
	for _, name := range []string{"Cache", "Code Cache", "GPUCache"} {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			report.add("warn", name, "cache directory does not exist")
			continue
		} else if err != nil {
			report.add("fail", name, err.Error())
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			report.add("fail", name, "failed to delete: "+err.Error())
		} else {
			report.add("fixed", name, "deleted "+path)
		}
	}

	return report
}

func (r *DiagnosticsReport) add(status, name, message string) {
	r.Items = append(r.Items, DiagnosticItem{Status: status, Name: name, Message: message})
}

func (r *DiagnosticsReport) addItem(item DiagnosticItem) {
	r.Items = append(r.Items, item)
}

func okIf(ok bool, name, okMessage, badMessage string) DiagnosticItem {
	if ok {
		return DiagnosticItem{Status: "ok", Name: name, Message: okMessage}
	}

	return DiagnosticItem{Status: "warn", Name: name, Message: badMessage}
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

func reportWinDivertConflict(report *DiagnosticsReport, autoFix bool) {
	if processRunning("winws.exe") || !serviceRunning("WinDivert") {
		report.add("ok", "WinDivert", "no orphaned active service found")
		return
	}

	if !autoFix {
		report.add("warn", "WinDivert", "service is active while winws.exe is not running; run diagnostics with --fix")
		return
	}

	_ = run("net", "stop", "WinDivert")
	_ = run("sc", "delete", "WinDivert")
	if serviceRunning("WinDivert") {
		report.add("fail", "WinDivert", "failed to remove orphaned service")
	} else {
		report.add("fixed", "WinDivert", "removed orphaned service")
	}
}

func reportConflictingServices(report *DiagnosticsReport, autoFix bool) {
	conflicts := []string{"GoodbyeDPI", "discordfix_zapret", "winws1", "winws2"}
	var found []string
	for _, name := range conflicts {
		if _, err := paths.SystemCommand("sc", "query", name).CombinedOutput(); err == nil {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		report.add("ok", "Bypass services", "no known conflicting services found")
		return
	}
	if !autoFix {
		report.add("warn", "Bypass services", "found conflicting services: "+strings.Join(found, ", "))
		return
	}

	for _, name := range found {
		_ = run("net", "stop", name)
		if err := run("sc", "delete", name); err != nil {
			report.add("fail", name, "failed to delete: "+err.Error())
		} else {
			report.add("fixed", name, "deleted conflicting service")
		}
	}
	_ = run("net", "stop", "WinDivert")
	_ = run("sc", "delete", "WinDivert")
	_ = run("net", "stop", "WinDivert14")
	_ = run("sc", "delete", "WinDivert14")
}

func run(name string, args ...string) error {
	out, err := paths.SystemCommand(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return nil
}
