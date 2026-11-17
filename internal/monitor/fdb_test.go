package monitor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeReader struct {
	tables map[string]Table
	err    error
}

func (f *fakeReader) Scalars(context.Context, ...string) (map[string]Value, error) {
	return map[string]Value{}, nil
}

func (f *fakeReader) Columns(_ context.Context, names ...string) (Table, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := Table{}
	for _, n := range names {
		for idx, r := range f.tables[n] {
			if out[idx] == nil {
				out[idx] = Row{}
			}
			for k, v := range r {
				out[idx][k] = v
			}
		}
	}
	return out, nil
}

func row(col string, n int) Row { return Row{col: Value{Kind: KindInt, Int: int64(n)}} }

func TestFDBContext(t *testing.T) {
	if WantFDB(context.Background()) || !WantFDB(WithFDB(context.Background())) {
		t.Error("WithFDB/WantFDB")
	}
}

func TestFDBFallbackAndUnknownPorts(t *testing.T) {
	b := &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{1: 101}}
	r := &fakeReader{tables: map[string]Table{
		"dot1dTpFdbPort": {"2.0.0.0.1.1": row("dot1dTpFdbPort", 1), "2.0.0.0.1.2": row("dot1dTpFdbPort", 9), "1.2": row("dot1dTpFdbPort", 1)},
	}}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 1 || b.Snap.FDB[0] != (FDBEntry{MAC: "02:00:00:00:01:01", IfIndex: 101}) || b.Snap.FDBTime.IsZero() {
		t.Errorf("FDB %+v", b.Snap.FDB)
	}
}

func TestFDBCapAndError(t *testing.T) {
	b := &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{1: 1}}
	tab := Table{}
	for i := 0; i < maxFDB+5; i++ {
		tab[fmt.Sprintf("1.0.0.%d.%d.%d.1", i>>16, (i>>8)&255, i&255)] = row("dot1qTpFdbPort", 1)
	}
	b.fdb(context.Background(), &fakeReader{tables: map[string]Table{"dot1qTpFdbPort": tab}})
	if len(b.Snap.FDB) != maxFDB || len(b.Snap.Notes) != 1 {
		t.Errorf("%d entries, notes %v", len(b.Snap.FDB), b.Snap.Notes)
	}
	b = &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{1: 1}}
	b.fdb(context.Background(), &fakeReader{err: errors.New("timeout")})
	if len(b.Snap.FDB) != 0 || !b.Snap.FDBTime.IsZero() || len(b.Snap.Notes) == 0 {
		t.Errorf("error case: %+v", b.Snap)
	}
}

// The cases below follow LANLeecher's FDB reader, which was checked on
// TP-Link JetStream and Omada switches.

func TestFDBStatusAndSpecialAddresses(t *testing.T) {
	b := &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{1: 101}}
	r := &fakeReader{tables: map[string]Table{
		"dot1qTpFdbPort": {
			"1.2.0.0.0.0.1":  row("dot1qTpFdbPort", 1), // learned
			"1.2.0.0.0.0.2":  row("dot1qTpFdbPort", 1), // self
			"1.2.0.0.0.0.3":  row("dot1qTpFdbPort", 1), // mgmt
			"1.2.0.0.0.0.4":  row("dot1qTpFdbPort", 1), // invalid
			"1.1.0.94.0.0.1": row("dot1qTpFdbPort", 1), // multicast
			"1.0.0.0.0.0.0":  row("dot1qTpFdbPort", 1), // zero
			"1.2.0.0.0.0.5":  row("dot1qTpFdbPort", 0), // no port
		},
		"dot1qTpFdbStatus": {
			"1.2.0.0.0.0.1": row("dot1qTpFdbStatus", 3),
			"1.2.0.0.0.0.2": row("dot1qTpFdbStatus", 4),
			"1.2.0.0.0.0.3": row("dot1qTpFdbStatus", 5),
			"1.2.0.0.0.0.4": row("dot1qTpFdbStatus", 2),
		},
	}}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 1 || b.Snap.FDB[0] != (FDBEntry{MAC: "02:00:00:00:00:01", VLAN: 1, IfIndex: 101}) {
		t.Errorf("FDB %+v", b.Snap.FDB)
	}
}

func TestFDBJetStreamPrefersBridgeMIB(t *testing.T) {
	// Q-BRIDGE reports port numbers 1–8, dot1dBasePortIfIndex and
	// dot1dTpFdbPort use 49153….
	b := &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{49153: 49153, 49154: 49154}}
	r := &fakeReader{tables: map[string]Table{
		"dot1qTpFdbPort": {"1.2.0.0.0.0.1": row("dot1qTpFdbPort", 1), "1.2.0.0.0.0.2": row("dot1qTpFdbPort", 2)},
		"dot1dTpFdbPort": {"2.0.0.0.0.1": row("dot1dTpFdbPort", 49153), "2.0.0.0.0.2": row("dot1dTpFdbPort", 49154)},
	}}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 2 || b.Snap.FDB[0].IfIndex != 49153 || b.Snap.FDB[1].IfIndex != 49154 {
		t.Errorf("FDB %+v", b.Snap.FDB)
	}
	// Q-BRIDGE with fewer unknown ports than BRIDGE-MIB stays.
	r.tables["dot1qTpFdbPort"]["1.2.0.0.0.0.3"] = row("dot1qTpFdbPort", 49153)
	r.tables["dot1dTpFdbPort"] = Table{"2.0.0.0.0.1": row("dot1dTpFdbPort", 7), "2.0.0.0.0.2": row("dot1dTpFdbPort", 8), "2.0.0.0.0.3": row("dot1dTpFdbPort", 9)}
	b = &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{49153: 49153}}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 1 || b.Snap.FDB[0].VLAN != 1 || len(b.Snap.Notes) != 1 {
		t.Errorf("FDB %+v notes %v", b.Snap.FDB, b.Snap.Notes)
	}
}

func TestFDBWithoutBridgePortTable(t *testing.T) {
	// No dot1dBasePortIfIndex: the bridge port is the ifIndex, if the switch
	// has that interface.
	b := &Base{Snap: &Snapshot{Time: time.Now(), Ports: []*Port{{IfIndex: 3}}}, BridgePorts: map[int]int{}}
	r := &fakeReader{tables: map[string]Table{
		"dot1qTpFdbPort": {"1.2.0.0.0.0.1": row("dot1qTpFdbPort", 3), "1.2.0.0.0.0.2": row("dot1qTpFdbPort", 77)},
	}}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 1 || b.Snap.FDB[0].IfIndex != 3 {
		t.Errorf("FDB %+v", b.Snap.FDB)
	}
}
