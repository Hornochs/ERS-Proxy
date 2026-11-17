package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lan-dot-party/ers-proxy/internal/monitor"
	"github.com/lan-dot-party/ers-proxy/internal/monitor/poller"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

type mapMonitor []poller.Status

func (m mapMonitor) All() []poller.Status { return m }
func (m mapMonitor) Status(id int64) (poller.Status, bool) {
	for _, s := range m {
		if s.DeviceID == id {
			return s, true
		}
	}
	return poller.Status{}, false
}

func pngBytes(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (h *harness) upload(path, field, name string, data []byte) *http.Response {
	h.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("_csrf", h.csrf)
	fw, _ := mw.CreateFormFile(field, name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", h.srv.URL)
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func TestMaps(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	h.user("bob", store.RoleUser)
	ctx := context.Background()
	mk := func(name string) *store.Device {
		d := &store.Device{Name: name, Kind: store.KindLink, URL: "https://" + name + ".example/", OpenMode: store.OpenInherit,
			Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto}
		if err := h.db.CreateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b, c := mk("core"), mk("access"), mk("spare")
	// core port 49 ↔ access port 1 over LLDP, 1 Gbit/s, 300/100 Mbit/s.
	snapA := &monitor.Snapshot{System: monitor.System{Name: "core", ChassisID: "02:00:00:00:00:0a"},
		Ports: []*monitor.Port{{IfIndex: 49, Name: "1/49", Physical: true, Speed: 1_000_000_000, Oper: "up",
			InBps: 100e6, OutBps: 300e6, Neighbor: &monitor.Neighbor{SysName: "access", ChassisID: "02:00:00:00:00:0b", PortID: "1", Bridge: true}}}}
	snapB := &monitor.Snapshot{System: monitor.System{Name: "access", ChassisID: "02:00:00:00:00:0b"},
		Ports: []*monitor.Port{{IfIndex: 1, Name: "1", Physical: true, Speed: 1_000_000_000, Oper: "up",
			InBps: 300e6, OutBps: 100e6, Neighbor: &monitor.Neighbor{SysName: "core", ChassisID: "02:00:00:00:00:0a", PortID: "1/49", Bridge: true}}}}
	h.ui.Monitor = mapMonitor{
		{DeviceID: a.ID, Name: "core", LastPoll: time.Now(), Snap: snapA},
		{DeviceID: b.ID, Name: "access", LastPoll: time.Now(), Snap: snapB},
	}

	// Users view, but cannot create.
	h.login("bob")
	if resp, _ := h.get("/maps"); resp.StatusCode != 200 {
		t.Fatalf("maps page for users: %d", resp.StatusCode)
	}
	if resp := h.post("/maps", url.Values{"_csrf": {h.csrf}, "name": {"x"}}, map[string]string{"Origin": h.srv.URL}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user created a map: %d", resp.StatusCode)
	}

	h.client.Jar, _ = newJar()
	h.login("admin")
	resp := h.post("/maps", url.Values{"_csrf": {h.csrf}, "name": {"Hall 1"}}, map[string]string{"Origin": h.srv.URL})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	maps, _ := h.db.ListMaps(ctx)
	if len(maps) != 1 {
		t.Fatalf("maps %v", maps)
	}
	m := maps[0]
	base := fmt.Sprintf("/maps/%d", m.ID)
	// The workspace sidebar lists the map, to open it as a tab.
	if _, body := h.get("/"); !strings.Contains(body, fmt.Sprintf(`data-map="%d"`, m.ID)) {
		t.Error("workspace does not list the map")
	}

	// Background: PNG accepted, SVG and junk refused.
	if r := h.upload(base+"/background", "image", "plan.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("svg accepted: %d", r.StatusCode)
	}
	big := append(pngBytes(t, 10, 10), make([]byte, 9<<20)...)
	if r := h.upload(base+"/background", "image", "big.png", big); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("9 MiB image: %d", r.StatusCode)
	}
	huge := make([]byte, 17<<20)
	if r := h.upload(base+"/background", "image", "huge.png", huge); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("17 MiB body: %d", r.StatusCode)
	}
	if r := h.upload(base+"/background", "image", "x.png", []byte("not an image")); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("junk accepted: %d", r.StatusCode)
	}
	if r := h.upload(base+"/background", "image", "plan.png", pngBytes(t, 800, 500)); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("png refused: %d", r.StatusCode)
	}
	r, body := h.get(base + "/background")
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(body, "\x89PNG") {
		t.Fatalf("background: %d %v", r.StatusCode, r.Header)
	}

	// Nodes as JSON; the CSRF token is required.
	nodes := fmt.Sprintf(`[{"id":%d,"x":0.2,"y":0.5},{"id":%d,"x":0.8,"y":0.5},{"id":%d,"x":0.5,"y":0.9}]`, a.ID, b.ID, c.ID)
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+base+"/nodes", strings.NewReader(nodes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.srv.URL)
	if r, _ := h.client.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("nodes without CSRF token: %d", r.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, h.srv.URL+base+"/nodes", strings.NewReader(nodes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.srv.URL)
	// An empty map has empty arrays, not null (map.js iterates them).
	if _, body := h.get(base + "/state.json"); !strings.Contains(body, `"nodes":[]`) || !strings.Contains(body, `"links":[]`) {
		t.Errorf("empty map: %s", body)
	}
	req.Header.Set("X-CSRF-Token", h.csrf)
	if r, _ := h.client.Do(req); r.StatusCode != http.StatusNoContent {
		t.Fatalf("nodes: %d", r.StatusCode)
	}

	state := func() mapStateJSON {
		t.Helper()
		_, body := h.get(base + "/state.json")
		var st mapStateJSON
		if err := json.Unmarshal([]byte(body), &st); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		return st
	}
	st := state()
	if st.Map.Width != 800 || st.Map.Height != 500 || st.Map.BG == "" || len(st.Nodes) != 3 {
		t.Fatalf("state %+v", st)
	}
	if len(st.Links) != 1 {
		t.Fatalf("links %+v", st.Links)
	}
	l := st.Links[0]
	if l.Source != "lldp" || !l.Rates || l.Util < 29 || l.Util > 31 {
		t.Fatalf("link %+v", l)
	}
	// AB is from A to B, whichever device is A.
	if (l.A.Device == a.ID && (l.AB != 300e6 || l.BA != 100e6)) || (l.A.Device == b.ID && (l.AB != 100e6 || l.BA != 300e6)) {
		t.Fatalf("rates %+v", l)
	}
	// Hide the automatic link, add a manual one to the spare device.
	hdr := map[string]string{"Origin": h.srv.URL, "X-CSRF-Token": h.csrf}
	if r := h.post(base+"/links", url.Values{"kind": {"hidden"}, "a_device": {fmt.Sprint(l.A.Device)}, "a_if": {fmt.Sprint(l.A.IfIndex)},
		"b_device": {fmt.Sprint(l.B.Device)}, "b_if": {fmt.Sprint(l.B.IfIndex)}}, hdr); r.StatusCode != http.StatusNoContent {
		t.Fatalf("hide: %d", r.StatusCode)
	}
	if r := h.post(base+"/links", url.Values{"kind": {"manual"}, "a_device": {fmt.Sprint(a.ID)}, "a_if": {"49"},
		"b_device": {fmt.Sprint(c.ID)}, "b_if": {"0"}}, hdr); r.StatusCode != http.StatusNoContent {
		t.Fatalf("manual: %d", r.StatusCode)
	}
	st = state()
	if len(st.Links) != 1 || st.Links[0].Source != "manual" || len(st.Hidden) != 1 {
		t.Fatalf("after hide/manual: links %+v hidden %+v", st.Links, st.Hidden)
	}
	if r := h.post(fmt.Sprintf("%s/links/%d/delete", base, st.Hidden[0].ID), url.Values{}, hdr); r.StatusCode != http.StatusNoContent {
		t.Fatalf("unhide: %d", r.StatusCode)
	}
	if st = state(); len(st.Links) != 2 {
		t.Fatalf("after unhide: %+v", st.Links)
	}
	// Users see the state without the editor data.
	h.client.Jar, _ = newJar()
	h.login("bob")
	if st = state(); len(st.Links) != 2 || st.Devices != nil || st.Hidden != nil || st.Nodes[0].Ports != nil {
		t.Fatalf("user state %+v", st)
	}
}

func newJar() (http.CookieJar, error) { return cookiejar.New(nil) }
