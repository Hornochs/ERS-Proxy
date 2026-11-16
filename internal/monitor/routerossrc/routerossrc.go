// Package routerossrc is a monitor.Reader over the REST API of MikroTik
// RouterOS 7 (internal/routeros). RouterOS's SNMP agent has no VLAN table
// (only dot1qPvid), no duplex and no LAG membership; its REST API has all of
// these. The reader loads the device's state once per poll and serves it
// under the names of the standard MIB objects, so the driver stays
// independent of the transport:
//
//   - system group (sysDescr "RouterOS <board> <version>" as the SNMP agent
//     reports it) and the MikroTik scalars mtxrSerialNumber and
//     mtxrFirmwareVersion;
//   - IF-MIB with 64-bit byte counters; ifIndex is the number of RouterOS's
//     interface ID, which is also the SNMP agent's ifIndex, so the history
//     continues when a device switches between SNMP and REST;
//   - dot3StatsDuplexStatus and the speed from /interface/ethernet/monitor;
//   - dot1dBasePortIfIndex (bridge port = ifIndex), dot3adAggPortAttachedAggID
//     (the bond's ifIndex);
//   - Q-BRIDGE: dot1qPvid, dot1qVlanStaticName and the current egress and
//     untagged port lists from the bridge VLAN table (only for bridges with
//     VLAN filtering). Members of a bond are listed with the bond too, so the
//     faceplate shows the VLANs on the physical ports;
//   - LLDP-MIB: lldpRemSysName and lldpRemPortId from /ip/neighbor (LLDP, CDP
//     and MNDP neighbours).
package routerossrc

import (
	"context"
	"strconv"
	"sync"

	"github.com/lan-dot-party/ers-proxy/internal/monitor"
	"github.com/lan-dot-party/ers-proxy/internal/routeros"
)

// ObjectID is the sysObjectID RouterOS's SNMP agent reports.
const ObjectID = "1.3.6.1.4.1.14988.1"

// Reader serves one round of requests (one poll). Open a new one per poll.
type Reader struct {
	C *routeros.Client

	once sync.Once
	st   *routeros.State
	err  error
	objs map[string]map[string]monitor.Value
}

// State returns the loaded state (loading it on the first call).
func (r *Reader) State(ctx context.Context) (*routeros.State, error) {
	r.once.Do(func() {
		r.st, r.err = r.C.Load(ctx)
		if r.err == nil {
			r.objs = objects(r.st)
		}
	})
	return r.st, r.err
}

// SourceNotes returns remarks about data that could not be read.
func (r *Reader) SourceNotes() []string {
	if r.st == nil {
		return nil
	}
	return append([]string(nil), r.st.Notes...)
}

// Scalars implements monitor.Reader.
func (r *Reader) Scalars(ctx context.Context, names ...string) (map[string]monitor.Value, error) {
	if _, err := r.State(ctx); err != nil {
		return nil, err
	}
	out := map[string]monitor.Value{}
	for _, n := range names {
		if v, ok := r.objs[n][""]; ok {
			out[n] = v
		}
	}
	return out, nil
}

// Columns implements monitor.Reader.
func (r *Reader) Columns(ctx context.Context, names ...string) (monitor.Table, error) {
	if _, err := r.State(ctx); err != nil {
		return nil, err
	}
	t := monitor.Table{}
	for _, n := range names {
		for idx, v := range r.objs[n] {
			if idx == "" {
				continue
			}
			if t[idx] == nil {
				t[idx] = monitor.Row{}
			}
			t[idx][n] = v
		}
	}
	return t, nil
}

func text(s string) monitor.Value { return monitor.Value{Kind: monitor.KindBytes, Bytes: []byte(s)} }
func integer(n int) monitor.Value { return monitor.Value{Kind: monitor.KindInt, Int: int64(n)} }
func unsigned(n uint64) monitor.Value {
	return monitor.Value{Kind: monitor.KindUint, Uint: n}
}

// IfType maps RouterOS interface types to IANA ifType values, as RouterOS's
// SNMP agent reports them.
func IfType(t string) int {
	switch t {
	case "ether":
		return 6 // ethernetCsmacd
	case "bond":
		return 161 // ieee8023adLag
	case "vlan":
		return 135 // l2vlan
	case "bridge":
		return 209 // bridge
	case "loopback":
		return 24 // softwareLoopback
	case "wlan", "wifi", "wifiwave2":
		return 71 // ieee80211
	}
	return 1 // other
}

