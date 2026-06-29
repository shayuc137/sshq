package policy

import (
	"fmt"

	"github.com/shayuc137/sshq/internal/appconfig"
)

const (
	modeAppend   = "append"
	modeOverride = "override"
)

func (c *Checker) effectivePolicy(alias string) (EffectiveRuleSet, error) {
	if c == nil || c.config == nil || !c.config.Exists() {
		return EffectiveRuleSet{}, nil
	}

	base := c.config.Policy.Default
	if base.Enabled != nil && !*base.Enabled {
		return EffectiveRuleSet{}, nil
	}

	effective := fromRuleSet(base, true)
	host, ok := c.config.Policy.Hosts[alias]
	if !ok {
		return effective, nil
	}

	if host.Enabled != nil && !*host.Enabled {
		return EffectiveRuleSet{}, nil
	}

	switch hostMode(host.Mode) {
	case modeOverride:
		return fromRuleSet(host.RuleSet, true), nil
	case modeAppend:
		return appendRuleSet(effective, host.RuleSet), nil
	default:
		return EffectiveRuleSet{}, fmt.Errorf("invalid policy mode %q for host %q", host.Mode, alias)
	}
}

func fromRuleSet(r appconfig.RuleSet, enabled bool) EffectiveRuleSet {
	return EffectiveRuleSet{
		Enabled:             enabled,
		CommandWhitelist:    append([]string(nil), r.CommandWhitelist...),
		CommandBlacklist:    append([]string(nil), r.CommandBlacklist...),
		LocalPathWhitelist:  append([]string(nil), r.LocalPathWhitelist...),
		RemotePathWhitelist: append([]string(nil), r.RemotePathWhitelist...),
	}
}

func appendRuleSet(base EffectiveRuleSet, host appconfig.RuleSet) EffectiveRuleSet {
	base.CommandWhitelist = appendUnique(base.CommandWhitelist, host.CommandWhitelist)
	base.CommandBlacklist = appendUnique(base.CommandBlacklist, host.CommandBlacklist)
	base.LocalPathWhitelist = appendUnique(base.LocalPathWhitelist, host.LocalPathWhitelist)
	base.RemotePathWhitelist = appendUnique(base.RemotePathWhitelist, host.RemotePathWhitelist)
	return base
}

func appendUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, v := range base {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, v := range extra {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func hostMode(mode string) string {
	if mode == "" {
		return modeAppend
	}
	return mode
}
