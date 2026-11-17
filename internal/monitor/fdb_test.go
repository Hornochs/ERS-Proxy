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
	return f.tables[names[0]], nil
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
