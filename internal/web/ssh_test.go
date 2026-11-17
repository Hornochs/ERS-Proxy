package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/lan-dot-party/ers-proxy/internal/config"
	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/store"
	"github.com/lan-dot-party/ers-proxy/internal/webssh/sshtest"
)

func sshHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, func(s *Server) {
		s.SSH = NewSSHConsole(config.SSH{Enabled: true, IdleTimeout: time.Minute, ConnectTimeout: 5 * time.Second, MaxSessions: 2})
	})
}

// sshDevice adds a device whose SSH server is the fake device dev.
func (h *harness) sshDevice(kind string, dev *sshtest.Device, login string, withWebLogin bool) *store.Device {
	h.t.Helper()
	_, port, _ := strings.Cut(dev.Addr, ":")
	p, _ := strconv.Atoi(port)
	d := &store.Device{Name: "ssh-" + kind, Kind: kind, URL: "http://127.0.0.1", OpenMode: store.OpenInherit,
		Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto}
	if kind == store.KindLink {
		d.URL = "http://127.0.0.1/"
	}
	ctx := context.Background()
	if err := h.db.CreateDevice(ctx, d); err != nil {
		h.t.Fatal(err)
	}
	if err := h.db.SetDeviceSSH(ctx, d.ID, true, p, login, "auto"); err != nil {
		h.t.Fatal(err)
	}
	if withWebLogin {
		got, _ := h.db.DeviceByID(ctx, d.ID)
		enc, err := devices.EncryptCredentials(h.ui.Key, got, devices.Credentials{Username: sshtest.User, Password: sshtest.Password})
		if err != nil {
			h.t.Fatal(err)
		}
		if err := h.db.SetDeviceCredentials(ctx, d.ID, enc); err != nil {
			h.t.Fatal(err)
		}
	}
	d, _ = h.db.DeviceByID(ctx, d.ID)
	return d
}

func (h *harness) dialSSH(id int64, csrf, origin string) (*websocket.Conn, *http.Response, error) {
	u := strings.Replace(h.srv.URL, "http://", "ws://", 1) + fmt.Sprintf("/ssh/%d/ws?csrf=%s", id, url.QueryEscape(csrf))
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: h.client, HTTPHeader: hdr})
}

// next reads messages until a text message arrives; binary data is collected.
func next(t *testing.T, c *websocket.Conn, out *strings.Builder) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (output so far %q)", err, out.String())
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m["t"] == "status" {
			continue
		}
		return m
	}
}

// waitOutput reads binary data until want appears.
func waitOutput(t *testing.T, c *websocket.Conn, out *strings.Builder, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for !strings.Contains(out.String(), want) {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("no %q in %q: %v", want, out.String(), err)
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
		}
	}
}

func TestSSHConsoleOffByDefault(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	h.login("admin")
	dev := sshtest.New(t, sshtest.Modern)
	d := h.sshDevice(store.KindERS, dev, "", true)
	if resp, _ := h.get(fmt.Sprintf("/ssh/%d", d.ID)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("console without ssh.enabled: %d", resp.StatusCode)
	}
	if _, body := h.get(fmt.Sprintf("/admin/devices/%d/edit", d.ID)); strings.Contains(body, `id="ssh-form"`) {
		t.Fatal("SSH settings shown without ssh.enabled")
	}
	if _, body := h.get("/"); strings.Contains(body, "data-ssh=") {
		t.Fatal("SSH action in the sidebar without ssh.enabled")
	}
}

