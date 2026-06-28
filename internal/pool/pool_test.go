package pool

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/sshclient"
	"golang.org/x/crypto/ssh"
)

// --- in-process SSH test server -------------------------------------------------

func newSSHServer(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	srvCfg := &ssh.ServerConfig{NoClientAuth: true}
	srvCfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(c, srvCfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func serveConn(c net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		c.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		nc.Reject(ssh.Prohibited, "no channels in test server")
	}
	sconn.Close()
}

func dialSSHRaw(addr string, hk ssh.PublicKey) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.FixedHostKey(hk),
		Timeout:         5 * time.Second,
	})
}

// --- test fixture: server + a tracked dialer wired into Pool.DialFunc -----------

type poolFixture struct {
	addr    string
	hk      ssh.PublicKey
	mu      sync.Mutex
	created []*sshclient.Client
	dials   int32
}

func newPoolFixture(t *testing.T) *poolFixture {
	t.Helper()
	addr, hk := newSSHServer(t)
	f := &poolFixture{addr: addr, hk: hk}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.created {
			c.Close()
		}
	})
	return f
}

func (f *poolFixture) dial(_ context.Context, _ sshclient.ConnConfig) (*sshclient.Client, error) {
	atomic.AddInt32(&f.dials, 1)
	c, err := dialSSHRaw(f.addr, f.hk)
	if err != nil {
		return nil, err
	}
	wrapped := &sshclient.Client{Client: c}
	f.mu.Lock()
	f.created = append(f.created, wrapped)
	f.mu.Unlock()
	return wrapped, nil
}

func (f *poolFixture) dialCount() int32 { return atomic.LoadInt32(&f.dials) }

// newTestPool wires the fixture's tracked dialer into Pool.DialFunc, replacing
// the former package-level seam. HealthFunc keeps the real isHealthy default
// (against the live test server) unless a test overrides it.
func newTestPool(ttl time.Duration, f *poolFixture) *Pool {
	p := New(ttl)
	p.DialFunc = f.dial
	return p
}

// blockingCloseConn wraps a net.Conn whose Close blocks until released, so a
// pooled client's Close can be parked off-lock to prove pool operations do not
// hold p.mu across connection teardown.
type blockingCloseConn struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingCloseConn) Close() error {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.Conn.Close()
}

func dialBlockingClose(t *testing.T, addr string, hk ssh.PublicKey, entered, release chan struct{}) *sshclient.Client {
	t.Helper()
	raw, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bc := &blockingCloseConn{Conn: raw, entered: entered, release: release}
	sshConn, chans, reqs, err := ssh.NewClientConn(bc, addr, &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.FixedHostKey(hk),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &sshclient.Client{Client: ssh.NewClient(sshConn, chans, reqs)}
}

// --- tests ----------------------------------------------------------------------

func TestKeyIncludesProxyJump(t *testing.T) {
	base := sshclient.ConnConfig{Host: "h", Port: "22", User: "u", IdentityFile: "k"}
	withProxy := base
	withProxy.ProxyJump = "jump"
	if Key(base) == Key(withProxy) {
		t.Error("ProxyJump must change the pool key so proxied connections are not aliased")
	}
}

func TestIsHealthy(t *testing.T) {
	addr, hk := newSSHServer(t)
	c, err := dialSSHRaw(addr, hk)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &sshclient.Client{Client: c}
	if !isHealthy(wrapped) {
		t.Error("a fresh connection should be healthy")
	}
	wrapped.Close()
	if isHealthy(wrapped) {
		t.Error("a closed connection should be unhealthy")
	}
}

func TestGetReusesHealthyConnection(t *testing.T) {
	f := newPoolFixture(t)
	p := newTestPool(time.Minute, f) // real isHealthy against the live server
	cfg := sshclient.ConnConfig{Host: "h1", Port: "22", User: "u"}

	c1, err := p.Get(context.Background(), "a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := p.Get(context.Background(), "a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Error("expected the second Get to reuse the pooled client")
	}
	if got := f.dialCount(); got != 1 {
		t.Errorf("expected exactly 1 dial, got %d", got)
	}
	if p.Len() != 1 {
		t.Errorf("expected pool len 1, got %d", p.Len())
	}
}

func TestGetReplacesUnhealthyConnection(t *testing.T) {
	f := newPoolFixture(t)
	var bad *sshclient.Client
	p := newTestPool(time.Minute, f)
	p.HealthFunc = func(c *sshclient.Client) bool { return c != bad }
	cfg := sshclient.ConnConfig{Host: "h1", Port: "22", User: "u"}

	c1, err := p.Get(context.Background(), "a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	bad = c1 // mark the pooled client as unhealthy

	c2, err := p.Get(context.Background(), "a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c2 == c1 {
		t.Error("expected a fresh client to replace the unhealthy one")
	}
	if got := f.dialCount(); got != 2 {
		t.Errorf("expected 2 dials, got %d", got)
	}
	if p.Len() != 1 {
		t.Errorf("expected pool len 1, got %d", p.Len())
	}
	if isHealthy(c1) {
		t.Error("the unhealthy client should have been closed by the pool")
	}
}

func TestGetConcurrentSameKeyDedups(t *testing.T) {
	f := newPoolFixture(t)
	p := newTestPool(time.Minute, f)
	cfg := sshclient.ConnConfig{Host: "h1", Port: "22", User: "u"}

	const n = 20
	results := make([]*sshclient.Client, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = p.Get(context.Background(), "a", cfg)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Get %d returned error: %v", i, err)
		}
	}
	if p.Len() != 1 {
		t.Errorf("expected a single pooled connection after concurrent same-key Get, got %d", p.Len())
	}
	want := results[0]
	for i, c := range results {
		if c != want {
			t.Errorf("result %d is a different client — dedup failed (redundant connection would leak)", i)
		}
	}
}

func TestGetConcurrentDifferentKeysNoDeadlock(t *testing.T) {
	f := newPoolFixture(t)
	p := newTestPool(time.Minute, f)

	const n = 15
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				cfg := sshclient.ConnConfig{Host: fmt.Sprintf("h%d", i), Port: "22", User: "u"}
				for j := 0; j < 3; j++ {
					if _, err := p.Get(context.Background(), "a", cfg); err != nil {
						t.Errorf("Get host h%d: %v", i, err)
					}
				}
			}(i)
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Get across distinct keys deadlocked")
	}
	if p.Len() != n {
		t.Errorf("expected %d pooled connections, got %d", n, p.Len())
	}
}

