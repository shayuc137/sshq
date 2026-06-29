package policy

import (
	"fmt"
	"regexp"
)

func (c *Checker) CheckCommand(alias, command string) Decision {
	effective, err := c.effectivePolicy(alias)
	if err != nil {
		return deny(alias, KindCommand, ReasonConfigError, err.Error(), command)
	}
	if !effective.Enabled {
		return allow(alias, KindCommand, command)
	}

	if len(effective.CommandWhitelist) > 0 {
		matched, pattern, err := matchAnyRegex(effective.CommandWhitelist, command)
		if err != nil {
			return deny(alias, KindCommand, ReasonConfigError, err.Error(), command)
		}
		if !matched {
			if c.grants != nil && c.grants.MatchCommand(alias, command) {
				return c.checkCommandBlacklist(alias, command, effective.CommandBlacklist)
			}
			return deny(alias, KindCommand, ReasonWhitelistMiss, pattern, command)
		}
	}

	return c.checkCommandBlacklist(alias, command, effective.CommandBlacklist)
}

func (c *Checker) checkCommandBlacklist(alias, command string, patterns []string) Decision {
	matched, pattern, err := matchAnyRegex(patterns, command)
	if err != nil {
		return deny(alias, KindCommand, ReasonConfigError, err.Error(), command)
	}
	if matched {
		return deny(alias, KindCommand, ReasonBlacklistMatch, pattern, command)
	}
	return allow(alias, KindCommand, command)
}

func matchAnyRegex(patterns []string, input string) (bool, string, error) {
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, pattern, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		if re.MatchString(input) {
			return true, pattern, nil
		}
	}
	return false, "", nil
}
