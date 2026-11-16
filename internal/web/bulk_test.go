package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

const bulkPassword = "correct horse battery"

func (h *harness) bulkForm(extra map[string]string, rows ...[4]string) url.Values {
	v := url.Values{"_csrf": {h.csrf}, "kind": {"ers-edm"}, "group_id": {"0"}, "open_mode": {"inherit"},
		"monitor_source": {"none"}, "monitor_driver": {"auto"}, "monitor_interval": {"60"}, "snmp_port": {"161"},
		"health_enabled": {"1"}}
	for k, x := range extra {
		v.Set(k, x)
	}
	for _, r := range rows {
		v.Add("row_name", r[0])
		v.Add("row_url", r[1])
		v.Add("row_kind", r[2])
		v.Add("row_group", r[3])
	}
	return v
}

func (h *harness) bulkPost(form url.Values) (*http.Response, string) {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/admin/devices/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.srv.URL)
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, sb.String()
}

func adminHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	h.login("admin")
	return h
}

func deviceCount(h *harness) int {
	ds, _ := h.db.ListDevices(context.Background())
	return len(ds)
}

func TestBulkCreate(t *testing.T) {
	h := adminHarness(t)
	ctx := context.Background()
	g := &store.Group{Name: "Hall"}
	if err := h.db.CreateGroup(ctx, g); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/admin/devices/bulk"); !strings.Contains(body, `id="bulk-rows"`) || strings.Count(body, `name="row_name"`) != 6 ||
		!strings.Contains(body, `name="community"`) || !strings.Contains(body, `id="bulk-row"`) {
		t.Fatal("bulk page incomplete")
	}
	if _, body := h.get("/admin/devices"); !strings.Contains(body, "/admin/devices/bulk") {
		t.Fatal("no bulk button on the device list")
	}
	form := h.bulkForm(map[string]string{"community": "c0mm", "login_username": "RW", "login_password": "secure",
		"confirm_password": bulkPassword},
		[4]string{"sw1", "https://192.0.2.1", "", ""},
		[4]string{"", "", "", ""},
		[4]string{"sw2", "192.0.2.2", "vsp-edm", itoa(g.ID)},
		[4]string{"Docs", "https://docs.example/", "link", "0"})
	resp, body := h.bulkPost(form)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "ok=bulk") {
		t.Fatalf("status %d %q %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	ds, _ := h.db.ListDevices(ctx)
	if len(ds) != 3 {
		t.Fatalf("%d devices", len(ds))
	}
	for _, d := range ds {
		if !d.HasSNMPCredentials() {
			t.Errorf("%s: no SNMP credentials", d.Name)
		}
		if c, err := devices.DecryptSNMP(h.ui.Key, d); err != nil || c.Community != "c0mm" {
			t.Errorf("%s: SNMP %v", d.Name, err)
		}
		switch d.Name {
		case "Docs":
			if d.HasCredentials() || d.Kind != "link" {
				t.Errorf("link: %+v", d)
			}
		case "sw2":
			if d.GroupID != g.ID || d.Kind != "vsp-edm" || !d.HasCredentials() || !d.AutoLogin {
				t.Errorf("sw2: %+v", d)
			}
		default:
			if !d.HasCredentials() || d.Kind != "ers-edm" || d.GroupID != 0 {
				t.Errorf("%s: %+v", d.Name, d)
			}
			if c, err := devices.DecryptCredentials(h.ui.Key, d); err != nil || c.Username != "RW" || c.Password != "secure" {
				t.Errorf("%s: login %v", d.Name, err)
			}
		}
	}
	audit, _ := h.db.ListAudit(ctx, 50)
	counts := map[string]int{}
	for _, a := range audit {
		counts[a.Action]++
		if strings.Contains(a.Details, "c0mm") || strings.Contains(a.Details, "secure") {
			t.Errorf("secret in audit: %+v", a)
		}
	}
	if counts["device.bulk_create"] != 1 || counts["device.create"] != 3 || counts["device.credentials.set"] != 2 || counts["device.snmp.set"] != 3 {
		t.Fatalf("audit %v", counts)
	}
}

func TestBulkInvalidRowCreatesNothing(t *testing.T) {
	h := adminHarness(t)
	form := h.bulkForm(map[string]string{"community": "c0mm", "login_username": "RW", "login_password": "secure",
		"confirm_password": bulkPassword},
		[4]string{"good-one", "https://192.0.2.1", "", ""},
		[4]string{"bad", "ht!tp://nope", "", ""})
	resp, body := h.bulkPost(form)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if deviceCount(h) != 0 {
		t.Fatal("devices created despite an error")
	}
	for _, want := range []string{`value="good-one"`, `value="bad"`, `value="ht!tp://nope"`, "Row 2", "field-error"} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %q", want)
		}
	}
	if strings.Contains(body, "Row 1:") {
		t.Error("row 1 flagged")
	}
	for _, secret := range []string{"c0mm", "secure", bulkPassword} {
		if strings.Contains(body, secret) {
			t.Errorf("response echoes %q", secret)
		}
	}
}

