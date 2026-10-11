//go:build windows

package maintenance

import "testing"

func TestLooksLikeTunnel(t *testing.T) {
	tests := []struct {
		name        string
		ifType      uint32
		description string
		want        bool
	}{
		{"v2RayTun", ifTypePropVirtual, "sing-tun Tunnel", true},
		{"Ethernet 3", 6, "TAP-Windows Adapter V9", true},
		{"Ethernet", 6, "Realtek PCIe GbE Family Controller", false},
		{"Wi-Fi", 71, "Intel(R) Wi-Fi 6 AX201 160MHz", false},
		{"vEthernet (Default Switch)", 6, "Hyper-V Virtual Ethernet Adapter", false},
	}

	for _, tt := range tests {
		if got := looksLikeTunnel(tt.ifType, tt.description, tt.name); got != tt.want {
			t.Errorf("looksLikeTunnel(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
