package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/monitor"
	"github.com/lan-dot-party/ers-proxy/internal/monitor/drivers"
	"github.com/lan-dot-party/ers-proxy/internal/secret"
	"github.com/lan-dot-party/ers-proxy/internal/snmp"
	"github.com/lan-dot-party/ers-proxy/internal/snmptest"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

type recSink struct{ samples, events int }

func (s *recSink) Samples(context.Context, int64, *monitor.Snapshot) error { s.samples++; return nil }
func (s *recSink) Events(context.Context, int64, []monitor.Event) error    { s.events++; return nil }

func TestPollerAgainstSimulator(t *testing.T) {
	target := snmptest.Simulator(t, snmptest.DataDir(t), "ers5600")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, log); err != nil {
		t.Fatal(err)
	}
	key, _ := secret.LoadOrCreate(filepath.Join(dir, "k"))
	d := &store.Device{Name: "stack", Kind: store.KindERS, URL: "http://127.0.0.1", OpenMode: store.OpenInherit,
		Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto, MonitorSource: store.MonitorSNMP,
		MonitorDriver: store.DriverAuto, MonitorInterval: 60, SNMPVersion: store.SNMPv2c, SNMPPort: target.Port}
	if err := db.CreateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	enc, err := devices.EncryptSNMP(key, d, snmp.Credentials{Community: "ers5600"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceSNMPCredentials(ctx, d.ID, enc); err != nil {
		t.Fatal(err)
	}
	// An unmonitored device must not get a loop.
	if err := db.CreateDevice(ctx, &store.Device{Name: "link", Kind: store.KindLink, URL: "https://x.example/",
		OpenMode: store.OpenInherit, Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto, MonitorSource: store.MonitorNone}); err != nil {
		t.Fatal(err)
	}

	p := New(db, key, monitor.NewRegistry(drivers.All()...), log)
	sink := &recSink{}
	p.Sink = sink
	go p.Run(ctx)
	deadline := time.Now().Add(20 * time.Second)
	var st Status
	for time.Now().Before(deadline) {
		if s, ok := p.Status(d.ID); ok && (s.Snap != nil || s.Err != "") {
			st = s
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st.Snap == nil {
		t.Fatalf("no snapshot: %+v", st)
	}
	if st.Driver != "ers" || st.Snap.Port(129) == nil || st.Snap.Port(129).Unit != 2 {
		t.Fatalf("status %+v", st)
	}
	if len(p.All()) != 1 {
		t.Fatalf("only the monitored device should be polled: %d", len(p.All()))
	}
	// A rename shows up although the loop keeps running.
	d.Name = "renamed"
	if err := db.UpdateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	p.Trigger()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.All()[0].Name != "renamed" {
		time.Sleep(100 * time.Millisecond)
	}
	if p.All()[0].Name != "renamed" {
		t.Fatalf("dashboard keeps the old name: %q", p.All()[0].Name)
	}
	// Changing the device restarts its loop; turning monitoring off removes it.
	d.MonitorSource = store.MonitorNone
	if err := db.UpdateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	p.Trigger()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(p.All()) != 0 {
		time.Sleep(100 * time.Millisecond)
	}
	if len(p.All()) != 0 {
		t.Fatal("loop not stopped after monitoring was turned off")
	}
}

func TestFDBState(t *testing.T) {
	var f fdbState
	now := time.Now()
	if !f.due(now) {
		t.Error("first poll must read the FDB")
	}
	f.requested = now
	if f.due(now.Add(fdbInterval-time.Second)) || !f.due(now.Add(fdbInterval)) {
		t.Error("FDB interval")
	}
	first := &monitor.Snapshot{FDB: []monitor.FDBEntry{{MAC: "02:00:00:00:00:01", IfIndex: 1}}, FDBTime: now}
	f.carry(first)
	second := &monitor.Snapshot{}
	f.carry(second)
	if len(second.FDB) != 1 || !second.FDBTime.Equal(now) {
		t.Errorf("FDB not carried over: %+v", second)
	}
	newer := &monitor.Snapshot{FDBTime: now.Add(time.Minute)}
	f.carry(newer)
	third := &monitor.Snapshot{}
	f.carry(third)
	if !third.FDBTime.Equal(newer.FDBTime) || len(third.FDB) != 0 {
		t.Errorf("newest FDB not kept: %+v", third)
	}
}

// A panic while polling one device is reported as a poll error; the loop keeps
// running and polls again.
func TestPollPanicKeepsLoopRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := db.Migrate(ctx, log); err != nil {
		t.Fatal(err)
	}
	key, _ := secret.LoadOrCreate(filepath.Join(dir, "k"))
	d := &store.Device{Name: "bad", Kind: store.KindERS, URL: "http://127.0.0.1", OpenMode: store.OpenInherit,
		Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto, MonitorSource: store.MonitorEDM,
		MonitorDriver: store.DriverAuto, MonitorInterval: 60}
	if err := db.CreateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	p := New(db, key, monitor.NewRegistry(drivers.All()...), log)
	var calls atomic.Int32
	p.OpenEDM = func(context.Context, *store.Device) (monitor.Reader, func(), error) {
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return nil, nil, errors.New("second poll")
	}
	go p.Run(ctx)
	waitErr := func(want string, poke bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if s, ok := p.Status(d.ID); ok && strings.Contains(s.Err, want) {
				return
			}
			if poke {
				p.PollNow(d.ID)
			}
			time.Sleep(100 * time.Millisecond)
		}
		s, _ := p.Status(d.ID)
		t.Fatalf("no %q error: %+v", want, s)
	}
	waitErr("panicked", false)
	waitErr("second poll", true)
}
