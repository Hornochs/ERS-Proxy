package monitor

import (
	"context"
	"fmt"
	"sort"
)

// maxFDB caps the number of address table entries kept in a snapshot.
const maxFDB = 20000

type fdbKey struct{}

// WithFDB asks CollectBase to read the learned MAC address table as well. The
// table can be large, so the poller requests it only every few minutes.
func WithFDB(ctx context.Context) context.Context { return context.WithValue(ctx, fdbKey{}, true) }

// WantFDB reports whether ctx asks for the address table.
func WantFDB(ctx context.Context) bool {
	v, _ := ctx.Value(fdbKey{}).(bool)
	return v
}

// FDBEntry is one learned MAC address.
type FDBEntry struct {
	MAC     string // aa:bb:cc:dd:ee:ff
	VLAN    int    // 0 when the agent has no VLAN-aware table
	IfIndex int
}

// FDB entry status values (dot1qTpFdbStatus / dot1dTpFdbStatus) that are
// not addresses learned from the network: invalid, the switch's own address
// and management (static) entries.
const (
	fdbStatusInvalid = 2
	fdbStatusSelf    = 4
	fdbStatusMgmt    = 5
)

// fdbTable is what one of the two address tables gave.
type fdbTable struct {
	entries  []FDBEntry
	unmapped int // entries on bridge ports that dot1dBasePortIfIndex does not list
	err      error
}

// fdb reads dot1qTpFdbPort (index fdbId.mac, fdbId taken as VLAN ID) and
// dot1dTpFdbPort (index mac). Q-BRIDGE is used unless it is empty or has
// entries on unknown bridge ports; then BRIDGE-MIB is read as well and the
// table with fewer unknown ports wins (TP-Link JetStream reports port numbers
// in Q-BRIDGE but ifIndexes in dot1dBasePortIfIndex). Failures only add a note.
// The rules follow LANLeecher's FDB reader, which was checked on hardware.
func (b *Base) fdb(ctx context.Context, r Reader) {
	q := b.fdbTable(ctx, r, "dot1qTpFdbPort", "dot1qTpFdbStatus")
	best := q
	if q.err != nil || len(q.entries) == 0 || q.unmapped > 0 {
		d := b.fdbTable(ctx, r, "dot1dTpFdbPort", "dot1dTpFdbStatus")
		qOK := q.err == nil && len(q.entries) > 0
		if d.err == nil && len(d.entries) > 0 && (!qOK || d.unmapped < q.unmapped) {
			best = d
		} else if !qOK && q.err != nil && d.err == nil {
			best = d // Q-BRIDGE failed, BRIDGE-MIB answered (maybe empty)
		}
		for _, t := range []fdbTable{q, d} {
			if t.err != nil {
				b.Snap.Notes = append(b.Snap.Notes, "MAC address table: "+t.err.Error())
			}
		}
		if q.err != nil && d.err != nil {
			return
		}
	}
	if best.unmapped > 0 {
		b.Snap.Notes = append(b.Snap.Notes, fmt.Sprintf("MAC address table: %d entries on unknown bridge ports ignored", best.unmapped))
	}
	entries := best.entries
	sort.Slice(entries, func(i, j int) bool {
		a, c := entries[i], entries[j]
		if a.MAC != c.MAC {
			return a.MAC < c.MAC
		}
		if a.VLAN != c.VLAN {
			return a.VLAN < c.VLAN
		}
		return a.IfIndex < c.IfIndex
	})
	if len(entries) > maxFDB {
		b.Snap.Notes = append(b.Snap.Notes, fmt.Sprintf("MAC address table: only the first %d of %d entries are kept", maxFDB, len(entries)))
		entries = entries[:maxFDB]
	}
	b.Snap.FDB = entries
	b.Snap.FDBTime = b.Snap.Time
}

// fdbTable reads one address table. Entries with status invalid, self or
// mgmt, multicast and all-zero addresses are left out. A bridge port is
// mapped through dot1dBasePortIfIndex; when the switch has no such table, the
// bridge port is taken as ifIndex if the switch has that interface.
func (b *Base) fdbTable(ctx context.Context, r Reader, portCol, statusCol string) fdbTable {
	t, err := r.Columns(ctx, portCol, statusCol)
	if err != nil {
		return fdbTable{err: err}
	}
	var out fdbTable
	for idx, row := range t {
		parts := Ints(idx)
		vlan := 0
		if portCol == "dot1qTpFdbPort" {
			if len(parts) != 7 {
				continue
			}
			vlan, parts = parts[0], parts[1:]
		}
		if len(parts) != 6 {
			continue
		}
		mac := make([]byte, 6)
		valid, zero := true, true
		for i, x := range parts {
			if x < 0 || x > 255 {
				valid = false
				break
			}
			mac[i] = byte(x)
			zero = zero && x == 0
		}
		bp, has := row[portCol].AsInt()
		if !valid || zero || mac[0]&1 == 1 || !has || bp == 0 {
			continue
		}
		if st, ok := row[statusCol].AsInt(); ok && (st == fdbStatusInvalid || st == fdbStatusSelf || st == fdbStatusMgmt) {
			continue
		}
		ifIndex, known := b.BridgePorts[bp]
		if !known {
			if len(b.BridgePorts) > 0 || b.Snap.Port(bp) == nil {
				out.unmapped++
				continue
			}
			ifIndex = bp
		}
		out.entries = append(out.entries, FDBEntry{MAC: FormatMAC(mac), VLAN: vlan, IfIndex: ifIndex})
	}
	return out
}
