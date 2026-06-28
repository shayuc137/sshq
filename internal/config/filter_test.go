package config

import "testing"

func TestMatchTag(t *testing.T) {
	tests := []struct {
		tags string
		want string
		ok   bool
	}{
		{"prod,web", "prod", true},
		{"prod,web", "web", true},
		{"prod,web", "dev", false},
		{"prod", "prod", true},
		{"", "prod", false},
		{"prod,web,api", "api", true},
	}
	for _, tt := range tests {
		if got := matchTag(tt.tags, tt.want); got != tt.ok {
			t.Errorf("matchTag(%q, %q) = %v, want %v", tt.tags, tt.want, got, tt.ok)
		}
	}
}

func TestStoreFilter(t *testing.T) {
	raw := []byte(`
# sshq:tags=prod,web
# sshq:env=production
Host web1
    HostName 10.0.0.1
    User root

# sshq:tags=prod,db
# sshq:env=production
Host db1
    HostName 10.0.0.2
    User root

# sshq:tags=dev,web
# sshq:env=staging
Host dev1
    HostName 10.0.0.3
    User root
`)
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		filter  Filter
		want    int
		aliases []string
	}{
		{"all", Filter{All: true}, 3, nil},
		{"tag prod", Filter{Tag: "prod"}, 2, []string{"db1", "web1"}},
		{"tag web", Filter{Tag: "web"}, 2, []string{"dev1", "web1"}},
		{"tag db", Filter{Tag: "db"}, 1, []string{"db1"}},
		{"env production", Filter{Env: "production"}, 2, []string{"db1", "web1"}},
		{"env staging", Filter{Env: "staging"}, 1, []string{"dev1"}},
		{"tag prod AND env production", Filter{Tag: "prod", Env: "production"}, 2, nil},
		{"tag web AND env staging", Filter{Tag: "web", Env: "staging"}, 1, nil},
		{"no match", Filter{Tag: "nonexistent"}, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := store.Filter(tt.filter)
			if len(got) != tt.want {
				t.Errorf("Filter(%+v) returned %d hosts, want %d", tt.filter, len(got), tt.want)
			}
		})
	}
}
