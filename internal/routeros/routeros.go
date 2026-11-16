// Package routeros reads the REST API of MikroTik RouterOS 7 (verified on a
// CRS310-8G+2S+ with RouterOS 7.25.1). The REST API is part of the web
// server (www-ssl, and www from 7.9 on) and uses HTTP Basic authentication
// on every request; there is no session.
//
// The client is read-only: it sends GET requests for an allow-list of menus
// (readPaths) and exactly one command, "/interface/ethernet/monitor" with
// "once" (POST, because the REST API maps every command to POST; the command
// only reads link rate and duplex). Nothing else is ever sent – in
// particular no "set", "add", "remove" or "export".
package routeros

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxAnswer is the largest answer read from the device.
const maxAnswer = 4 << 20

// maxVLANs limits how many VLAN IDs ranges in the bridge VLAN table expand to.
const maxVLANs = 4094

// readPaths are the only menus read with GET.
var readPaths = map[string]bool{
	"system/identity": true, "system/resource": true, "system/routerboard": true, "snmp": true,
	"interface": true, "interface/ethernet": true, "interface/bonding": true, "interface/vlan": true,
	"interface/bridge": true, "interface/bridge/port": true, "interface/bridge/vlan": true,
	"ip/neighbor": true,
}

// monitorPath is the only command sent with POST.
const monitorPath = "interface/ethernet/monitor"

// ErrLogin means the device did not accept the stored login. The client does
// not try the same login again (RouterOS logs every failed login).
var ErrLogin = errors.New("the device rejected the login (check the web login stored for the device)")

// Client talks to one device. Safe for concurrent use; requests are sent one
// at a time.
type Client struct {
	Transport http.RoundTripper
	// Base is http(s)://host[:port] of the device.
	Base               string
	Username, Password string

	mu       sync.Mutex
	rejected bool
}

// Record is one entry of a RouterOS menu. The REST API returns every value as
// a string.
type Record map[string]string

// UnmarshalJSON accepts strings and turns other JSON values into their text.
func (r *Record) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = Record{}
	for k, v := range raw {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			s = strings.Trim(string(v), `"`)
		}
		(*r)[k] = s
	}
	return nil
}

// Bool reads a RouterOS boolean ("true"/"yes").
func (r Record) Bool(k string) bool { return r[k] == "true" || r[k] == "yes" }

// Uint reads a number; 0 when missing or not a number.
func (r Record) Uint(k string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(r[k]), 10, 64)
	return n
}

// Int reads a number; 0 when missing or not a number.
func (r Record) Int(k string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(r[k]))
	return n
}

