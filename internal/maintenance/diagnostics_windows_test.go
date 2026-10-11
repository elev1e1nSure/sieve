//go:build windows

package maintenance

import (
	"fmt"
	"testing"
)

const netshGlobalTemplate = "Querying active state...\r\n\r\n" +
	"TCP Global Parameters\r\n" +
	"----------------------------------------------\r\n" +
	"Receive-Side Scaling State          : enabled \r\n" +
	"Receive Window Auto-Tuning Level    : normal \r\n" +
	"RFC 1323 Timestamps                 : %s \r\n" +
	"Receive Segment Coalescing State    : enabled \r\n"

func TestTimestampsRowEnabled(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"enabled", fmt.Sprintf(netshGlobalTemplate, "enabled"), true},
		{"disabled while other rows are enabled", fmt.Sprintf(netshGlobalTemplate, "disabled"), false},
		{"legacy label", "Timestamps : enabled\r\n", true},
		{"localized label", "Временные метки RFC 1323 : enabled\r\n", true},
		{"row missing", "Receive-Side Scaling State : enabled\r\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timestampsRowEnabled(tt.output); got != tt.want {
				t.Fatalf("timestampsRowEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
