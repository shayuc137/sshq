package config

import (
	"fmt"
	"sort"
	"strings"
)

func RenderListCompact(hosts []Host) string {
	var b strings.Builder
	for _, h := range hosts {
		auth := "key"
		if h.IdentityFile == "" {
			auth = "agent"
		}
		line := fmt.Sprintf("%s %s@%s:%s auth=%s", h.Alias, h.User, h.HostName, h.Port, auth)
		if desc := h.Metadata["description"]; desc != "" {
			line += " desc=" + desc
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "total=%d\n", len(hosts))
	return b.String()
}

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

func RenderInfoCompact(h Host) string {
	auth := "key"
	if h.IdentityFile == "" {
		auth = "agent"
	}
	parts := []string{
		fmt.Sprintf("%s %s@%s:%s auth=%s", h.Alias, h.User, h.HostName, h.Port, auth),
	}
	if h.IdentityFile != "" {
		parts = append(parts, "identity="+h.IdentityFile)
	}
	if h.ProxyJump != "" {
		parts = append(parts, "proxy="+h.ProxyJump)
	}
	keys := make([]string, 0, len(h.Metadata))
	for k := range h.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+h.Metadata[k])
	}
	return strings.Join(parts, " ") + "\n"
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