func TestSSHConsoleERS(t *testing.T) {
	h := sshHarness(t)
	h.user("admin", store.RoleAdmin)
	h.user("bob", store.RoleUser)
	dev := sshtest.New(t, sshtest.ERS)
	d := h.sshDevice(store.KindERS, dev, "", true)

	// Users cannot open it.
	h.login("bob")
	if resp, _ := h.get(fmt.Sprintf("/ssh/%d", d.ID)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user opened the console page: %d", resp.StatusCode)
	}
	if _, resp, err := h.dialSSH(d.ID, h.csrf, h.srv.URL); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user opened the console: %v %v", err, resp)
	}
	if _, body := h.get("/"); strings.Contains(body, "data-ssh=") {
		t.Fatal("SSH action shown to a user")
	}

	jar, _ := cookiejar.New(nil)
	h.client.Jar = jar
	h.login("admin")
	resp, body := h.get(fmt.Sprintf("/ssh/%d", d.ID))
	csp := resp.Header.Get("Content-Security-Policy")
	if resp.StatusCode != 200 || !strings.Contains(csp, "style-src 'self' 'nonce-") || strings.Contains(csp, "unsafe-inline") ||
		!strings.Contains(csp, "frame-ancestors 'self'") || !strings.Contains(body, "/static/vendor/xterm/xterm.js") {
		t.Fatalf("page: %d %q", resp.StatusCode, csp)
	}
	if _, body := h.get("/"); !strings.Contains(body, fmt.Sprintf(`data-ssh="%d"`, d.ID)) {
		t.Fatal("no SSH action in the admin's sidebar")
	}
	// Origin and CSRF token are checked.
	if _, resp, err := h.dialSSH(d.ID, "wrong", h.srv.URL); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong CSRF token: %v", err)
	}
	if _, resp, err := h.dialSSH(d.ID, h.csrf, "http://evil.example"); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: %v", err)
	}
	if _, resp, err := h.dialSSH(d.ID, h.csrf, ""); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no origin: %v", err)
	}

	// First connection: unknown host key, nothing sent.
	c, _, err := h.dialSSH(d.ID, h.csrf, h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	ctx := context.Background()
	_ = wsjson.Write(ctx, c, map[string]any{"t": "start", "cols": 100, "rows": 30})
	m := next(t, c, &out)
	if m["t"] != "hostkey" || m["fingerprint"] != dev.Fingerprint || m["type"] != "ssh-dss" {
		t.Fatalf("want host key, got %v", m)
	}
	if dev.Logins.Load() != 0 {
		t.Fatal("login sent before the host key was confirmed")
	}
	_ = c.CloseNow()
	// A wrong nonce does not pin anything; the right one does.
	r := h.post(fmt.Sprintf("/ssh/%d/hostkey", d.ID), url.Values{"nonce": {"x"}}, map[string]string{"Origin": h.srv.URL, "X-CSRF-Token": h.csrf})
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("wrong nonce: %d", r.StatusCode)
	}
	r = h.post(fmt.Sprintf("/ssh/%d/hostkey", d.ID), url.Values{"nonce": {m["nonce"].(string)}}, map[string]string{"Origin": h.srv.URL, "X-CSRF-Token": h.csrf})
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("trust: %d", r.StatusCode)
	}
	if got, _ := h.db.DeviceByID(ctx, d.ID); got.SSHHostKey != dev.Fingerprint {
		t.Fatalf("pin %q", got.SSHHostKey)
	}

	// Now the console connects with the legacy profile and the web login.
	c, _, err = h.dialSSH(d.ID, h.csrf, h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	_ = wsjson.Write(ctx, c, map[string]any{"t": "start", "cols": 100, "rows": 30})
	m = next(t, c, &out)
	if m["t"] != "connected" || m["profile"] != "legacy" || m["hostkey"] != "ssh-dss" {
		t.Fatalf("want connected (legacy), got %v", m)
	}
	waitOutput(t, c, &out, "Ctrl-Y")
	if !strings.Contains(out.String(), "Ethernet Routing Switch") {
		t.Errorf("no banner in %q", out.String())
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("show sys-info\r"))
	waitOutput(t, c, &out, "echo:show sys-info")
	_ = wsjson.Write(ctx, c, map[string]any{"t": "resize", "cols": 132, "rows": 43})
	deadline := time.After(5 * time.Second)
	for got := [2]int{}; got != [2]int{132, 43}; {
		select {
		case got = <-dev.Sizes:
		case <-deadline:
			t.Fatal("resize did not reach the device")
		}
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("exit\r"))
	if m := next(t, c, &out); m["t"] != "closed" {
		t.Fatalf("want closed, got %v", m)
	}
	time.Sleep(100 * time.Millisecond)
	entries, err := h.db.ListAudit(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		seen[e.Action] = e.Details
		if strings.Contains(e.Details, sshtest.Password) {
			t.Fatalf("password in the audit log: %+v", e)
		}
	}
	for _, a := range []string{"ssh.hostkey.trust", "ssh.open", "ssh.close"} {
		if _, ok := seen[a]; !ok {
			t.Errorf("no %s audit entry (have %v)", a, seen)
		}
	}
	if !strings.Contains(seen["ssh.open"], "profile=legacy") || !strings.Contains(seen["ssh.close"], "reason=shell ended") {
		t.Errorf("audit details %q / %q", seen["ssh.open"], seen["ssh.close"])
	}
}

