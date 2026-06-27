package tunnel

import (
	"context"
	"testing"
)

func TestRegistryAddAndList(t *testing.T) {
	r := NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t1 := &Tunnel{Config: Config{Direction: Local, Alias: "host1", LocalAddr: ":8080", RemoteAddr: "localhost:80"}, cancel: cancel}
	r.Add(t1)

	if t1.ID == "" {
		t.Error("ID not assigned")
	}

	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(list))
	}
	if list[0].Alias != "host1" || list[0].Direction != "local" {
		t.Errorf("unexpected: %+v", list[0])
	}
	_ = ctx
}

func TestRegistryStop(t *testing.T) {
	r := NewRegistry()
	cancelled := false
	t1 := &Tunnel{
		Config: Config{Direction: Local, Alias: "host1"},
		cancel: func() { cancelled = true },
	}
	r.Add(t1)
	id := t1.ID

	if err := r.Stop(id); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Error("cancel not called")
	}
	if len(r.List()) != 0 {
		t.Error("tunnel not removed from registry")
	}
}

func TestRegistryStopNotFound(t *testing.T) {
	r := NewRegistry()
	if err := r.Stop("nonexistent"); err == nil {
		t.Error("expected error")
	}
}

func TestRegistryStopAll(t *testing.T) {
	r := NewRegistry()
	count := 0
	for i := 0; i < 3; i++ {
		t1 := &Tunnel{
			Config: Config{Direction: Local, Alias: "host"},
			cancel: func() { count++ },
		}
		r.Add(t1)
	}

	r.StopAll()
	if count != 3 {
		t.Errorf("expected 3 cancels, got %d", count)
	}
	if len(r.List()) != 0 {
		t.Error("tunnels not cleared")
	}
}