// List splits a comma-separated value.
func (r Record) List(k string) []string {
	var out []string
	for _, s := range strings.Split(r[k], ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// apiError is the error body of the REST API.
type apiError struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if c.rejected {
		return ErrLogin
	}
	switch {
	case method == http.MethodGet && readPaths[path]:
	case method == http.MethodPost && path == monitorPath:
	default:
		return fmt.Errorf("routeros: %s /rest/%s is not a read", method, path)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimSuffix(c.Base, "/") + "/rest/" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("routeros /%s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*maxAnswer))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("routeros /%s: %w", path, err)
	}
	if len(data) > maxAnswer {
		return fmt.Errorf("routeros /%s: answer too large", path)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		c.rejected = true
		return ErrLogin
	case resp.StatusCode != http.StatusOK:
		var e apiError
		if json.Unmarshal(data, &e) == nil && e.Message != "" {
			if e.Detail != "" {
				return fmt.Errorf("routeros /%s: HTTP %d %s (%s)", path, resp.StatusCode, e.Message, e.Detail)
			}
			return fmt.Errorf("routeros /%s: HTTP %d %s", path, resp.StatusCode, e.Message)
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("routeros /%s: HTTP 404 (is this RouterOS 7 with the REST API? It needs the www-ssl or www service)", path)
		}
		return fmt.Errorf("routeros /%s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("routeros /%s: answer is not JSON: %w", path, err)
	}
	return nil
}

// list reads a menu. props limits the answer to these properties (.proplist).
func (c *Client) list(ctx context.Context, path string, props ...string) ([]Record, error) {
	var q url.Values
	if len(props) > 0 {
		q = url.Values{".proplist": {strings.Join(props, ",")}}
	}
	var out []Record
	if err := c.do(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// item reads a menu with a single entry (system/identity, system/resource …).
func (c *Client) item(ctx context.Context, path string) (Record, error) {
	var out Record
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Interface is one interface of the device.
type Interface struct {
	// ID is the number of RouterOS's internal ID (".id" "*B" = 11); the
	// device's SNMP agent uses it as ifIndex.
	ID                int
	Name, DefaultName string
	// Type is RouterOS's type: ether, bond, bridge, vlan, loopback, wlan …
	Type     string
	Comment  string
	Disabled bool
	Running  bool

	RxBytes, TxBytes   uint64
	RxErrors, TxErrors uint64
	RxDrops, TxDrops   uint64

	// From /interface/ethernet/monitor (ethernet ports with link only).
	Speed      uint64 // bit/s, 0 = unknown
	FullDuplex bool
	HasDuplex  bool

	// Bridge port settings (empty Bridge: not a bridge port).
	Bridge     string
	PVID       int
	TaggedOnly bool // frame-types=admit-only-vlan-tagged
	// Bond is the bonding interface this port is a slave of.
	Bond string
}

// VLAN is one VLAN ID of a bridge's VLAN table, with its member interfaces
// (by name). Entries of all bridges with VLAN filtering are merged.
type VLAN struct {
	ID       int
	Name     string
	Tagged   []string
	Untagged []string
}

// Neighbor is an entry of /ip/neighbor (LLDP, CDP or MNDP).
type Neighbor struct {
	// Interface is the local port the neighbour was seen on.
	Interface string
	Identity  string
	// PortID is the remote port as the neighbour describes it ("interface-name").
	PortID string
	Bridge bool
	// By is how it was discovered (lldp, cdp, mndp, comma-separated).
	By string
}

// State is everything read in one poll.
type State struct {
	Identity string
	// Board is the board name ("CRS310-8G+2S+"), Version the RouterOS version.
	Board, Version string
	// Model, Serial and Firmware are from system/routerboard (empty on CHR and x86).
	Model, Serial, Firmware string
	Location                string
	Uptime                  time.Duration

	Interfaces []*Interface
	// VLANFiltering is true when at least one bridge filters VLANs; VLANs
	// and PVIDs are only filled then.
	VLANFiltering bool
	VLANs         []VLAN
	Neighbors     []Neighbor
	// Notes are remarks about data that could not be read.
	Notes []string
}

// Interface returns the interface with the given name.
func (s *State) Interface(name string) *Interface {
	for _, i := range s.Interfaces {
		if i.Name == name {
			return i
		}
	}
	return nil
}

// Load reads the device's state.
func (c *Client) Load(ctx context.Context) (*State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := &State{}
	res, err := c.item(ctx, "system/resource")
	if err != nil {
		return nil, err
	}
	st.Board, st.Version = res["board-name"], res["version"]
	if i := strings.IndexByte(st.Version, ' '); i > 0 {
		st.Version = st.Version[:i] // "7.25.1 (stable)" in older versions
	}
	st.Uptime = ParseDuration(res["uptime"])
	if id, err := c.item(ctx, "system/identity"); err == nil {
		st.Identity = id["name"]
	} else {
		return nil, err
	}
	if rb, err := c.item(ctx, "system/routerboard"); err == nil && rb.Bool("routerboard") {
		st.Model, st.Serial, st.Firmware = rb["model"], rb["serial-number"], rb["current-firmware"]
	}
	if s, err := c.item(ctx, "snmp"); err == nil {
		st.Location = s["location"]
	}

	ifs, err := c.list(ctx, "interface", ".id", "name", "default-name", "type", "comment", "disabled", "running",
		"rx-byte", "tx-byte", "rx-error", "tx-error", "rx-drop", "tx-drop")
	if err != nil {
		return nil, err
	}
	for _, r := range ifs {
		id, ok := ParseID(r[".id"])
		if !ok || r["name"] == "" {
			continue
		}
		st.Interfaces = append(st.Interfaces, &Interface{
			ID: id, Name: r["name"], DefaultName: r["default-name"], Type: r["type"], Comment: r["comment"],
			Disabled: r.Bool("disabled"), Running: r.Bool("running"),
			RxBytes: r.Uint("rx-byte"), TxBytes: r.Uint("tx-byte"), RxErrors: r.Uint("rx-error"), TxErrors: r.Uint("tx-error"),
			RxDrops: r.Uint("rx-drop"), TxDrops: r.Uint("tx-drop"),
		})
	}
	if len(st.Interfaces) == 0 {
		return nil, errors.New("routeros: the device returned no interfaces")
	}
	sort.Slice(st.Interfaces, func(i, j int) bool { return st.Interfaces[i].ID < st.Interfaces[j].ID })

	c.linkRates(ctx, st)

	if bonds, err := c.list(ctx, "interface/bonding", "name", "slaves"); err == nil {
		for _, b := range bonds {
			for _, s := range b.List("slaves") {
				if i := st.Interface(s); i != nil {
					i.Bond = b["name"]
				}
			}
		}
	} else {
		st.Notes = append(st.Notes, "LAGs: "+err.Error())
	}

	if err := c.bridges(ctx, st); err != nil {
		st.Notes = append(st.Notes, "VLANs: "+err.Error())
	}

	if ns, err := c.list(ctx, "ip/neighbor", "interface", "identity", "interface-name", "system-caps-enabled", "discovered-by"); err == nil {
		for _, n := range ns {
			ifs := n.List("interface")
			if len(ifs) == 0 {
				continue
			}
			caps := "," + n["system-caps-enabled"] + ","
			st.Neighbors = append(st.Neighbors, Neighbor{Interface: ifs[0], Identity: n["identity"], PortID: n["interface-name"],
				Bridge: strings.Contains(caps, ",bridge,"), By: n["discovered-by"]})
		}
	} else {
		st.Notes = append(st.Notes, "Neighbours: "+err.Error())
	}
	return st, nil
}

// linkRates asks /interface/ethernet/monitor once for the rate and duplex of
// the running ethernet ports.
func (c *Client) linkRates(ctx context.Context, st *State) {
	var ids []string
	byName := map[string]*Interface{}
	for _, i := range st.Interfaces {
		if i.Type == "ether" && i.Running && !i.Disabled {
			ids = append(ids, "*"+strings.ToUpper(strconv.FormatInt(int64(i.ID), 16)))
			byName[i.Name] = i
		}
	}
	if len(ids) == 0 {
		return
	}
	body := map[string]string{"numbers": strings.Join(ids, ","), "once": "", ".proplist": "name,status,rate,full-duplex"}
	var out []Record
	if err := c.do(ctx, http.MethodPost, monitorPath, nil, body, &out); err != nil {
		st.Notes = append(st.Notes, "Speed and duplex: "+err.Error())
		return
	}
	for _, r := range out {
		i := byName[r["name"]]
		if i == nil || r["status"] != "link-ok" {
			continue
		}
		i.Speed = ParseRate(r["rate"])
		if v, ok := r["full-duplex"]; ok {
			i.HasDuplex, i.FullDuplex = true, v == "true" || v == "yes"
		}
	}
}

// bridges reads bridge ports and the bridge VLAN table.
func (c *Client) bridges(ctx context.Context, st *State) error {
	brs, err := c.list(ctx, "interface/bridge", "name", "vlan-filtering")
	if err != nil {
		return err
	}
	filtering := map[string]bool{}
	var off []string
	for _, b := range brs {
		if b.Bool("vlan-filtering") {
			filtering[b["name"]] = true
		} else {
			off = append(off, b["name"])
		}
	}
	if len(brs) == 0 {
		return nil
	}
	st.VLANFiltering = len(filtering) > 0
	if len(off) > 0 {
		st.Notes = append(st.Notes, "VLANs: VLAN filtering is off on bridge "+strings.Join(off, ", ")+"; its VLAN settings are not shown")
	}
	ports, err := c.list(ctx, "interface/bridge/port", "interface", "bridge", "pvid", "frame-types", "disabled")
	if err != nil {
		return err
	}
	for _, p := range ports {
		if p.Bool("disabled") {
			continue
		}
		i := st.Interface(p["interface"])
		if i == nil {
			continue
		}
		i.Bridge = p["bridge"]
		if filtering[i.Bridge] {
			i.PVID = p.Int("pvid")
			i.TaggedOnly = p["frame-types"] == "admit-only-vlan-tagged"
		}
	}
	if !st.VLANFiltering {
		return nil
	}
	// VLAN interfaces on a bridge give its VLANs their names.
	names := map[int]string{}
	if vs, err := c.list(ctx, "interface/vlan", "name", "vlan-id", "interface"); err == nil {
		for _, v := range vs {
			if filtering[v["interface"]] {
				if id := v.Int("vlan-id"); id > 0 && names[id] == "" {
					names[id] = v["name"]
				}
			}
		}
	}
	table, err := c.list(ctx, "interface/bridge/vlan", "bridge", "vlan-ids", "tagged", "untagged",
		"current-tagged", "current-untagged", "disabled", "dynamic", "comment")
	if err != nil {
		return err
	}
	type members struct{ tagged, untagged map[string]bool }
	byID := map[int]*members{}
	var order []int
	for _, e := range table {
		if e.Bool("disabled") || !filtering[e["bridge"]] {
			continue
		}
		ids := ParseIDList(e["vlan-ids"], maxVLANs)
		for _, id := range ids {
			m := byID[id]
			if m == nil {
				if len(byID) >= maxVLANs {
					break
				}
				m = &members{map[string]bool{}, map[string]bool{}}
				byID[id] = m
				order = append(order, id)
			}
			// Configured members plus what RouterOS added at run time
			// (current-*: dynamic entries, MVRP); configured wins.
			for _, n := range append(e.List("tagged"), e.List("current-tagged")...) {
				if !m.untagged[n] {
					m.tagged[n] = true
				}
			}
			for _, n := range append(e.List("untagged"), e.List("current-untagged")...) {
				if !m.tagged[n] {
					m.untagged[n] = true
				}
			}
			if names[id] == "" && !e.Bool("dynamic") && e["comment"] != "" {
				names[id] = e["comment"]
			}
		}
	}
	sort.Ints(order)
	for _, id := range order {
		v := VLAN{ID: id, Name: names[id]}
		for n := range byID[id].tagged {
			v.Tagged = append(v.Tagged, n)
		}
		for n := range byID[id].untagged {
			v.Untagged = append(v.Untagged, n)
		}
		sort.Strings(v.Tagged)
		sort.Strings(v.Untagged)
		st.VLANs = append(st.VLANs, v)
	}
	return nil
}

// ParseID turns RouterOS's internal ID ("*1A") into a number.
func ParseID(s string) (int, bool) {
	if !strings.HasPrefix(s, "*") {
		return 0, false
	}
	n, err := strconv.ParseInt(s[1:], 16, 32)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int(n), true
}

// ParseIDList expands "1,10,20-25" into VLAN IDs (1–4094), at most max of them.
func ParseIDList(s string, max int) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi := part, part
		if i := strings.IndexByte(part, '-'); i > 0 {
			lo, hi = part[:i], part[i+1:]
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || a < 1 || b > 4094 || a > b {
			continue
		}
		for id := a; id <= b && len(out) < max; id++ {
			out = append(out, id)
		}
	}
	return out
}

// ParseRate reads the link rate of /interface/ethernet/monitor ("10Mbps",
// "2.5Gbps") in bit/s; 0 when unknown.
func ParseRate(s string) uint64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "bps")
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "G"):
		mult, s = 1e9, strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		mult, s = 1e6, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, strings.TrimSuffix(s, "k")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return uint64(f*mult + 0.5)
}

// ParseDuration reads RouterOS durations such as "1w2d3h4m5s" or "22m38s"
// (also with "ms" and "us"); 0 when it cannot be read.
func ParseDuration(s string) time.Duration {
	units := []struct {
		suffix string
		d      time.Duration
	}{{"us", time.Microsecond}, {"ms", time.Millisecond}, {"w", 7 * 24 * time.Hour}, {"d", 24 * time.Hour},
		{"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}}
	var total time.Duration
	num := ""
	for i := 0; i < len(s); {
		ch := s[i]
		if ch >= '0' && ch <= '9' {
			num += string(ch)
			i++
			continue
		}
		matched := false
		for _, u := range units {
			if strings.HasPrefix(s[i:], u.suffix) {
				n, err := strconv.Atoi(num)
				if err != nil {
					return 0
				}
				total += time.Duration(n) * u.d
				num, i, matched = "", i+len(u.suffix), true
				break
			}
		}
		if !matched {
			return 0
		}
	}
	if num != "" {
		return 0
	}
	return total
}
