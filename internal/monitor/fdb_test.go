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

// TP-Link T1500G-8T (seen on hardware, 2026-11-17): Q-BRIDGE has port
// numbers, dot1dBasePortIfIndex and dot1dTpFdbPort have ifIndexes, and
// dot1dTpFdbTable is indexed by vlan.mac like Q-BRIDGE.
func TestFDBBridgeMIBWithVLANIndex(t *testing.T) {
	b := &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{49153: 49153, 49160: 49160}}
	r := &fakeReader{tables: map[string]Table{
		"dot1qTpFdbPort": {"100.2.0.0.0.1.1": row("dot1qTpFdbPort", 8), "7.2.0.0.0.1.2": row("dot1qTpFdbPort", 1)},
		"dot1dTpFdbPort": {"100.2.0.0.0.1.1": row("dot1dTpFdbPort", 49160), "7.2.0.0.0.1.2": row("dot1dTpFdbPort", 49153)},
	}}
	b.fdb(context.Background(), r)
	want := []FDBEntry{{MAC: "02:00:00:00:01:01", VLAN: 100, IfIndex: 49160}, {MAC: "02:00:00:00:01:02", VLAN: 7, IfIndex: 49153}}
	if fmt.Sprint(b.Snap.FDB) != fmt.Sprint(want) {
		t.Errorf("FDB %+v, want %+v", b.Snap.FDB, want)
	}
}

func TestFDBReadsOwnMACs(t *testing.T) {
	mac := func(b ...byte) Row { return Row{"ifPhysAddress": Value{Kind: KindBytes, Bytes: b}} }
	b := &Base{Snap: &Snapshot{Time: time.Now()}}
	r := &fakeReader{tables: map[string]Table{"ifPhysAddress": {
		"1":  mac(0x84, 0x83, 0x71, 0xf7, 0x54, 0x2f),
		"2":  mac(0x84, 0x83, 0x71, 0xf7, 0x54, 0x2f), // same MAC on two interfaces
		"3":  mac(0x84, 0x83, 0x71, 0xf7, 0x54, 0x80),
		"4":  mac(0, 0, 0, 0, 0, 0),          // loopback
		"5":  mac(),                          // no address
		"6":  mac(0x01, 0x80, 0xc2, 0, 0, 0), // multicast
		"7":  mac(1, 2, 3),                   // not a MAC
		"99": {"ifPhysAddress": Value{Kind: KindInt, Int: 5}},
	}}}
	b.fdb(context.Background(), r)
	if want := []string{"84:83:71:f7:54:2f", "84:83:71:f7:54:80"}; fmt.Sprint(b.Snap.OwnMACs) != fmt.Sprint(want) {
		t.Errorf("own MACs %v, want %v", b.Snap.OwnMACs, want)
	}
}

func TestZeroBaseMACIsEmpty(t *testing.T) {
	r := &scalarReader{v: map[string]Value{"dot1dBaseBridgeAddress": {Kind: KindBytes, Bytes: make([]byte, 6)}}}
	var sys System
	readIdentity(context.Background(), r, &sys)
	if sys.BaseMAC != "" {
		t.Errorf("base MAC %q, want empty", sys.BaseMAC)
	}
}

type scalarReader struct{ v map[string]Value }

func (s *scalarReader) Scalars(context.Context, ...string) (map[string]Value, error) { return s.v, nil }
func (s *scalarReader) Columns(context.Context, ...string) (Table, error)            { return Table{}, nil }

// VOSS over EDM (2026-11-17): Q-BRIDGE and BRIDGE-MIB read as empty, the
// RAPID-CITY table answers with vlan.mac and ifIndex values; self entries
// are left out.
func TestFDBRapidCityFallback(t *testing.T) {
	b := &Base{Snap: &Snapshot{Time: time.Now(), Ports: []*Port{{IfIndex: 239}}}, BridgePorts: map[int]int{48: 239}}
	r := &fakeReader{tables: map[string]Table{
		"rcBridgeTpFdbPort":   {"100.2.0.0.0.1.1": row("rcBridgeTpFdbPort", 239), "100.132.131.113.247.84.128": row("rcBridgeTpFdbPort", 111)},
		"rcBridgeTpFdbStatus": {"100.2.0.0.0.1.1": row("rcBridgeTpFdbStatus", 3), "100.132.131.113.247.84.128": row("rcBridgeTpFdbStatus", 4)},
	}}
	b.fdb(context.Background(), r)
	want := []FDBEntry{{MAC: "02:00:00:00:01:01", VLAN: 100, IfIndex: 239}}
	if fmt.Sprint(b.Snap.FDB) != fmt.Sprint(want) || len(b.Snap.Notes) != 0 {
		t.Errorf("FDB %+v notes %v, want %+v", b.Snap.FDB, b.Snap.Notes, want)
	}
	// Not read when Q-BRIDGE has entries.
	b = &Base{Snap: &Snapshot{Time: time.Now()}, BridgePorts: map[int]int{1: 101}}
	r.tables["dot1qTpFdbPort"] = Table{"1.2.0.0.0.1.9": row("dot1qTpFdbPort", 1)}
	b.fdb(context.Background(), r)
	if len(b.Snap.FDB) != 1 || b.Snap.FDB[0].IfIndex != 101 {
		t.Errorf("FDB %+v", b.Snap.FDB)
	}
}
