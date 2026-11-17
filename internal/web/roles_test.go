package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// Users never open devices: the stored device account usually has full
// rights, and opening logs in with it.
func TestUsersCannotOpenDevices(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	h.user("bob", store.RoleUser)
	h.fakeProbe()
	devs := []*store.Device{
		h.addDevice(&store.Device{Name: "ERS proxied", Kind: store.KindERS, URL: "http://ers.example"}),
		h.addDevice(&store.Device{Name: "VSP direct", Kind: store.KindEDMDirect, URL: "https://vsp.example"}),
		h.addDevice(&store.Device{Name: "FW link", Kind: store.KindLink, URL: "https://fw.example/"}),
	}

	u := newHarnessShared(t, h)
	u.login("bob")
	for _, d := range devs {
		if resp, _ := u.get("/open/" + itoa(d.ID)); resp.StatusCode != http.StatusForbidden {
			t.Errorf("user opened %s: %d, want 403", d.Name, resp.StatusCode)
		}
	}
	resp, body := u.get("/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workspace for user: %d", resp.StatusCode)
	}
	for _, d := range devs {
		if strings.Contains(body, d.Name) || strings.Contains(body, "/open/"+itoa(d.ID)) {
			t.Errorf("workspace shows %s to a user", d.Name)
		}
	}

	h.login("admin")
	resp, _ = h.get("/open/" + itoa(devs[2].ID))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin open link: %d", resp.StatusCode)
	}
	if _, body := h.get("/"); !strings.Contains(body, "FW link") {
		t.Error("workspace hides devices from admins")
	}
	entries, err := h.db.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Action == "device.open" && e.Target == "FW link" && e.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Error("opening a device is not audited")
	}
}
