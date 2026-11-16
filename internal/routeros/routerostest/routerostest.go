// Package routerostest is a fake RouterOS REST API for tests. It models a
// small switch with four copper ports, one SFP+ port, a bond, a bridge with
// VLAN filtering and one LLDP neighbour; all values are made up.
package routerostest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Login accepted by the fake device.
const (
	User     = "monitor"
	Password = "test-password"
)

type m = map[string]string

// Server is the fake device. Use it as an http.Handler.
type Server struct {
	mu sync.Mutex
	// Requests lists every request as "METHOD /path".
	Requests []string
	// Writes lists requests that would change something (anything but GET
	// and the monitor command).
	Writes []string
	// Logins counts rejected logins.
	Rejected int
	// NoMonitor makes /interface/ethernet/monitor fail (a user without the
	// right to run it).
	NoMonitor bool
	// FilteringOff turns VLAN filtering off on the bridge.
	FilteringOff bool
}

// New returns a fake device.
func New() *Server { return &Server{} }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/rest/")
	if r.Method != http.MethodGet && (r.Method != http.MethodPost || path != "interface/ethernet/monitor") {
		s.Writes = append(s.Writes, r.Method+" "+r.URL.Path)
	}
	if u, p, ok := r.BasicAuth(); !ok || u != User || p != Password {
		s.Rejected++
		w.Header().Set("WWW-Authenticate", `Basic realm="device"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": 401, "message": "Unauthorized"})
		return
	}
	switch {
	case r.Method == http.MethodPost && path == "interface/ethernet/monitor":
		s.monitor(w, r)
	case r.Method != http.MethodGet:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": 400, "message": "Bad Request", "detail": "not allowed in the fake"})
	default:
		v, ok := s.menu(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": 400, "message": "Bad Request", "detail": "no such command prefix"})
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *Server) monitor(w http.ResponseWriter, r *http.Request) {
	if s.NoMonitor {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": 400, "message": "Bad Request", "detail": "not enough permissions (9)"})
		return
	}
	var body struct {
		Numbers string  `json:"numbers"`
		Once    *string `json:"once"`
	}
	b, _ := io.ReadAll(r.Body)
	if json.Unmarshal(b, &body) != nil || body.Once == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": 400, "message": "Bad Request", "detail": "monitor without once would not end"})
		return
	}
	state := map[string]m{
		"*2": {"name": "ether1", "status": "link-ok", "rate": "1Gbps", "full-duplex": "true"},
		"*3": {"name": "ether2", "status": "link-ok", "rate": "100Mbps", "full-duplex": "false"},
		"*4": {"name": "ether3", "status": "link-ok", "rate": "2.5Gbps", "full-duplex": "true"},
		"*5": {"name": "ether4", "status": "link-ok", "rate": "2.5Gbps", "full-duplex": "true"},
		"*6": {"name": "uplink", "status": "link-ok", "rate": "10Gbps", "full-duplex": "true"},
	}
	var out []m
	for _, id := range strings.Split(body.Numbers, ",") {
		if e, ok := state[id]; ok {
			out = append(out, e)
		} else {
			out = append(out, m{"status": "no-link"})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) menu(path string) (any, bool) {
	filtering := "true"
	if s.FilteringOff {
		filtering = "false"
	}
	switch path {
	case "system/identity":
		return m{"name": "ros-test"}, true
	case "system/resource":
		return m{"board-name": "CRS310-8G+2S+", "version": "7.25.1", "uptime": "1d2h3m4s", "platform": "MikroTik"}, true
	case "system/routerboard":
		return m{"routerboard": "true", "model": "CRS310-8G+2S+", "serial-number": "TEST0002", "current-firmware": "7.21.5"}, true
	case "snmp":
		return m{"enabled": "true", "location": "rack 1", "contact": ""}, true
	case "interface":
		return []m{
			{".id": "*1", "name": "lo", "type": "loopback", "disabled": "false", "running": "true", "rx-byte": "100", "tx-byte": "100"},
			{".id": "*2", "name": "ether1", "default-name": "ether1", "type": "ether", "comment": "office", "disabled": "false", "running": "true",
				"rx-byte": "5000000000", "tx-byte": "1000", "rx-error": "1", "tx-error": "0", "rx-drop": "2", "tx-drop": "0"},
			{".id": "*3", "name": "ether2", "default-name": "ether2", "type": "ether", "disabled": "false", "running": "true", "rx-byte": "10", "tx-byte": "20"},
			{".id": "*4", "name": "ether3", "default-name": "ether3", "type": "ether", "disabled": "false", "running": "true", "rx-byte": "30", "tx-byte": "40"},
			{".id": "*5", "name": "ether4", "default-name": "ether4", "type": "ether", "disabled": "false", "running": "true", "rx-byte": "50", "tx-byte": "60"},
			{".id": "*6", "name": "uplink", "default-name": "sfp-sfpplus1", "type": "ether", "disabled": "false", "running": "true", "rx-byte": "70", "tx-byte": "80"},
			{".id": "*7", "name": "ether5", "default-name": "ether5", "type": "ether", "disabled": "true", "running": "false"},
			{".id": "*8", "name": "bridge1", "type": "bridge", "disabled": "false", "running": "true"},
			{".id": "*9", "name": "bond1", "type": "bond", "disabled": "false", "running": "true", "rx-byte": "80", "tx-byte": "100"},
			{".id": "*A", "name": "mgmt", "type": "vlan", "disabled": "false", "running": "true"},
		}, true
	case "interface/bonding":
		return []m{{"name": "bond1", "slaves": "ether3,ether4", "mode": "802.3ad"}}, true
	case "interface/bridge":
		return []m{{"name": "bridge1", "vlan-filtering": filtering}}, true
	case "interface/bridge/port":
		return []m{
			{"interface": "ether1", "bridge": "bridge1", "pvid": "10", "frame-types": "admit-all", "disabled": "false"},
			{"interface": "ether2", "bridge": "bridge1", "pvid": "20", "frame-types": "admit-only-untagged-and-priority-tagged", "disabled": "false"},
			{"interface": "bond1", "bridge": "bridge1", "pvid": "20", "frame-types": "admit-all", "disabled": "false"},
			{"interface": "uplink", "bridge": "bridge1", "pvid": "1", "frame-types": "admit-only-vlan-tagged", "disabled": "false"},
		}, true
	case "interface/vlan":
		return []m{{"name": "mgmt", "vlan-id": "10", "interface": "bridge1"}}, true
	case "interface/bridge/vlan":
		return []m{
			{"bridge": "bridge1", "vlan-ids": "10", "tagged": "bridge1,uplink,bond1", "untagged": "ether1", "current-tagged": "bridge1,uplink",
				"current-untagged": "ether1", "disabled": "false", "dynamic": "false"},
			{"bridge": "bridge1", "vlan-ids": "20", "tagged": "uplink", "untagged": "ether2,bond1", "current-tagged": "uplink",
				"current-untagged": "ether2,bond1", "disabled": "false", "dynamic": "false", "comment": "clients"},
			{"bridge": "bridge1", "vlan-ids": "30-31", "tagged": "uplink", "untagged": "", "disabled": "false", "dynamic": "false"},
			{"bridge": "bridge1", "vlan-ids": "99", "tagged": "uplink", "untagged": "", "disabled": "true", "dynamic": "false"},
			{"bridge": "bridge1", "vlan-ids": "1", "tagged": "", "untagged": "bridge1", "current-untagged": "bridge1", "disabled": "false",
				"dynamic": "true", "comment": "added by pvid"},
		}, true
	case "ip/neighbor":
		return []m{
			{"interface": "uplink,bridge1", "identity": "core-sw", "interface-name": "1/1", "system-caps-enabled": "bridge,router", "discovered-by": "lldp"},
			{"interface": "ether2,bridge1", "identity": "phone", "interface-name": "eth0", "system-caps-enabled": "telephone", "discovered-by": "lldp"},
		}, true
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
