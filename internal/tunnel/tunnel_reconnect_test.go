package tunnel

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastPolicy keeps the backoff tiny so retry-path tests stay quick.
var fastPolicy = backoffPolicy{base: time.Millisecond, max: 5 * time.Millisecond, maxFails: 100}

// --- test doubles ---------------------------------------------------------------

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake" }

// fakeConn satisfies net.Conn; only Close is exercised by acceptLoop.
type fakeConn struct{ net.Conn }

func (fakeConn) Close() error { return nil }

type accept struct {
	conn net.Conn
	err  error
}

type fnListener struct {
	fn     func() (net.Conn, error)
	closed chan struct{}
	once   sync.Once
}

func (l *fnListener) Accept() (net.Conn, error) { return l.fn() }
func (l *fnListener) Close() error              { l.once.Do(func() { close(l.closed) }); return nil }
func (l *fnListener) Addr() net.Addr            { return fakeAddr{} }

func newFnListener(fn func() (net.Conn, error)) *fnListener {
	return &fnListener{fn: fn, closed: make(chan struct{})}
}

// newBlockingListener blocks in Accept until Close is called, then reports a
// closed listener (mirrors a real net.Listener shut down via ctx cancel).
func newBlockingListener() *fnListener {
	l := &fnListener{closed: make(chan struct{})}
	l.fn = func() (net.Conn, error) {
		<-l.closed
		return nil, net.ErrClosed
	}
	return l
}

// newSeqListener returns the queued accepts in order, then blocks until Close.
func newSeqListener(items ...accept) *fnListener {
	l := &fnListener{closed: make(chan struct{})}
	var mu sync.Mutex
	pos := 0
	l.fn = func() (net.Conn, error) {
		mu.Lock()
		if pos < len(items) {
			it := items[pos]
			pos++
			mu.Unlock()
			return it.conn, it.err
		}
		mu.Unlock()
		<-l.closed
		return nil, net.ErrClosed
	}
	return l
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// --- tests ----------------------------------------------------------------------

func TestAcceptLoopExitsOnListenerClose(t *testing.T) {
	ln := newBlockingListener()
	tun := &Tunnel{Config: Config{Alias: "h"}, cancel: func() {}}
	done := make(chan struct{})
	go func() {
		tun.acceptLoop(context.Background(), ln, fastPolicy, nil, func(net.Conn) {})
		close(done)
	}()

	ln.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acceptLoop did not exit after the listener was closed")
	}
}

func TestAcceptLoopExitsOnContextCancel(t *testing.T) {
	var handled int32
	ln := newFnListener(func() (net.Conn, error) { return nil, errors.New("boom") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the loop runs

	tun := &Tunnel{Config: Config{Alias: "h"}, cancel: func() {}}
	done := make(chan struct{})
	go func() {
		tun.acceptLoop(ctx, ln, fastPolicy, nil, func(net.Conn) { atomic.AddInt32(&handled, 1) })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acceptLoop ignored a cancelled context and kept retrying")
	}
	if atomic.LoadInt32(&handled) != 0 {
		t.Error("handler ran despite cancellation")
	}
}

func TestAcceptLoopRetriesThenRecovers(t *testing.T) {
	ln := newSeqListener(
		accept{nil, errors.New("temporary")}, // transient failure
		accept{fakeConn{}, nil},              // then a real connection
	)
	tun := &Tunnel{Config: Config{Alias: "h"}, cancel: func() {}}
	var handled int32
	done := make(chan struct{})
	go func() {
		tun.acceptLoop(context.Background(), ln, fastPolicy, nil, func(net.Conn) { atomic.AddInt32(&handled, 1) })
		close(done)
	}()

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&handled) == 1 })
	ln.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acceptLoop did not exit after recovering and being closed")
	}
	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Errorf("expected exactly 1 handled connection, got %d", got)
	}
}

func TestAcceptLoopGivesUpAfterMaxFails(t *testing.T) {
	ln := newFnListener(func() (net.Conn, error) { return nil, errors.New("dead listener") })
	var cancelled int32
	tun := &Tunnel{Config: Config{Alias: "h"}, cancel: func() { atomic.AddInt32(&cancelled, 1) }}

	var mu sync.Mutex
	var msgs []string
	infoFn := func(s string) {
		mu.Lock()
		msgs = append(msgs, s)
		mu.Unlock()
	}
	policy := backoffPolicy{base: time.Millisecond, max: 2 * time.Millisecond, maxFails: 3}

	done := make(chan struct{})
	go func() {
		tun.acceptLoop(context.Background(), ln, policy, infoFn, func(net.Conn) {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLoop did not give up after repeated failures")
	}
	if atomic.LoadInt32(&cancelled) != 1 {
		t.Errorf("expected the tunnel to cancel itself on give-up, cancelled=%d", atomic.LoadInt32(&cancelled))
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "giving up") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a give-up notification via infoFn, got %v", msgs)
	}
}

func TestAcceptLoopCancelDuringBackoff(t *testing.T) {
	var calls int32
	ln := newFnListener(func() (net.Conn, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("temporary")
	})
	tun := &Tunnel{Config: Config{Alias: "h"}, cancel: func() {}}
	ctx, cancel := context.WithCancel(context.Background())
	policy := backoffPolicy{base: 200 * time.Millisecond, max: time.Second, maxFails: 100}

	done := make(chan struct{})
	go func() {
		tun.acceptLoop(ctx, ln, policy, nil, func(net.Conn) {})
		close(done)
	}()

	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&calls) >= 1 })
	cancel() // cancel while the loop is sleeping in backoff
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("acceptLoop did not exit when cancelled during backoff")
	}
	if atomic.LoadInt32(&calls) >= int32(policy.maxFails) {
		t.Error("acceptLoop kept retrying to max instead of exiting on cancel")
	}
}
