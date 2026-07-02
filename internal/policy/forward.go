package policy

import (
	"fmt"
	"strconv"
	"strings"
)

func (c *Checker) CheckLocalForward(alias, target string) Decision {
	return c.checkForward(alias, target, KindLocalForward, func(e EffectiveRuleSet) []string {
		return e.LocalForwardWhitelist
	})
}

func (c *Checker) CheckRemoteForward(alias, target string) Decision {
	return c.checkForward(alias, target, KindRemoteForward, func(e EffectiveRuleSet) []string {
		return e.RemoteForwardWhitelist
	})
}

func (c *Checker) checkForward(alias, target, kind string, whitelist func(EffectiveRuleSet) []string) Decision {
	effective, err := c.effectivePolicy(alias)
	if err != nil {
		return deny(alias, kind, ReasonConfigError, err.Error(), target)
	}
	if !effective.Enabled {
		return allow(alias, kind, target)
	}

	wl := whitelist(effective)
	if len(wl) == 0 {
		return allow(alias, kind, target)
	}

	allowed, matchedEntry, err := forwardAllowed(target, wl)
	if err != nil {
		return deny(alias, kind, ReasonConfigError, err.Error(), target)
	}
	if allowed {
		return allow(alias, kind, target)
	}

	grantMatch := false
	if c.grants != nil {
		switch kind {
		case KindLocalForward:
			grantMatch = c.grants.MatchLocalForward(alias, target)
		case KindRemoteForward:
			grantMatch = c.grants.MatchRemoteForward(alias, target)
		}
	}
	if grantMatch {
		return allow(alias, kind, target)
	}
	return deny(alias, kind, ReasonForwardDenied, matchedEntry, target)
}

func forwardAllowed(target string, whitelist []string) (bool, string, error) {
	tHost, tPort, err := parseForwardTarget(target)
	if err != nil {
		return false, "", fmt.Errorf("invalid forward target %q: %w", target, err)
	}
	for _, entry := range whitelist {
		eHost, ePortSpec, err := parseForwardEntry(entry)
		if err != nil {
			return false, entry, fmt.Errorf("invalid whitelist entry %q: %w", entry, err)
		}
		if hostMatches(tHost, eHost) && portMatches(tPort, ePortSpec) {
			return true, entry, nil
		}
	}
	return false, "", nil
}

func parseForwardTarget(target string) (host string, port int, err error) {
	host, portStr, err := splitHostPort(target)
	if err != nil {
		return "", 0, err
	}
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q", portStr)
	}
	if port < 0 || port > 65535 {
		return "", 0, fmt.Errorf("port %d out of range", port)
	}
	return host, port, nil
}

func parseForwardEntry(entry string) (host, portSpec string, err error) {
	host, portSpec, err = splitHostPort(entry)
	if err != nil {
		return "", "", err
	}
	if portSpec == "*" {
		return host, portSpec, nil
	}
	if strings.Contains(portSpec, "-") {
		parts := strings.SplitN(portSpec, "-", 2)
		lo, err := strconv.Atoi(parts[0])
		if err != nil {
			return "", "", fmt.Errorf("invalid range start %q", parts[0])
		}
		hi, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", "", fmt.Errorf("invalid range end %q", parts[1])
		}
		if lo < 0 || hi > 65535 || lo > hi {
			return "", "", fmt.Errorf("invalid port range %d-%d", lo, hi)
		}
		return host, portSpec, nil
	}
	p, err := strconv.Atoi(portSpec)
	if err != nil {
		return "", "", fmt.Errorf("invalid port %q", portSpec)
	}
	if p < 0 || p > 65535 {
		return "", "", fmt.Errorf("port %d out of range", p)
	}
	return host, portSpec, nil
}

func splitHostPort(s string) (host, port string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty host:port")
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port in %q", s)
	}
	return s[:i], s[i+1:], nil
}

func hostMatches(target, pattern string) bool {
	if pattern == "*" {
		return true
	}
	return strings.EqualFold(target, pattern)
}

func portMatches(port int, spec string) bool {
	if spec == "*" {
		return true
	}
	if strings.Contains(spec, "-") {
		parts := strings.SplitN(spec, "-", 2)
		lo, _ := strconv.Atoi(parts[0])
		hi, _ := strconv.Atoi(parts[1])
		return port >= lo && port <= hi
	}
	exact, _ := strconv.Atoi(spec)
	return port == exact
}
