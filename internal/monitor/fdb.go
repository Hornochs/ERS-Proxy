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

// fdb reads dot1qTpFdbPort (index fdbId.mac, fdbId taken as VLAN ID) and, when
// that is empty, dot1dTpFdbPort (index mac). Failures only add a note.
func (b *Base) fdb(ctx context.Context, r Reader) {
	var entries []FDBEntry
	ok := false
	for _, col := range []string{"dot1qTpFdbPort", "dot1dTpFdbPort"} {
		t, err := r.Columns(ctx, col)
		if err != nil {
			b.Snap.Notes = append(b.Snap.Notes, "MAC address table: "+err.Error())
			continue
		}
		ok = true
		for idx, row := range t {
			parts := Ints(idx)
			vlan := 0
			if col == "dot1qTpFdbPort" {
				if len(parts) != 7 {
					continue
				}
				vlan, parts = parts[0], parts[1:]
			}
			if len(parts) != 6 {
				continue
			}
			mac := make([]byte, 6)
			valid := true
			for i, x := range parts {
				if x < 0 || x > 255 {
					valid = false
					break
				}
				mac[i] = byte(x)
			}
			bp, has := row[col].AsInt()
			if !valid || !has {
				continue
			}
			ifIndex, known := b.BridgePorts[bp]
			if !known {
				continue
			}
			entries = append(entries, FDBEntry{MAC: FormatMAC(mac), VLAN: vlan, IfIndex: ifIndex})
		}
		if len(entries) > 0 {
			break
		}
	}
	if !ok {
		return
	}
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