// TestGetReleasesLockDuringHealthCheck is the R4 regression guard: a health
// check (network I/O) must run off-lock, so an in-flight check on one key may
// not stall other pool operations.
func TestGetReleasesLockDuringHealthCheck(t *testing.T) {
	f := newPoolFixture(t)
	p := newTestPool(time.Minute, f)
	cfg := sshclient.ConnConfig{Host: "h1", Port: "22", User: "u"}

	c1, err := p.Get(context.Background(), "a", cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Make the health check for c1 block until released.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	p.HealthFunc = func(c *sshclient.Client) bool {
		if c == c1 {
			once.Do(func() { close(entered) })
			<-release
		}
		return true
	}

	getDone := make(chan struct{})
	go func() {
		p.Get(context.Background(), "a", cfg)
		close(getDone)
	}()
	<-entered // the blocked Get is now inside the health check, off-lock

	lenDone := make(chan int, 1)
	go func() { lenDone <- p.Len() }()
	select {
	case <-lenDone:
		// Good: p.mu was free while the health check was running.
	case <-time.After(2 * time.Second):
		t.Fatal("Pool.Len blocked during an in-flight health check — lock held across network I/O (R4 regression)")
	}

	close(release)
	<-getDone
}

// TestReapReleasesLockDuringClose is the R4 guard for the teardown paths: Reap
// (and, by the same discipline, Close/CloseAll) must collect expired entries
// under the lock and close them off-lock, so a slow connection teardown does not
// stall other pool operations.
func TestReapReleasesLockDuringClose(t *testing.T) {
	addr, hk := newSSHServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	blocking := dialBlockingClose(t, addr, hk, entered, release)

	p := New(time.Minute)
	p.HealthFunc = func(*sshclient.Client) bool { return true }
	p.DialFunc = func(context.Context, sshclient.ConnConfig) (*sshclient.Client, error) {
		return blocking, nil
	}
	cfg := sshclient.ConnConfig{Host: "h1", Port: "22", User: "u"}
	if _, err := p.Get(context.Background(), "a", cfg); err != nil {
		t.Fatal(err)
	}

	// Age the entry so Reap selects it for teardown.
	p.mu.Lock()
	for _, e := range p.conns {
		e.lastUsed = time.Now().Add(-time.Hour)
	}
	p.mu.Unlock()

	reapDone := make(chan struct{})
	go func() {
		p.Reap()
		close(reapDone)
	}()
	<-entered // Reap is now blocked inside client.Close, off-lock

	lenDone := make(chan int, 1)
	go func() { lenDone <- p.Len() }()
	select {
	case <-lenDone:
		// Good: p.mu was free while the reap's client.Close ran.
	case <-time.After(2 * time.Second):
		t.Fatal("Pool.Len blocked while Reap closed a connection off-lock (R4 regression)")
	}

	close(release)
	<-reapDone
}
