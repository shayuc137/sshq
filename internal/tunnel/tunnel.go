package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

type Direction string

const (
	Local  Direction = "local"
	Remote Direction = "remote"
)

type Config struct {
	Direction  Direction
	Alias      string
	LocalAddr  string
	RemoteAddr string
}

type Tunnel struct {
	ID         string
	Config     Config
	cancel     context.CancelFunc
	listener   net.Listener
	activeConn int64
	mu         sync.Mutex
	err        error
}

type Registry struct {
	mu      sync.Mutex
	tunnels map[string]*Tunnel
	nextID  int
}

func NewRegistry() *Registry {
	return &Registry{tunnels: make(map[string]*Tunnel)}
}

func (r *Registry) Add(t *Tunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	t.ID = fmt.Sprintf("tun-%d", r.nextID)
	r.tunnels[t.ID] = t
}

func (r *Registry) Remove(id string) (*Tunnel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tunnels[id]
	if ok {
		delete(r.tunnels, id)
	}
	return t, ok
}

func (r *Registry) Get(id string) (*Tunnel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tunnels[id]
	return t, ok
}

type TunnelInfo struct {
	ID         string `json:"id"`
	Direction  string `json:"direction"`
	Alias      string `json:"alias"`
	LocalAddr  string `json:"local_addr"`
	RemoteAddr string `json:"remote_addr"`
	ActiveConn int64  `json:"active_connections"`
}

func (r *Registry) List() []TunnelInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]TunnelInfo, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		list = append(list, TunnelInfo{
			ID:         t.ID,
			Direction:  string(t.Config.Direction),
			Alias:      t.Config.Alias,
			LocalAddr:  t.Config.LocalAddr,
			RemoteAddr: t.Config.RemoteAddr,
			ActiveConn: atomic.LoadInt64(&t.activeConn),
		})
	}
	return list
}

func (r *Registry) StopAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.tunnels))
	for id := range r.tunnels {
		ids = append(ids, id)
	}
	r.mu.Unlock()

	for _, id := range ids {
		r.Stop(id)
	}
}

func (r *Registry) Stop(id string) error {
	t, ok := r.Remove(id)
	if !ok {
		return fmt.Errorf("tunnel %q not found", id)
	}
	t.cancel()
	if t.listener != nil {
		t.listener.Close()
	}
	return nil
}

func StartLocal(ctx context.Context, client *ssh.Client, cfg Config, infoFn func(string)) (*Tunnel, error) {
	ln, err := net.Listen("tcp", cfg.LocalAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", cfg.LocalAddr, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	t := &Tunnel{Config: cfg, cancel: cancel, listener: ln}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&t.activeConn, 1)
			go func() {
				defer atomic.AddInt64(&t.activeConn, -1)
				defer conn.Close()

				remote, err := client.Dial("tcp", cfg.RemoteAddr)
				if err != nil {
					if infoFn != nil {
						infoFn(fmt.Sprintf("tunnel %s → %s: dial remote failed: %s", cfg.LocalAddr, cfg.RemoteAddr, err))
					}
					return
				}
				defer remote.Close()

				relay(conn, remote)
			}()
		}
	}()

	if infoFn != nil {
		infoFn(fmt.Sprintf("local forward %s → %s via %s", cfg.LocalAddr, cfg.RemoteAddr, cfg.Alias))
	}
	return t, nil
}

func StartRemote(ctx context.Context, client *ssh.Client, cfg Config, infoFn func(string)) (*Tunnel, error) {
	ln, err := client.Listen("tcp", cfg.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("remote listen %s: %w", cfg.RemoteAddr, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	t := &Tunnel{Config: cfg, cancel: cancel, listener: ln}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&t.activeConn, 1)
			go func() {
				defer atomic.AddInt64(&t.activeConn, -1)
				defer conn.Close()

				local, err := net.Dial("tcp", cfg.LocalAddr)
				if err != nil {
					if infoFn != nil {
						infoFn(fmt.Sprintf("tunnel %s → %s: dial local failed: %s", cfg.RemoteAddr, cfg.LocalAddr, err))
					}
					return
				}
				defer local.Close()

				relay(conn, local)
			}()
		}
	}()

	if infoFn != nil {
		infoFn(fmt.Sprintf("remote forward %s → %s via %s", cfg.RemoteAddr, cfg.LocalAddr, cfg.Alias))
	}
	return t, nil
}

func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(a, b)
		if tc, ok := a.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		io.Copy(b, a)
		if tc, ok := b.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	wg.Wait()
}