func TestBulkDuplicateAndExisting(t *testing.T) {
	h := adminHarness(t)
	ctx := context.Background()
	d := &store.Device{Name: "Old", Kind: "link", URL: "https://old.example/", OpenMode: "inherit", Isolation: "sandbox", TLSMode: "auto",
		MonitorSource: "none", MonitorDriver: "auto", MonitorInterval: 60, SNMPVersion: "v2c", SNMPPort: 161}
	if err := h.db.CreateDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	_, body := h.bulkPost(h.bulkForm(map[string]string{"kind": "link"},
		[4]string{"A", "https://a.example/", "", ""},
		[4]string{"a", "https://a.example/", "", ""},
		[4]string{"OLD", "https://old.example/", "", ""}))
	if !strings.Contains(body, "appear twice") || !strings.Contains(body, "already exists") {
		t.Fatalf("missing duplicate/exists errors")
	}
	if deviceCount(h) != 1 {
		t.Fatal("devices created despite errors")
	}
}

func TestBulkCredentialsNeedPassword(t *testing.T) {
	h := adminHarness(t)
	for _, pw := range []string{"", "wrong"} {
		extra := map[string]string{"login_username": "RW", "login_password": "secure"}
		if pw != "" {
			extra["confirm_password"] = pw
		}
		resp, body := h.bulkPost(h.bulkForm(extra, [4]string{"x", "https://192.0.2.1", "", ""}))
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "needs your password") {
			t.Fatalf("pw %q: status %d", pw, resp.StatusCode)
		}
	}
	// SNMP alone needs it too.
	if resp, _ := h.bulkPost(h.bulkForm(map[string]string{"community": "c"}, [4]string{"x", "https://192.0.2.1", "", ""})); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("SNMP without password: %d", resp.StatusCode)
	}
	// Half a login.
	if resp, _ := h.bulkPost(h.bulkForm(map[string]string{"login_username": "RW", "confirm_password": bulkPassword}, [4]string{"x", "https://192.0.2.1", "", ""})); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("half login: %d", resp.StatusCode)
	}
	if deviceCount(h) != 0 {
		t.Fatal("devices created")
	}
	// Without credentials no password is needed.
	if resp, _ := h.bulkPost(h.bulkForm(nil, [4]string{"x", "https://192.0.2.1", "", ""})); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("plain create: %d", resp.StatusCode)
	}
}

func TestBulkEmptyAndLimit(t *testing.T) {
	h := adminHarness(t)
	resp, body := h.bulkPost(h.bulkForm(nil, [4]string{"", "", "", ""}, [4]string{"", "", "", ""}))
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "No row filled in") {
		t.Fatalf("empty: %d", resp.StatusCode)
	}
	var rows [][4]string
	for i := 0; i <= maxBulkRows; i++ {
		rows = append(rows, [4]string{"d" + itoa(int64(i)), "https://192.0.2.1", "", ""})
	}
	resp, body = h.bulkPost(h.bulkForm(map[string]string{"kind": "link"}, rows...))
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Too many rows") || deviceCount(h) != 0 {
		t.Fatalf("too many: %d", resp.StatusCode)
	}
}

func TestBulkAccess(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	h.user("bob", store.RoleUser)
	h.login("bob")
	if resp, _ := h.get("/admin/devices/bulk"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user GET: %d", resp.StatusCode)
	}
	if resp, _ := h.bulkPost(h.bulkForm(nil, [4]string{"x", "https://192.0.2.1", "", ""})); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user POST: %d", resp.StatusCode)
	}
	h2 := newHarnessShared(t, h)
	h2.login("admin")
	form := h2.bulkForm(nil, [4]string{"x", "https://192.0.2.1", "", ""})
	form.Del("_csrf")
	if resp, _ := h2.bulkPost(form); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d", resp.StatusCode)
	}
	if deviceCount(h2) != 0 {
		t.Fatal("device created")
	}
}
