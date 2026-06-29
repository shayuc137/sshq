package policy

import "github.com/shayuc137/sshq/internal/appconfig"

const (
	KindCommand    = "command"
	KindLocalPath  = "local-path"
	KindRemotePath = "remote-path"

	ReasonWhitelistMiss    = "whitelist_miss"
	ReasonBlacklistMatch   = "blacklist_match"
	ReasonLocalPathDenied  = "local_path_denied"
	ReasonRemotePathDenied = "remote_path_denied"
	ReasonConfigError      = "config_error"
)

type Checker struct {
	config *appconfig.Config
	grants *GrantManager
}

type EffectiveRuleSet struct {
	Enabled             bool     `json:"enabled"`
	CommandWhitelist    []string `json:"command_whitelist"`
	CommandBlacklist    []string `json:"command_blacklist"`
	LocalPathWhitelist  []string `json:"local_path_whitelist"`
	RemotePathWhitelist []string `json:"remote_path_whitelist"`
}

type Decision struct {
	Allowed bool   `json:"allowed"`
	Alias   string `json:"alias,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Input   string `json:"input,omitempty"`
}

func NewChecker(cfg *appconfig.Config, grants *GrantManager) *Checker {
	return &Checker{config: cfg, grants: grants}
}

func (c *Checker) Reload(cfg *appconfig.Config) {
	c.config = cfg
}

func (c *Checker) EffectivePolicy(alias string) EffectiveRuleSet {
	effective, _ := c.effectivePolicy(alias)
	return effective
}

func allow(alias, kind, input string) Decision {
	return Decision{Allowed: true, Alias: alias, Kind: kind, Input: input}
}

func deny(alias, kind, reason, pattern, input string) Decision {
	return Decision{
		Allowed: false,
		Alias:   alias,
		Kind:    kind,
		Reason:  reason,
		Pattern: pattern,
		Input:   input,
	}
}

func (d Decision) Err() error {
	if d.Allowed {
		return nil
	}
	return &BlockedError{
		Alias:   d.Alias,
		Kind:    d.Kind,
		Reason:  d.Reason,
		Pattern: d.Pattern,
		Input:   d.Input,
	}
}
