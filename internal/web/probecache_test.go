package web

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/upstream"
)

func TestProbeCache(t *testing.T) {
	var c probeCache
	var calls atomic.Int32
	probe := func() *upstream.ProbeResult { calls.Add(1); return &upstream.ProbeResult{} }

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.get(1, "https://a.example", probe) }()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("10 concurrent opens probed %d times, want 1", n)
	}
	c.get(1, "https://b.example", probe) // URL changed
	c.get(2, "https://a.example", probe) // other device
	if n := calls.Load(); n != 3 {
		t.Fatalf("probes = %d, want 3", n)
	}
	c.forget(1)
	c.get(1, "https://b.example", probe)
	if n := calls.Load(); n != 4 {
		t.Fatalf("probe after forget not repeated: %d", n)
	}
}