// objects maps the state to MIB object names; index "" holds scalars.
func objects(st *routeros.State) map[string]map[string]monitor.Value {
	o := map[string]map[string]monitor.Value{}
	set := func(name, idx string, v monitor.Value) {
		if o[name] == nil {
			o[name] = map[string]monitor.Value{}
		}
		o[name][idx] = v
	}
	descr := "RouterOS"
	if st.Board != "" {
		descr += " " + st.Board
	}
	if st.Version != "" {
		descr += " " + st.Version
	}
	set("sysDescr", "", text(descr))
	set("sysObjectID", "", monitor.Value{Kind: monitor.KindOID, OID: ObjectID})
	set("sysUpTime", "", unsigned(uint64(st.Uptime.Milliseconds()/10)))
	set("sysName", "", text(st.Identity))
	set("sysLocation", "", text(st.Location))
	if st.Serial != "" {
		set("mtxrSerialNumber", "", text(st.Serial))
	}
	if st.Firmware != "" {
		set("mtxrFirmwareVersion", "", text(st.Firmware))
	}

	byName := map[string]*routeros.Interface{}
	maxID := 0
	for _, i := range st.Interfaces {
		byName[i.Name] = i
		maxID = max(maxID, i.ID)
		idx := strconv.Itoa(i.ID)
		set("ifDescr", idx, text(i.Name))
		set("ifName", idx, text(i.Name))
		set("ifAlias", idx, text(i.Comment))
		set("ifType", idx, integer(IfType(i.Type)))
		admin, oper := 1, 2
		if i.Disabled {
			admin = 2
		}
		if i.Running {
			oper = 1
		}
		set("ifAdminStatus", idx, integer(admin))
		set("ifOperStatus", idx, integer(oper))
		if i.Speed > 0 {
			set("ifHighSpeed", idx, unsigned(i.Speed/1_000_000))
			set("ifSpeed", idx, unsigned(min(i.Speed, 4_294_967_295)))
		}
		if i.HasDuplex {
			duplex := 2
			if i.FullDuplex {
				duplex = 3
			}
			set("dot3StatsDuplexStatus", idx, integer(duplex))
		}
		set("ifHCInOctets", idx, unsigned(i.RxBytes))
		set("ifHCOutOctets", idx, unsigned(i.TxBytes))
		set("ifInErrors", idx, unsigned(i.RxErrors))
		set("ifOutErrors", idx, unsigned(i.TxErrors))
		set("ifInDiscards", idx, unsigned(i.RxDrops))
		set("ifOutDiscards", idx, unsigned(i.TxDrops))
	}
	// Bridge ports and bond slaves; a slave takes over its bond's PVID.
	pvid := map[int]int{}
	for _, i := range st.Interfaces {
		idx := strconv.Itoa(i.ID)
		port := i.Bridge != ""
		if b := byName[i.Bond]; b != nil {
			set("dot3adAggPortAttachedAggID", idx, integer(b.ID))
			port = port || b.Bridge != ""
			if b.PVID > 0 && !b.TaggedOnly {
				pvid[i.ID] = b.PVID
			}
		}
		if port {
			set("dot1dBasePortIfIndex", idx, integer(i.ID))
		}
		if i.PVID > 0 && !i.TaggedOnly {
			pvid[i.ID] = i.PVID
		}
	}
	for _, n := range st.Neighbors {
		i := byName[n.Interface]
		if i == nil {
			continue
		}
		k := 1
		for {
			if _, used := o["lldpRemSysName"]["0."+strconv.Itoa(i.ID)+"."+strconv.Itoa(k)]; !used {
				break
			}
			k++
		}
		idx := "0." + strconv.Itoa(i.ID) + "." + strconv.Itoa(k)
		set("lldpRemSysName", idx, text(n.Identity))
		set("lldpRemPortId", idx, text(n.PortID))
		set("lldpRemPortDesc", idx, text(""))
		caps := []byte{0, 0}
		if n.Bridge {
			caps[0] = 0x20 // bit 2: bridge
		}
		set("lldpRemSysCapEnabled", idx, monitor.Value{Kind: monitor.KindBytes, Bytes: caps})
	}
	if !st.VLANFiltering {
		return o
	}
	for id, v := range pvid {
		set("dot1qPvid", strconv.Itoa(id), integer(v))
	}
	// slaves lists the members of each bond by bond name.
	slaves := map[string][]int{}
	for _, i := range st.Interfaces {
		if i.Bond != "" {
			slaves[i.Bond] = append(slaves[i.Bond], i.ID)
		}
	}
	size := (maxID + 7) / 8
	bits := func(b []byte, names []string) {
		for _, n := range names {
			i := byName[n]
			if i == nil || i.Type == "bridge" { // the bridge itself is the CPU port
				continue
			}
			for _, id := range append([]int{i.ID}, slaves[n]...) {
				b[(id-1)/8] |= 0x80 >> uint((id-1)%8) // Q-BRIDGE PortList: bit 0 = bridge port 1
			}
		}
	}
	for _, v := range st.VLANs {
		vid := strconv.Itoa(v.ID)
		set("dot1qVlanStaticName", vid, text(v.Name))
		egress, untag := make([]byte, size), make([]byte, size)
		bits(egress, v.Tagged)
		bits(egress, v.Untagged)
		bits(untag, v.Untagged)
		set("dot1qVlanCurrentEgressPorts", "0."+vid, monitor.Value{Kind: monitor.KindBytes, Bytes: egress})
		set("dot1qVlanCurrentUntaggedPorts", "0."+vid, monitor.Value{Kind: monitor.KindBytes, Bytes: untag})
	}
	return o
}
