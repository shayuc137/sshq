package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// HostList wraps []Host as a Renderable for output.Writer.Render in non-JSON mode.
type HostList []Host

func (hl HostList) Pretty() string { return RenderListPretty([]Host(hl)) }

// MarshalJSON renders an empty or nil HostList as [] rather than null, keeping the
// JSON list contract stable for agents that iterate over the array.
func (hl HostList) MarshalJSON() ([]byte, error) {
	if len(hl) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal([]Host(hl))
}

// HostDetail wraps a single Host as a Renderable for output.Writer.Render in non-JSON mode.
type HostDetail Host

func (hd HostDetail) Pretty() string { return RenderInfoPretty(Host(hd)) }

func RenderListPretty(hosts []Host) string {
	if len(hosts) == 0 {
		return "No hosts configured.\n"
	}

	aliasW, hostW := 5, 8
	for _, h := range hosts {
		if len(h.Alias) > aliasW {
			aliasW = len(h.Alias)
		}
		if len(h.HostName) > hostW {
			hostW = len(h.HostName)
		}
	}

	var b strings.Builder
	for _, h := range hosts {
		fmt.Fprintf(&b, "%-*s | %-*s | %s@%s:%s\n",
			aliasW, h.Alias,
			hostW, h.HostName,
			h.User, h.HostName, h.Port)
	}
	return b.String()
}

func RenderInfoPretty(h Host) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Alias:        %s\n", h.Alias)
	fmt.Fprintf(&b, "HostName:     %s\n", h.HostName)
	fmt.Fprintf(&b, "User:         %s\n", h.User)
	fmt.Fprintf(&b, "Port:         %s\n", h.Port)
	fmt.Fprintf(&b, "IdentityFile: %s\n", valueOr(h.IdentityFile, "(none)"))
	fmt.Fprintf(&b, "ProxyJump:    %s\n", valueOr(h.ProxyJump, "(none)"))

	if len(h.Metadata) > 0 {
		b.WriteString("---\n")
		mkeys := make([]string, 0, len(h.Metadata))
		for k := range h.Metadata {
			mkeys = append(mkeys, k)
		}
		sort.Strings(mkeys)
		for _, k := range mkeys {
			fmt.Fprintf(&b, "%-13s %s\n", k+":", h.Metadata[k])
		}
	}
	return b.String()
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