func TestSSHConsoleAsksForLogin(t *testing.T) {
	h := sshHarness(t)
	h.user("admin", store.RoleAdmin)
	h.login("admin")
	dev := sshtest.New(t, sshtest.KbdInt)
	d := h.sshDevice(store.KindLink, dev, "ask", false)
	if _, err := h.db.SetDeviceSSHHostKey(context.Background(), d.ID, dev.Fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	c, _, err := h.dialSSH(d.ID, h.csrf, h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	ctx := context.Background()
	var out strings.Builder
	_ = wsjson.Write(ctx, c, map[string]any{"t": "start", "cols": 80, "rows": 24})
	if m := next(t, c, &out); m["t"] != "login" || m["reason"] != "needed" {
		t.Fatalf("want login, got %v", m)
	}
	_ = wsjson.Write(ctx, c, map[string]any{"t": "auth", "user": sshtest.User, "pass": "wrong"})
	if m := next(t, c, &out); m["t"] != "login" || m["reason"] != "rejected" {
		t.Fatalf("want login again, got %v", m)
	}
	_ = wsjson.Write(ctx, c, map[string]any{"t": "auth", "user": sshtest.User, "pass": sshtest.Password})
	if m := next(t, c, &out); m["t"] != "connected" || m["profile"] != "modern" {
		t.Fatalf("want connected, got %v", m)
	}
	waitOutput(t, c, &out, "Ctrl-Y")
}

func TestSSHLegacyOnlyForERS(t *testing.T) {
	h := sshHarness(t)
	h.user("admin", store.RoleAdmin)
	h.login("admin")
	dev := sshtest.New(t, sshtest.ERS)
	d := h.sshDevice(store.KindRouterOS, dev, "", true) // a RouterOS device offering only ERS algorithms
	if _, err := h.db.SetDeviceSSHHostKey(context.Background(), d.ID, dev.Fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	c, _, err := h.dialSSH(d.ID, h.csrf, h.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	var out strings.Builder
	_ = wsjson.Write(context.Background(), c, map[string]any{"t": "start", "cols": 80, "rows": 24})
	m := next(t, c, &out)
	if m["t"] != "error" || m["key"] != "ssh.err.algorithms" || !strings.Contains(fmt.Sprint(m["detail"]), "the device offers ssh-dss") {
		t.Fatalf("want an algorithm error, got %v", m)
	}
	if dev.Logins.Load() != 0 {
		t.Fatal("login sent")
	}
}

func TestDeviceSSHSettings(t *testing.T) {
	h := sshHarness(t)
	h.user("admin", store.RoleAdmin)
	h.login("admin")
	dev := sshtest.New(t, sshtest.Modern)
	d := h.sshDevice(store.KindVSP, dev, "", false)
	ctx := context.Background()
	if _, err := h.db.SetDeviceSSHHostKey(ctx, d.ID, dev.Fingerprint, ""); err != nil {
		t.Fatal(err)
	}
	hdr := map[string]string{"Origin": h.srv.URL}
	path := fmt.Sprintf("/admin/devices/%d/ssh", d.ID)
	// Legacy is refused for a VSP; an own login needs the admin's password.
	if r := h.post(path, url.Values{"_csrf": {h.csrf}, "ssh_enabled": {"1"}, "ssh_port": {"22"}, "ssh_profile": {"legacy"}}, hdr); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("legacy on VSP: %d", r.StatusCode)
	}
	if r := h.post(path, url.Values{"_csrf": {h.csrf}, "ssh_enabled": {"1"}, "ssh_port": {fmt.Sprint(d.SSHPort)}, "ssh_profile": {"auto"},
		"ssh_username": {"rwa"}, "ssh_password": {"pw"}, "confirm_password": {"wrong"}}, hdr); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("own login without password: %d", r.StatusCode)
	}
	if r := h.post(path, url.Values{"_csrf": {h.csrf}, "ssh_enabled": {"1"}, "ssh_port": {fmt.Sprint(d.SSHPort)}, "ssh_profile": {"auto"},
		"ssh_username": {"rwa"}, "ssh_password": {"pw"}, "confirm_password": {"correct horse battery"}}, hdr); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("own login: %d", r.StatusCode)
	}
	got, _ := h.db.DeviceByID(ctx, d.ID)
	if c, err := devices.DecryptSSH(h.ui.Key, got); err != nil || c.Username != "rwa" || got.SSHHostKey == "" {
		t.Fatalf("stored: %+v %v %q", c, err, got.SSHHostKey)
	}
	// A new port clears the SSH login and the host key.
	if r := h.post(path, url.Values{"_csrf": {h.csrf}, "ssh_enabled": {"1"}, "ssh_port": {"2222"}, "ssh_profile": {"auto"}}, hdr); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("port change: %d", r.StatusCode)
	}
	got, _ = h.db.DeviceByID(ctx, d.ID)
	if got.SSHPort != 2222 || got.HasSSHCredentials() || got.SSHHostKey != "" {
		t.Fatalf("after port change: port %d login %v key %q", got.SSHPort, got.HasSSHCredentials(), got.SSHHostKey)
	}
	if _, body := h.get(fmt.Sprintf("/admin/devices/%d/edit", d.ID)); !strings.Contains(body, `id="ssh-form"`) ||
		strings.Contains(body, "old ERS, insecure") || strings.Contains(body, "alte ERS, unsicher") {
		t.Fatal("SSH card missing or offers legacy for a VSP")
	}
}
