package monitor

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// sysReader answers the system group and serves interface tables.
type sysReader struct{ fakeReader }

func (s *sysReader) Scalars(context.Context, ...string) (map[string]Value, error) {
	return map[string]Value{"sysName": {Kind: KindBytes, Bytes: []byte("sw")}}, nil
}

func TestCollectBaseLimits(t *testing.T) {
	ifs := Table{}
	for _, idx := range []int{0, 70000, -5} { // not plausible ifIndex values
		ifs[strconv.Itoa(idx)] = row("ifType", 6)
	}
	ifs["65535"] = row("ifType", 6)
	ifs["x"] = row("ifType", 6)
	b, err := CollectBase(context.Background(), &sysReader{fakeReader{tables: map[string]Table{"ifType": ifs}}}, BaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Snap.Ports) != 1 || b.Snap.Ports[0].IfIndex != 65535 {
		t.Fatalf("ports %+v", b.Snap.Ports)
	}

	many := Table{}
	for i := 1; i <= MaxPorts+100; i++ {
		many[strconv.Itoa(i)] = row("ifType", 6)
	}
	b, err = CollectBase(context.Background(), &sysReader{fakeReader{tables: map[string]Table{"ifType": many}}}, BaseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Snap.Ports) != MaxPorts || b.Snap.Ports[MaxPorts-1].IfIndex != MaxPorts {
		t.Fatalf("%d ports", len(b.Snap.Ports))
	}
	if len(b.Snap.Notes) != 1 || !strings.Contains(b.Snap.Notes[0], "more than") {
		t.Errorf("notes %v", b.Snap.Notes)
	}
}

func TestBitsMSBLimit(t *testing.T) {
	long := make([]byte, 10*MaxBitsOctets)
	for i := range long {
		long[i] = 0xff
	}
	if got := BitsMSB(long); len(got) != MaxBitsOctets*8 || got[len(got)-1] != MaxBitsOctets*8-1 {
		t.Fatalf("%d bits", len(got))
	}
}
