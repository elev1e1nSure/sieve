package ui

import (
	"reflect"
	"strings"
	"testing"
)

func TestTail(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if got := tail(lines, 2); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("tail = %v", got)
	}
	if got := tail(lines, 5); !reflect.DeepEqual(got, lines) {
		t.Fatalf("tail with large limit = %v", got)
	}
}

func TestFriendlyLogLine(t *testing.T) {
	cases := []struct {
		in      string
		match   string // substring the friendly event must contain
		handled bool
	}{
		{"github version v71", "v71", true},
		{"we have 12 user defined desync profile(s)", "desync profiles ready", true},
		{"Loaded 42 hosts from C:\\lists\\list-general.txt", "list-general.txt", true},
		{"Loaded 100 ip/subnets from C:\\lists\\ipset-all.txt", "ipset-all.txt", true},
		{"windivert initialized. capture is started.", "traffic capture started", true},
		{"something failed badly", "something failed badly", true},
		{"random noise line", "", false},
	}

	for _, c := range cases {
		got, handled := friendlyLogLine(c.in)
		if handled != c.handled {
			t.Errorf("friendlyLogLine(%q) handled = %v, want %v", c.in, handled, c.handled)
			continue
		}
		if handled && !strings.Contains(got, c.match) {
			t.Errorf("friendlyLogLine(%q) = %q, want substring %q", c.in, got, c.match)
		}
	}
}

func TestFormatFriendlyLogsDedupes(t *testing.T) {
	lines := []string{
		"windivert initialized. capture is started.",
		"windivert initialized. capture is started.",
		"noise",
	}
	events := formatFriendlyLogs(lines)
	if len(events) != 1 {
		t.Fatalf("events = %v, want single deduped entry", events)
	}
}

func TestAppendRecent(t *testing.T) {
	var lines []string
	for _, line := range []string{"one", " ", "two", "three", "four"} {
		lines = appendRecent(lines, line, 3)
	}
	want := []string{"two", "three", "four"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("appendRecent = %v, want %v", lines, want)
	}
}
