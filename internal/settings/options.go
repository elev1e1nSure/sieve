package settings

import (
	"context"
	"strings"
)

const (
	IPSetUnchanged = ""
	IPSetLoaded    = "loaded"
	IPSetNone      = "none"
	IPSetAny       = "any"

	GameOff = "off"
	GameAll = "all"
	GameTCP = "tcp"
	GameUDP = "udp"
)

type RuntimeOptions struct {
	TestTimeout int      `json:"test_timeout"`
	NoCache     bool     `json:"no_cache"`
	NoAddPath   bool     `json:"no_add_path"`
	IPSetMode   string   `json:"ipset_mode,omitempty"`
	Domains     []string `json:"domains,omitempty"`
	DomainFiles []string `json:"domain_files,omitempty"`
	GameMode    string   `json:"game_mode"`
}

type GamePorts struct {
	TCP string
	UDP string
}

func (o RuntimeOptions) Game() GamePorts {
	switch strings.ToLower(strings.TrimSpace(o.GameMode)) {
	case GameAll:
		return GamePorts{TCP: "1024-65535", UDP: "1024-65535"}
	case GameTCP:
		return GamePorts{TCP: "1024-65535", UDP: "12"}
	case GameUDP:
		return GamePorts{TCP: "12", UDP: "1024-65535"}
	default:
		return GamePorts{TCP: "12", UDP: "12"}
	}
}

func (o RuntimeOptions) HasListChanges() bool {
	return strings.TrimSpace(o.IPSetMode) != "" ||
		len(o.Domains) > 0 ||
		len(o.DomainFiles) > 0
}

func (o RuntimeOptions) Normalized() RuntimeOptions {
	if o.TestTimeout <= 0 {
		o.TestTimeout = 5
	}
	if strings.TrimSpace(o.GameMode) == "" {
		o.GameMode = GameOff
	}

	return o
}

func (o RuntimeOptions) Apply(ctx context.Context, listsDir string) ([]string, error) {
	report, err := ApplyLists(ctx, listsDir, o)
	if err != nil {
		return nil, err
	}

	notices := make([]string, 0, len(report.Items))
	for _, item := range report.Items {
		notices = append(notices, item.Kind+": "+item.Message)
	}

	return notices, nil
}
