package drivers

import (
	"context"
	"sort"
	"strings"

	"github.com/lan-dot-party/ers-proxy/internal/monitor"
)

// RouterOS covers MikroTik devices running RouterOS (CRS3xx switches, also
// routers), verified on a CRS310-8G+2S+ with RouterOS 7.25.1. Facts from the
// hardware:
//   - sysObjectID is 1.3.6.1.4.1.14988.1 (SwOS reports .14988.2), sysDescr
//     "RouterOS <board> <version>";
//   - ifIndex is the number of RouterOS's internal interface ID: lo is 1,
//     then the ethernet ports in front-panel order (ether1 … sfp-sfpplus2),
//     then bridges, bonds and VLAN interfaces; ifName is the interface's
//     (renamable) name;
//   - the SNMP agent implements IF-MIB with 64-bit counters, BRIDGE-MIB,
//     LLDP-MIB and from Q-BRIDGE only dot1qPvid: no VLAN table, no EtherLike
//     duplex and no IEEE8023-LAG. VLAN membership, tagging, duplex and bonds
//     are only in the REST API, which the "routeros" data source
//     (internal/monitor/routerossrc) reads and serves under the standard
//     object names;
//   - mtxrSerialNumber is the serial number, mtxrFirmwareVersion the
//     RouterBOOT firmware; the RouterOS version is the last word of sysDescr.
type RouterOS struct{}

// Name implements monitor.Driver.
func (RouterOS) Name() string { return "routeros" }

// Match implements monitor.Driver.
func (RouterOS) Match(sys monitor.System) int {
	switch {
	case sys.ObjectID == "1.3.6.1.4.1.14988.1" || strings.HasPrefix(sys.ObjectID, "1.3.6.1.4.1.14988.1."):
		return 100
	case strings.HasPrefix(sys.Descr, "RouterOS"):
		return 90
	}
	return 0
}

// Collect implements monitor.Driver.
func (RouterOS) Collect(ctx context.Context, r monitor.Reader) (*monitor.Snapshot, error) {
	b, err := monitor.CollectBase(ctx, r, monitor.BaseOptions{})
	if err != nil {
		return nil, err
	}
	snap := b.Snap
	snap.Driver = "routeros"
	// Front-panel number = position among the ethernet ports in ifIndex
	// order (bridge port numbers only cover the ports in a bridge).
	n := 0
	for _, p := range sortedByIfIndex(snap.Ports) {
		if p.Physical {
			n++
			p.Unit, p.Number = 1, n
		}
	}
	snap.SortPorts()
	model, version := routerOSDescr(snap.System.Descr)
	unit := monitor.Unit{Number: 1, Model: model, Firmware: version}
	if v, err := r.Scalars(ctx, "mtxrSerialNumber"); err == nil {
		unit.Serial = v["mtxrSerialNumber"].String()
	}
	snap.Units = []monitor.Unit{unit}
	if err := QBridge(ctx, r, b); err != nil {
		snap.Notes = append(snap.Notes, "VLANs: "+err.Error())
	}
	if src, web := r.(interface{ SourceNotes() []string }); web {
		snap.Notes = append(snap.Notes, src.SourceNotes()...)
	} else if len(snap.VLANs) == 0 {
		snap.Notes = append(snap.Notes, "VLANs: RouterOS reports only each port's PVID over SNMP; choose the data source \"RouterOS (REST API)\" for VLANs, tagging, duplex and bonds")
	}
	return snap, nil
}

func sortedByIfIndex(ports []*monitor.Port) []*monitor.Port {
	out := append([]*monitor.Port(nil), ports...)
	sort.Slice(out, func(i, j int) bool { return out[i].IfIndex < out[j].IfIndex })
	return out
}

// routerOSDescr splits sysDescr "RouterOS CRS310-8G+2S+ 7.25.1" into board
// and version.
func routerOSDescr(descr string) (model, version string) {
	f := strings.Fields(descr)
	if len(f) == 0 || f[0] != "RouterOS" {
		return "", ""
	}
	switch len(f) {
	case 1:
		return "", ""
	case 2:
		return f[1], ""
	}
	return strings.Join(f[1:len(f)-1], " "), f[len(f)-1]
}
