package policy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

const MaxGrantTTL = time.Hour

type Grant struct {
	ID        string    `json:"id"`
	Alias     string    `json:"alias"`
	Kind      string    `json:"kind"`
	Pattern   string    `json:"pattern"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type GrantManager struct {
	mu     sync.Mutex
	grants map[string]*Grant
	now    func() time.Time
}

func NewGrantManager() *GrantManager {
	return newGrantManagerWithClock(time.Now)
}

func newGrantManagerWithClock(now func() time.Time) *GrantManager {
	return &GrantManager{
		grants: make(map[string]*Grant),
		now:    now,
	}
}

func (m *GrantManager) Add(alias, kind, pattern string, ttl time.Duration) (*Grant, error) {
	if m == nil {
		return nil, fmt.Errorf("grant manager unavailable")
	}
	if err := validateGrant(kind, pattern, ttl); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()

	now := m.now()
	g := &Grant{
		ID:        newGrantID(),
		Alias:     alias,
		Kind:      kind,
		Pattern:   pattern,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	m.grants[g.ID] = g
	return cloneGrant(g), nil
}

func (m *GrantManager) Revoke(id string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()
	if _, ok := m.grants[id]; !ok {
		return false
	}
	delete(m.grants, id)
	return true
}

func (m *GrantManager) RevokeByAlias(alias string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()

	removed := 0
	for id, g := range m.grants {
		if g.Alias == alias {
			delete(m.grants, id)
			removed++
		}
	}
	return removed
}

func (m *GrantManager) List(alias string) []*Grant {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()

	out := make([]*Grant, 0, len(m.grants))
	for _, g := range m.grants {
		if alias == "" || g.Alias == alias {
			out = append(out, cloneGrant(g))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Alias == out[j].Alias {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].Alias < out[j].Alias
	})
	return out
}

func (m *GrantManager) MatchCommand(alias, command string) bool {
	return m.match(alias, KindCommand, func(pattern string) bool {
		matched, _, err := matchAnyRegex([]string{pattern}, command)
		return err == nil && matched
	})
}

func (m *GrantManager) MatchLocalPath(alias, localPath string) bool {
	return m.match(alias, KindLocalPath, func(pattern string) bool {
		matched, _, err := localPathAllowed(localPath, []string{pattern})
		return err == nil && matched
	})
}

func (m *GrantManager) MatchRemotePath(alias, remotePath string) bool {
	return m.match(alias, KindRemotePath, func(pattern string) bool {
		matched, _, err := remotePathAllowed(remotePath, []string{pattern})
		return err == nil && matched
	})
}

func (m *GrantManager) Purge() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()
}

func (m *GrantManager) match(alias, kind string, matchPattern func(string) bool) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()

	for _, g := range m.grants {
		if g.Alias == alias && g.Kind == kind && matchPattern(g.Pattern) {
			return true
		}
	}
	return false
}

func (m *GrantManager) purgeLocked() {
	now := m.now()
	for id, g := range m.grants {
		if !g.ExpiresAt.After(now) {
			delete(m.grants, id)
		}
	}
}

func validateGrant(kind, pattern string, ttl time.Duration) error {
	switch kind {
	case KindCommand:
		if _, _, err := matchAnyRegex([]string{pattern}, ""); err != nil {
			return err
		}
	case KindLocalPath:
		if _, err := normalizeLocalPath(pattern); err != nil {
			return fmt.Errorf("invalid local path grant %q: %w", pattern, err)
		}
	case KindRemotePath:
		if err := remoteWhitelistValid([]string{pattern}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid grant kind %q", kind)
	}
	if ttl <= 0 {
		return fmt.Errorf("ttl must be positive")
	}
	if ttl > MaxGrantTTL {
		return fmt.Errorf("ttl exceeds maximum %s", MaxGrantTTL)
	}
	return nil
}

func newGrantID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("grant-%d", time.Now().UnixNano())
	}
	return "grant-" + hex.EncodeToString(b[:])
}

func cloneGrant(g *Grant) *Grant {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}
