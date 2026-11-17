package cmd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/edmproxy"
	"github.com/lan-dot-party/ers-proxy/internal/monitor"
	"github.com/lan-dot-party/ers-proxy/internal/monitor/routerossrc"
	"github.com/lan-dot-party/ers-proxy/internal/routeros"
	"github.com/lan-dot-party/ers-proxy/internal/secret"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// routerOSSources keeps one REST client per RouterOS device for the
// dashboard's "routeros" data source. The client remembers a rejected login,
// so a wrong password is tried only once until the stored login or the
// address changes.
type routerOSSources struct {
	proxy *edmproxy.Proxy
	key   *secret.Key
	mu    sync.Mutex
	m     map[int64]routerOSEntry
}

type routerOSEntry struct {
	sig string
	c   *routeros.Client
}

func newRouterOSSource(p *edmproxy.Proxy, key *secret.Key) *routerOSSources {
	return &routerOSSources{proxy: p, key: key, m: map[int64]routerOSEntry{}}
}

// open implements poller.OpenFunc.
func (g *routerOSSources) open(_ context.Context, d *store.Device) (monitor.Reader, func(), error) {
	if !d.RouterOS() {
		return nil, nil, errors.New("the RouterOS data source needs a device of kind routeros")
	}
	if !d.HasCredentials() {
		return nil, nil, errors.New("the RouterOS data source needs a stored web login (device settings)")
	}
	c, err := g.client(d)
	if err != nil {
		return nil, nil, err
	}
	return &routerossrc.Reader{C: c}, func() {}, nil
}

func (g *routerOSSources) client(d *store.Device) (*routeros.Client, error) {
	h := sha256.Sum256(d.Credentials)
	sig := fmt.Sprintf("%s|%s|%s|%x", d.URL, d.TLSMode, d.TLSPin, h[:8])
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.m[d.ID]; ok && e.sig == sig {
		return e.c, nil
	}
	cred, err := devices.DecryptCredentials(g.key, d)
	if err != nil {
		return nil, fmt.Errorf("open the stored web login: %w", err)
	}
	uc, err := g.proxy.Client(d)
	if err != nil {
		return nil, err
	}
	c := &routeros.Client{Transport: uc.Transport, Base: uc.Base().String(), Username: cred.Username, Password: cred.Password}
	g.m[d.ID] = routerOSEntry{sig: sig, c: c}
	return c, nil
}
