package settings

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/elev1e1nSure/sieve/internal/fsutil"
)

const (
	ipsetURL      = "https://raw.githubusercontent.com/Flowseal/zapret-discord-youtube/refs/heads/main/.service/ipset-service.txt"
	ipsetSentinel = "203.0.113.113/32"
	managedStart  = "# sieve: managed domains begin"
	managedEnd    = "# sieve: managed domains end"
)

type ListReport struct {
	Items []ReportItem
}

type ReportItem struct {
	Kind    string
	Message string
}

func ApplyLists(ctx context.Context, listsDir string, opts RuntimeOptions) (ListReport, error) {
	report := ListReport{}
	if err := os.MkdirAll(listsDir, 0o755); err != nil {
		return report, err
	}

	switch strings.ToLower(strings.TrimSpace(opts.IPSetMode)) {
	case "":
	case IPSetLoaded:
		msg, err := setIPSetLoaded(filepath.Join(listsDir, "ipset-all.txt"))
		if err != nil {
			return report, err
		}
		report.add("ipset", msg)
	case IPSetNone:
		if err := setIPSetMode(filepath.Join(listsDir, "ipset-all.txt"), ipsetSentinel+"\n"); err != nil {
			return report, err
		}
		report.add("ipset", "none mode: only documentation subnet remains")
	case IPSetAny:
		if err := setIPSetMode(filepath.Join(listsDir, "ipset-all.txt"), ""); err != nil {
			return report, err
		}
		report.add("ipset", "any mode: ipset filter disabled by empty list")
	default:
		return report, fmt.Errorf("invalid ipset mode %q: use loaded, none, or any", opts.IPSetMode)
	}

	domains, err := collectDomains(opts.Domains, opts.DomainFiles)
	if err != nil {
		return report, err
	}
	if err := replaceManagedDomains(filepath.Join(listsDir, "list-general-user.txt"), domains); err != nil {
		return report, err
	}
	if len(domains) > 0 {
		report.add("domains", fmt.Sprintf("configured %d explicit domains in list-general-user.txt", len(domains)))
	}

	return report, nil
}

func UpdateIPSet(ctx context.Context, listsDir string) (ListReport, error) {
	report := ListReport{}
	if err := os.MkdirAll(listsDir, 0o755); err != nil {
		return report, err
	}

	count, err := updateIPSet(ctx, filepath.Join(listsDir, "ipset-all.txt"))
	if err != nil {
		return report, err
	}

	report.add("ipset", fmt.Sprintf("updated from Flowseal service list (%d entries)", count))
	return report, nil
}

func (r *ListReport) add(kind, message string) {
	r.Items = append(r.Items, ReportItem{Kind: kind, Message: message})
}

func updateIPSet(ctx context.Context, path string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipsetURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "sieve")

	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ipset update failed: %s", resp.Status)
	}

	var payload bytes.Buffer
	count, err := copyCountingLines(&payload, resp.Body)
	if err != nil {
		return 0, err
	}
	if err := fsutil.WriteAtomic(path, payload.Bytes()); err != nil {
		return 0, err
	}

	return count, nil
}

func copyCountingLines(dst io.Writer, src io.Reader) (int, error) {
	scanner := bufio.NewScanner(src)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
		}
		if _, err := fmt.Fprintln(dst, scanner.Text()); err != nil {
			return 0, err
		}
	}

	return count, scanner.Err()
}

func setIPSetLoaded(path string) (string, error) {
	backup := path + ".backup"
	if _, err := os.Stat(backup); err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.Rename(backup, path); err != nil {
			return "", err
		}
		return "loaded mode: restored ipset-all.txt.backup", nil
	}

	count, err := countNonEmptyLines(path)
	if err != nil {
		return "", err
	}
	if count > 1 {
		return fmt.Sprintf("loaded mode: current ipset has %d entries", count), nil
	}

	return "", errors.New("loaded ipset requested, but no backup/list is available; run with --update-ipset")
}

func setIPSetMode(path, content string) error {
	if err := backupIPSet(path); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

func backupIPSet(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if isEmptyOrSentinel(data) {
		return nil
	}

	return os.WriteFile(path+".backup", data, 0o644)
}

func isEmptyOrSentinel(data []byte) bool {
	lines := strings.Fields(string(data))
	if len(lines) == 0 {
		return true
	}

	return len(lines) == 1 && lines[0] == ipsetSentinel
}

func countNonEmptyLines(path string) (int, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}

	return count, scanner.Err()
}

func collectDomains(values, files []string) ([]string, error) {
	seen := map[string]bool{}
	for _, value := range values {
		for _, domain := range splitDomains(value) {
			seen[domain] = true
		}
	}

	for _, file := range files {
		domains, err := readDomainFile(file)
		if err != nil {
			return nil, err
		}
		for _, domain := range domains {
			seen[domain] = true
		}
	}

	domains := make([]string, 0, len(seen))
	for domain := range seen {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains, nil
}

func splitDomains(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	domains := make([]string, 0, len(parts))
	for _, part := range parts {
		domain := normalizeDomain(part)
		if domain != "" {
			domains = append(domains, domain)
		}
	}

	return domains
}

func readDomainFile(path string) ([]string, error) {
	// filepath.Abs cleans the path, so no ".." traversal survives here.
	resolved, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	if home != "" && !strings.HasPrefix(strings.ToLower(resolved), strings.ToLower(home)+string(os.PathSeparator)) {
		return nil, fmt.Errorf("domain file must be inside user home directory: %s", path)
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		domains = append(domains, splitDomains(line)...)
	}

	return domains, scanner.Err()
}

func normalizeDomain(value string) string {
	domain := strings.ToLower(strings.TrimSpace(value))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.Trim(domain, ".")
	if slash := strings.IndexByte(domain, '/'); slash >= 0 {
		domain = domain[:slash]
	}
	if domain == "" || strings.ContainsAny(domain, `\*"'<>()[]{}|`) {
		return ""
	}

	return domain
}

func replaceManagedDomains(path string, domains []string) error {
	var lines []string
	inManagedBlock := false

	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			switch trimmed {
			case managedStart:
				inManagedBlock = true
				continue
			case managedEnd:
				inManagedBlock = false
				continue
			}
			if !inManagedBlock {
				lines = append(lines, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(domains) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, managedStart)
		lines = append(lines, domains...)
		lines = append(lines, managedEnd)
	}

	payload := strings.Join(lines, "\n")
	if payload != "" && !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}

	return os.WriteFile(path, []byte(payload), 0o644)
}
