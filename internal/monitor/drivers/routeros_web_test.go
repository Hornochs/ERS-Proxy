package drivers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/monitor"
	"github.com/lan-dot-party/ers-proxy/internal/monitor/routerossrc"
	"github.com/lan-dot-party/ers-proxy/internal/routeros"
	"github.com/lan-dot-party/ers-proxy/internal/routeros/routerostest"
)

// TestRouterOSWebSource collects through the RouterOS REST API (routerossrc)
// of the fake device: VLANs, tagging, duplex, bonds and neighbours come from
// there.
func TestRouterOSWebSource(t *testing.T) {
	srv := httptest.NewServer(routerostest.New())
	defer srv.Close()
	c := &routeros.Client{Transport: http.DefaultTransport, Base: srv.URL, Username: routerostest.User, Password: routerostest.Password}
	ctx := context.Background()
	r := &routerossrc.Reader{C: c}
	sys, err := monitor.ReadSystem(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	d, err := monitor.NewRegistry(All()...).Detect(sys)
	if err != nil || d.Name() != "routeros" {
		t.Fatalf("detect: %v %v", d, err)
	}
	s, err := d.Collect(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.System.Name != "ros-test" || s.System.Descr != "RouterOS CRS310-8G+2S+ 7.25.1" || s.System.Location != "rack 1" {
		t.Fatalf("system %+v", s.System)
	}
	if u := s.Units[0]; u.Model != "CRS310-8G+2S+" || u.Firmware != "7.25.1" || u.Serial != "TEST0002" {
		t.Errorf("unit %+v", u)
	}
	p1 := front(t, s, 1)
	if p1.Name != "ether1" || p1.IfIndex != 2 || p1.Alias != "office" || p1.Duplex != "full" || p1.Speed != 1e9 || p1.PVID != 10 ||
		!reflect.DeepEqual(p1.Untagged, []int{10}) || len(p1.Tagged) != 0 || p1.Mode != "access" || p1.InErrors != 1 ||
		p1.InOctets != 5_000_000_000 || !p1.HasHC || p1.InDiscards != 2 {
		t.Errorf("port 1: %+v", p1)
	}
	p2 := front(t, s, 2)
	if p2.Duplex != "half" || p2.Speed != 100e6 || p2.Neighbor == nil || p2.Neighbor.SysName != "phone" || p2.Neighbor.Bridge {
		t.Errorf("port 2: %+v %+v", p2, p2.Neighbor)
	}
	// Bond members carry the bond's VLANs and PVID.
	for _, n := range []int{3, 4} {
		p := front(t, s, n)
		if p.LAG != 9 || p.LAGName != "bond1" || p.PVID != 20 || !reflect.DeepEqual(p.Untagged, []int{20}) ||
			!reflect.DeepEqual(p.Tagged, []int{10}) || p.Mode != "trunk" {
			t.Errorf("port %d: %+v", n, p)
		}
	}
	// Renamed SFP+ port, tagged only: no PVID.
	p5 := front(t, s, 5)
	if p5.Name != "uplink" || p5.PVID != 0 || len(p5.Untagged) != 0 || !reflect.DeepEqual(p5.Tagged, []int{10, 20, 30, 31}) ||
		p5.Speed != 10e9 || p5.Neighbor == nil || p5.Neighbor.SysName != "core-sw" || p5.Neighbor.PortID != "1/1" || !p5.Neighbor.Bridge {
		t.Errorf("uplink: %+v %+v", p5, p5.Neighbor)
	}
	if p6 := front(t, s, 6); p6.Name != "ether5" || p6.Admin != "down" || p6.Up() {
		t.Errorf("port 6: %+v", p6)
	}
	if len(s.VLANs) != 5 || s.VLANs[1] != (monitor.VLAN{ID: 10, Name: "mgmt"}) || s.VLANs[2] != (monitor.VLAN{ID: 20, Name: "clients"}) {
		t.Errorf("VLANs %+v", s.VLANs)
	}
	for _, p := range s.Ports {
		if p.Name == "bridge1" && (len(p.Tagged) > 0 || len(p.Untagged) > 0) {
			t.Errorf("bridge as VLAN member: %+v", p)
		}
		if p.Physical != (p.Number > 0) {
			t.Errorf("%s: physical %v number %d", p.Name, p.Physical, p.Number)
		}
	}
	if len(s.Notes) != 0 {
		t.Errorf("notes %v", s.Notes)
	}
}

func TestRouterOSMatch(t *testing.T) {
	ros := monitor.System{ObjectID: "1.3.6.1.4.1.14988.1", Descr: "RouterOS CRS310-8G+2S+ 7.25.1"}
	swos := monitor.System{ObjectID: "1.3.6.1.4.1.14988.2", Descr: "CRS305-1G-4S+ SwOS v2.18"}
	reg := monitor.NewRegistry(All()...)
	if d, err := reg.Detect(ros); err != nil || d.Name() != "routeros" {
		t.Errorf("RouterOS detected as %v (%v)", d, err)
	}
	if d, err := reg.Detect(swos); err != nil || d.Name() != "swos" {
		t.Errorf("SwOS detected as %v (%v)", d, err)
	}
	if m, v := routerOSDescr("RouterOS RB5009UG+S+ 7.16"); m != "RB5009UG+S+" || v != "7.16" {
		t.Errorf("descr: %q %q", m, v)
	}
	if m, v := routerOSDescr("Linux"); m != "" || v != "" {
		t.Errorf("descr: %q %q", m, v)
	}
}

// front returns the physical port with the given front-panel number.
func front(t *testing.T, s *monitor.Snapshot, number int) *monitor.Port {
	t.Helper()
	for _, p := range s.Ports {
		if p.Physical && p.Number == number {
			return p
		}
	}
	t.Fatalf("front-panel port %d missing", number)
	return nil
}
