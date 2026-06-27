package config

import "strings"

type Filter struct {
	Tag string
	Env string
	All bool
}

func (s *Store) Filter(f Filter) []Host {
	if f.All {
		return s.List()
	}

	var result []Host
	for _, h := range s.hosts {
		if f.Tag != "" && !matchTag(h.Metadata["tags"], f.Tag) {
			continue
		}
		if f.Env != "" && h.Metadata["env"] != f.Env {
			continue
		}
		result = append(result, h)
	}
	return result
}

func matchTag(tags, want string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == want {
			return true
		}
	}
	return false
}
