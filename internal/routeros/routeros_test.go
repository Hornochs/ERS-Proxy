package routeros_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lan-dot-party/ers-proxy/internal/routeros"
	"github.com/lan-dot-party/ers-proxy/internal/routeros/routerostest"
)

func client(srv *httptest.Server, user, pass string) *routeros.Client {
	return &routeros.Client{Transport: http.DefaultTransport, Base: srv.URL, Username: user, Password: pass}
}

func TestLoad(t *testing.T) {
	fake := routerostest.New()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	st, err := client(srv, routerostest.User, routerostest.Password).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Identity != "ros-test" || st.Board != "CRS310-8G+2S+" || st.Version != "7.25.1" || st.Serial != "TEST0002" ||
		st.Firmware != "7.21.5" || st.Location != "rack 1" || st.Uptime != 26*time.Hour+3*time.Minute+4*time.Second {
		t.Errorf("system %+v", st)
	}
	e1 := st.Interface("ether1")
	if e1 == nil || e1.ID != 2 || e1.Comment != "office" || e1.Speed != 1e9 || !e1.FullDuplex || e1.RxBytes != 5_000_000_000 ||
		e1.RxErrors != 1 || e1.RxDrops != 2 || e1.PVID != 10 || e1.Bridge != "bridge1" {
		t.Errorf("ether1 %+v", e1)
	}
	if e2 := st.Interface("ether2"); e2.Speed != 100e6 || e2.FullDuplex || !e2.HasDuplex {
		t.Errorf("ether2 %+v", e2)
	}
	if e3 := st.Interface("ether3"); e3.Bond != "bond1" || e3.Speed != 2.5e9 {
		t.Errorf("ether3 %+v", e3)
	}
	if up := st.Interface("uplink"); !up.TaggedOnly || up.DefaultName != "sfp-sfpplus1" || up.Speed != 10e9 {
		t.Errorf("uplink %+v", up)
	}
	if e5 := st.Interface("ether5"); !e5.Disabled || e5.Speed != 0 {
		t.Errorf("ether5 %+v", e5)
	}
	var ids []int
	for _, v := range st.VLANs {
		ids = append(ids, v.ID)
	}
	if !reflect.DeepEqual(ids, []int{1, 10, 20, 30, 31}) {
		t.Fatalf("VLAN IDs %v", ids)
	}
	v10, v20 := st.VLANs[1], st.VLANs[2]
	if v10.Name != "mgmt" || !reflect.DeepEqual(v10.Tagged, []string{"bond1", "bridge1", "uplink"}) || !reflect.DeepEqual(v10.Untagged, []string{"ether1"}) {
		t.Errorf("VLAN 10 %+v", v10)
	}
	if v20.Name != "clients" || !reflect.DeepEqual(v20.Untagged, []string{"bond1", "ether2"}) {
		t.Errorf("VLAN 20 %+v", v20)
	}
	if st.VLANs[0].Name != "" { // dynamic entry: comment "added by pvid" is no name
		t.Errorf("VLAN 1 %+v", st.VLANs[0])
	}
	if len(st.Neighbors) != 2 || st.Neighbors[0] != (routeros.Neighbor{Interface: "uplink", Identity: "core-sw", PortID: "1/1", MAC: "AA:BB:CC:00:00:01", Bridge: true, By: "lldp"}) {
		t.Errorf("neighbours %+v", st.Neighbors)
	}
	wantHosts := []routeros.Host{
		{MAC: "AA:BB:CC:00:00:01", VID: 10, Interface: "uplink", Bridge: "bridge1", Dynamic: true},
		{MAC: "AA:BB:CC:00:00:02", VID: 20, Interface: "ether2", Bridge: "bridge1", Dynamic: true},
		{MAC: "AA:BB:CC:00:00:03", VID: 20, Interface: "bond1", Bridge: "bridge1", Dynamic: true},
	}
	if !reflect.DeepEqual(st.Hosts, wantHosts) { // the local entry is skipped
		t.Errorf("hosts %+v", st.Hosts)
	}
	if len(fake.Writes) != 0 {
		t.Errorf("writes %v", fake.Writes)
	}
	if len(st.Notes) != 0 {
		t.Errorf("notes %v", st.Notes)
	}
}

func TestLoadWithoutMonitorAndFiltering(t *testing.T) {
	fake := routerostest.New()
	fake.NoMonitor, fake.FilteringOff = true, true
	srv := httptest.NewServer(fake)
	defer srv.Close()
	st, err := client(srv, routerostest.User, routerostest.Password).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.VLANFiltering || len(st.VLANs) != 0 || st.Interface("ether1").PVID != 0 || st.Interface("ether1").Speed != 0 {
		t.Errorf("state %+v", st)
	}
	notes := strings.Join(st.Notes, " | ")
	if !strings.Contains(notes, "Speed and duplex") || !strings.Contains(notes, "VLAN filtering is off on bridge bridge1") {
		t.Errorf("notes %q", notes)
	}
}

// A rejected login is not sent again.
func TestRejectedLogin(t *testing.T) {
	fake := routerostest.New()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := client(srv, routerostest.User, "wrong")
	for i := 0; i < 3; i++ {
		if _, err := c.Load(context.Background()); !errors.Is(err, routeros.ErrLogin) {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	if fake.Rejected != 1 {
		t.Errorf("%d rejected logins sent, want 1", fake.Rejected)
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]uint64{"10Mbps": 10e6, "100Mbps": 100e6, "1Gbps": 1e9, "2.5Gbps": 2.5e9, "10Gbps": 10e9, "": 0, "x": 0} {
		if got := routeros.ParseRate(in); got != want {
			t.Errorf("ParseRate(%q) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[string]time.Duration{
		"22m38s": 22*time.Minute + 38*time.Second, "1w2d3h4m5s": 9*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second,
		"5s100ms": 5*time.Second + 100*time.Millisecond, "": 0, "3x": 0, "12": 0,
	} {
		if got := routeros.ParseDuration(in); got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", in, got, want)
		}
	}
	if got := routeros.ParseIDList("1, 10,20-22,5000,9-8,x", 100); !reflect.DeepEqual(got, []int{1, 10, 20, 21, 22}) {
		t.Errorf("ParseIDList = %v", got)
	}
	if got := routeros.ParseIDList("1-4094", 3); len(got) != 3 {
		t.Errorf("ParseIDList limit = %v", got)
	}
	if n, ok := routeros.ParseID("*1A"); !ok || n != 26 {
		t.Errorf("ParseID = %d %v", n, ok)
	}
	if _, ok := routeros.ParseID("1A"); ok {
		t.Error("ParseID without *")
	}
}

// TestReadOnly checks the source: the only POST is the monitor command, and
// no write command of the REST API appears.
func TestReadOnly(t *testing.T) {
	fake := routerostest.New()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	if _, err := client(srv, routerostest.User, routerostest.Password).Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range fake.Requests {
		if !strings.HasPrefix(r, "GET /rest/") && r != "POST /rest/interface/ethernet/monitor" {
			t.Errorf("request %q", r)
		}
	}
}
