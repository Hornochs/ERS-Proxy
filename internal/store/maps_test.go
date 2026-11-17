package store

import (
	"context"
	"errors"
	"testing"
)

func TestMaps(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()
	a := &Device{Name: "a", Kind: KindERS, URL: "http://192.0.2.1", OpenMode: OpenInherit, Isolation: IsolationSandbox, TLSMode: TLSAuto}
	b := &Device{Name: "b", Kind: KindERS, URL: "http://192.0.2.2", OpenMode: OpenInherit, Isolation: IsolationSandbox, TLSMode: TLSAuto}
	for _, d := range []*Device{a, b} {
		if err := db.CreateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	m := &Map{Name: "Hall 1"}
	if err := db.CreateMap(ctx, m); err != nil || m.Refresh != 15 {
		t.Fatalf("create: %v %+v", err, m)
	}
	if err := db.CreateMap(ctx, &Map{Name: "hall 1"}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if err := db.CreateMap(ctx, &Map{Name: "x", Refresh: 1}); err == nil {
		t.Fatal("refresh 1 s accepted")
	}
	if err := db.SetMapBackground(ctx, m.ID, []byte("png bytes"), "image/png", 800, 600); err != nil {
		t.Fatal(err)
	}
	got, err := db.MapByID(ctx, m.ID)
	if err != nil || !got.HasBackground() || got.BGWidth != 800 || got.BGType != "image/png" {
		t.Fatalf("map %+v %v", got, err)
	}
	data, typ, hash, err := db.MapBackground(ctx, m.ID)
	if err != nil || string(data) != "png bytes" || typ != "image/png" || hash != got.BGHash {
		t.Fatalf("background %q %s %s %v", data, typ, hash, err)
	}
	if err := db.SetMapNodes(ctx, m.ID, []MapNode{{a.ID, 0.25, 0.5}, {b.ID, 1.5, -1}}); err != nil {
		t.Fatal(err)
	}
	nodes, _ := db.MapNodes(ctx, m.ID)
	if len(nodes) != 2 || nodes[1].X != 1 || nodes[1].Y != 0 {
		t.Fatalf("nodes %+v", nodes)
	}
	if err := db.SetMapNodes(ctx, m.ID, []MapNode{{a.ID, 0.1, 0.1}, {9999, 0.5, 0.5}}); err == nil {
		t.Fatal("unknown device accepted")
	}
	if nodes, _ := db.MapNodes(ctx, m.ID); len(nodes) != 2 {
		t.Fatalf("failed update changed the nodes: %+v", nodes)
	}
	l := &MapLink{Kind: MapLinkManual, ADevice: a.ID, AIf: 1, BDevice: b.ID, BIf: 2}
	if err := db.AddMapLink(ctx, m.ID, l); err != nil {
		t.Fatal(err)
	}
	if !l.Same(b.ID, 2, a.ID, 0) || l.Same(a.ID, 3, b.ID, 2) {
		t.Fatal("Same")
	}
	if err := db.AddMapLink(ctx, m.ID, &MapLink{Kind: "other", ADevice: a.ID, BDevice: b.ID}); err == nil {
		t.Fatal("bad kind accepted")
	}
	// Deleting a device removes its nodes and links.
	if err := db.DeleteDevice(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	nodes, _ = db.MapNodes(ctx, m.ID)
	links, _ := db.MapLinks(ctx, m.ID)
	if len(nodes) != 1 || len(links) != 0 {
		t.Fatalf("after delete: %+v %+v", nodes, links)
	}
	if err := db.DeleteMap(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MapByID(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted map: %v", err)
	}
}
