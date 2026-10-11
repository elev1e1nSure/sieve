//go:build windows

package maintenance

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ifTypePropVirtual is the interface type of wintun adapters, which
// WireGuard, sing-box (v2RayTun, Hiddify, ...) and similar VPNs use.
const ifTypePropVirtual = 53

// tunnelKeywords catch VPN adapters that present as plain Ethernet, such as
// TAP-based OpenVPN.
var tunnelKeywords = []string{"vpn", "tap-windows", "wireguard", "wintun", "tun"}

// TunnelRoute names the adapter that carries traffic to the internet when it
// looks like a VPN tunnel. Such traffic reaches the ISP's DPI wrapped by the
// VPN, so sifting would judge the VPN instead of the strategy.
func TunnelRoute() (string, error) {
	var index uint32
	// Any public address works; only the chosen interface matters.
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}, &index); err != nil {
		return "", fmt.Errorf("find internet route: %w", err)
	}

	adapter, err := adapterByIndex(index)
	if err != nil || adapter == nil {
		return "", err
	}
	if !looksLikeTunnel(adapter.ifType, adapter.description, adapter.name) {
		return "", nil
	}
	return adapter.name, nil
}

func looksLikeTunnel(ifType uint32, description, name string) bool {
	if ifType == ifTypePropVirtual {
		return true
	}
	text := strings.ToLower(description + " " + name)
	for _, keyword := range tunnelKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

type adapter struct {
	name, description string
	ifType            uint32
}

func adapterByIndex(index uint32) (*adapter, error) {
	const flags = windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_DNS_SERVER
	size := uint32(16 << 10)
	// The adapter list can grow between the sizing call and the real one.
	for range 3 {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list network adapters: %w", err)
		}

		for entry := first; entry != nil; entry = entry.Next {
			if entry.IfIndex == index {
				return &adapter{
					name:        windows.UTF16PtrToString(entry.FriendlyName),
					description: windows.UTF16PtrToString(entry.Description),
					ifType:      entry.IfType,
				}, nil
			}
		}
		return nil, nil
	}
	return nil, errors.New("list network adapters: adapter list kept growing")
}
