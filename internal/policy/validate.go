package policy

import (
	"fmt"
	"regexp"

	"github.com/shayuc137/sshq/internal/appconfig"
)

type ValidationError struct {
	Alias   string `json:"alias,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.Alias != "" {
		return fmt.Sprintf("%s.%s: %s", e.Alias, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func ValidateConfig(cfg *appconfig.Config, aliasExists func(string) bool) []ValidationError {
	if cfg == nil || !cfg.Exists() {
		return nil
	}

	var errs []ValidationError
	errs = append(errs, validateRuleSet("", "policy.default", cfg.Policy.Default)...)
	for alias, host := range cfg.Policy.Hosts {
		if aliasExists != nil && !aliasExists(alias) {
			errs = append(errs, ValidationError{Alias: alias, Field: "policy.hosts", Message: "host alias not found in SSH config"})
		}
		if hostMode(host.Mode) != modeAppend && hostMode(host.Mode) != modeOverride {
			errs = append(errs, ValidationError{Alias: alias, Field: "mode", Message: "must be append or override"})
		}
		errs = append(errs, validateRuleSet(alias, "policy.hosts."+alias, host.RuleSet)...)
	}
	if cfg.Audit.MaxSize != "" {
		if _, err := appconfig.ParseSize(cfg.Audit.MaxSize); err != nil {
			errs = append(errs, ValidationError{Field: "audit.max_size", Message: err.Error()})
		}
	}
	return errs
}

func validateRuleSet(alias, prefix string, r appconfig.RuleSet) []ValidationError {
	var errs []ValidationError
	errs = append(errs, validateRegexes(alias, prefix+".command_whitelist", r.CommandWhitelist)...)
	errs = append(errs, validateRegexes(alias, prefix+".command_blacklist", r.CommandBlacklist)...)
	for _, p := range r.LocalPathWhitelist {
		if _, err := normalizeLocalPath(p); err != nil {
			errs = append(errs, ValidationError{Alias: alias, Field: prefix + ".local_path_whitelist", Message: err.Error()})
		}
	}
	for _, p := range r.RemotePathWhitelist {
		if err := remoteWhitelistValid([]string{p}); err != nil {
			errs = append(errs, ValidationError{Alias: alias, Field: prefix + ".remote_path_whitelist", Message: err.Error()})
		}
	}
	return errs
}

func validateRegexes(alias, field string, patterns []string) []ValidationError {
	var errs []ValidationError
	for _, pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			errs = append(errs, ValidationError{Alias: alias, Field: field, Message: fmt.Sprintf("invalid regex %q: %s", pattern, err)})
		}
	}
	return errs
}
