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
	IPSetMode         string
	UpdateIPSet       bool
	Domains           []string
	DomainFiles       []string
	GameMode          string
	ClearDiscordCache bool
	Diagnostics       bool
	AutoFix           bool
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
	return o.UpdateIPSet ||
		strings.TrimSpace(o.IPSetMode) != "" ||
		len(o.Domains) > 0 ||
		len(o.DomainFiles) > 0
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
