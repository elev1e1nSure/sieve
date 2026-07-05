package settings

import "testing"

func TestNormalizedDefaults(t *testing.T) {
	opts := RuntimeOptions{}.Normalized()
	if opts.TestTimeout != 5 {
		t.Errorf("TestTimeout = %d, want 5", opts.TestTimeout)
	}
	if opts.GameMode != GameOff {
		t.Errorf("GameMode = %q, want %q", opts.GameMode, GameOff)
	}

	// Explicit values survive normalization.
	opts = RuntimeOptions{TestTimeout: 30, GameMode: GameTCP}.Normalized()
	if opts.TestTimeout != 30 || opts.GameMode != GameTCP {
		t.Errorf("normalized = %+v, explicit values lost", opts)
	}
}

func TestValidate(t *testing.T) {
	valid := RuntimeOptions{TestTimeout: 5, IPSetMode: IPSetLoaded, GameMode: GameAll}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid options rejected: %v", err)
	}

	cases := []RuntimeOptions{
		{TestTimeout: 5, IPSetMode: "bogus"},
		{TestTimeout: 5, GameMode: "bogus"},
		{TestTimeout: 0},
		{TestTimeout: -1},
	}
	for _, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error", c)
		}
	}
}

func TestGamePorts(t *testing.T) {
	cases := []struct {
		mode     string
		tcp, udp string
	}{
		{GameOff, "12", "12"},
		{"", "12", "12"},
		{GameAll, "1024-65535", "1024-65535"},
		{GameTCP, "1024-65535", "12"},
		{GameUDP, "12", "1024-65535"},
		{" TCP ", "1024-65535", "12"}, // case/space insensitive
	}

	for _, c := range cases {
		ports := RuntimeOptions{GameMode: c.mode}.Game()
		if ports.TCP != c.tcp || ports.UDP != c.udp {
			t.Errorf("Game(%q) = %+v, want tcp=%s udp=%s", c.mode, ports, c.tcp, c.udp)
		}
	}
}
