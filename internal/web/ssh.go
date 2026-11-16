package web

// SSH console in the browser (roadmap phase 9). Only when ssh.enabled is set
// in config.yaml (Server.SSH != nil) and only for admins: the routes are
// registered inside the admin group, never otherwise. The browser talks to
// /ssh/{id}/ws over a WebSocket; the server holds the SSH connection
// (internal/webssh) and relays bytes. Nothing of a session is recorded except
// audit entries (who, which device, how long, how many bytes).

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/lan-dot-party/ers-proxy/internal/app"
	"github.com/lan-dot-party/ers-proxy/internal/auth"
	"github.com/lan-dot-party/ers-proxy/internal/config"
	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/store"
	"github.com/lan-dot-party/ers-proxy/internal/webssh"
)

// SSHConsole is the state of the SSH console; nil unless ssh.enabled is set.
type SSHConsole struct {
	cfg config.SSH
	// Dial overrides how the server connects to devices (tests).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	mu      sync.Mutex
	open    int
	pending map[string]pendingHostKey
}

// pendingHostKey is a host key shown to an admin for confirmation.
type pendingHostKey struct {
	deviceID, userID int64
	fingerprint      string
	expires          time.Time
}

// pendingHostKeyTTL is how long a shown host key can be confirmed.
const pendingHostKeyTTL = 10 * time.Minute

// sshPingInterval is how often a console pings the browser and checks the
// idle time and the portal session.
const sshPingInterval = 30 * time.Second

// maxSSHLoginAttempts is how many logins one console tries.
const maxSSHLoginAttempts = 3

// NewSSHConsole returns the console state for cfg.
func NewSSHConsole(cfg config.SSH) *SSHConsole {
	return &SSHConsole{cfg: cfg, pending: map[string]pendingHostKey{}}
}

func (c *SSHConsole) acquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open >= c.cfg.MaxSessions {
		return false
	}
	c.open++
	return true
}

func (c *SSHConsole) release() {
	c.mu.Lock()
	c.open--
	c.mu.Unlock()
}

// Open returns the number of open consoles.
func (c *SSHConsole) Open() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open
}

// offer remembers a host key shown to an admin and returns the nonce that
// confirms it.
func (c *SSHConsole) offer(deviceID, userID int64, fingerprint string) string {
	nonce := auth.RandomToken(16)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, p := range c.pending {
		if now.After(p.expires) {
			delete(c.pending, k)
		}
	}
	c.pending[nonce] = pendingHostKey{deviceID: deviceID, userID: userID, fingerprint: fingerprint, expires: now.Add(pendingHostKeyTTL)}
	return nonce
}

// take returns and forgets the host key confirmed with nonce.
func (c *SSHConsole) take(nonce string, deviceID, userID int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[nonce]
	delete(c.pending, nonce)
	if !ok || p.deviceID != deviceID || p.userID != userID || time.Now().After(p.expires) {
		return "", false
	}
	return p.fingerprint, true
}

// sshDevice loads the device of /ssh/{id} and checks that it offers the
// console (404 otherwise).
func (s *Server) sshDevice(w http.ResponseWriter, r *http.Request) (*store.Device, bool) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return nil, false
	}
	if !d.SSHEnabled || !devices.SSHSupported(d) {
		s.errorPage(w, r, http.StatusNotFound, "error.not_found")
		return nil, false
	}
	return d, true
}

type sshPageData struct {
	Device    *store.Device
	Nonce     string // style nonce for xterm.js
	NeedLogin bool   // no stored login: the console asks
	Login     string // effective login source
}

// sshPage renders the console. xterm.js creates <style> elements, so this
// page allows styles with a per-response nonce (set on them by ssh.js).
func (s *Server) sshPage(w http.ResponseWriter, r *http.Request) {
	d, ok := s.sshDevice(w, r)
	if !ok {
		return
	}
	nonce := auth.RandomToken(16)
	h := w.Header()
	h.Set("Content-Security-Policy", strings.Replace(h.Get("Content-Security-Policy"), "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1))
	h.Set("Cache-Control", "no-store")
	frameable(w)
	_, stored, _ := devices.SSHCredentials(s.Key, d)
	p := s.page(r, "", "ssh", sshPageData{Device: d, Nonce: nonce, NeedLogin: !stored, Login: devices.SSHLogin(d)})
	p.Title = d.Name + " · SSH"
	p.Wide = true
	s.render(w, r, "ssh", p)
}

// sshTrust pins a host key that the console showed to this admin.
func (s *Server) sshTrust(w http.ResponseWriter, r *http.Request) {
	d, ok := s.sshDevice(w, r)
	if !ok {
		return
	}
	u := app.User(r.Context())
	fp, ok := s.SSH.take(r.PostFormValue("nonce"), d.ID, u.ID)
	if !ok {
		http.Error(w, "expired", http.StatusConflict)
		return
	}
	stored, err := s.DB.SetDeviceSSHHostKey(r.Context(), d.ID, fp, "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !stored {
		http.Error(w, "a host key is already pinned", http.StatusConflict)
		return
	}
	s.Audit(r, "ssh.hostkey.trust", d.Name, fp)
	w.WriteHeader(http.StatusNoContent)
}

// Messages between console page and server (text frames, JSON). Terminal
// data travels in binary frames.
type sshMsg struct {
	T    string `json:"t"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// sshWS is the console's WebSocket. A WebSocket upgrade is a GET, which the
// csrf middleware does not check, so this handler checks Origin and the CSRF
// token (query parameter) itself.
func (s *Server) sshWS(w http.ResponseWriter, r *http.Request) {
	d, ok := s.sshDevice(w, r)
	if !ok {
		return
	}
	sess, u := app.Session(r.Context()), app.User(r.Context())
	if sess == nil || u == nil || r.Header.Get("Origin") == "" || !s.SameOrigin(r) ||
		subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("csrf")), []byte(sess.CSRFToken)) != 1 {
		s.Log.Warn("rejected SSH console connection", "device", d.Name, "origin", r.Header.Get("Origin"))
		s.errorPage(w, r, http.StatusForbidden, "error.csrf")
		return
	}
	if !s.SSH.acquire() {
		s.errorPage(w, r, http.StatusServiceUnavailable, "ssh.err.busy")
		return
	}
	defer s.SSH.release()
	// The server's read and write timeouts must not cut the console.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	origin, _ := url.Parse(s.Cfg.Origin())
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{origin.Host}})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	reason := s.sshConsole(ctx, ws, r, d, sess)
	_ = ws.Close(websocket.StatusNormalClosure, reason)
}

// sshConsole runs one console and returns why it ended.
func (s *Server) sshConsole(ctx context.Context, ws *websocket.Conn, r *http.Request, d *store.Device, sess *store.Session) (reason string) {
	defer func() {
		if p := recover(); p != nil {
			s.Log.Error("SSH console panic", "device", d.Name, "panic", p)
			reason = "internal error"
		}
	}()
	send := func(m map[string]any) {
		wctx, c := context.WithTimeout(ctx, 10*time.Second)
		defer c()
		_ = wsjson.Write(wctx, ws, m)
	}
	fail := func(key, detail string) string {
		send(map[string]any{"t": "error", "key": key, "detail": detail})
		return key
	}
	start, err := readMsg(ctx, ws, 30*time.Second)
	if err != nil || start.T != "start" {
		return "no start"
	}
	cred, stored, err := devices.SSHCredentials(s.Key, d)
	if err != nil {
		s.Log.Warn("open stored SSH login", "device", d.Name, "err", err)
	}
	source := "stored"
	why := "needed"
	var sh *webssh.Session
	for attempt := 1; ; attempt++ {
		if !stored {
			send(map[string]any{"t": "login", "reason": why})
			m, err := readMsg(ctx, ws, 5*time.Minute)
			if err != nil || m.T != "auth" || m.User == "" {
				return "no login"
			}
			cred, source = devices.Credentials{Username: m.User, Password: m.Pass}, "typed"
		}
		send(map[string]any{"t": "status", "key": "ssh.connecting"})
		sh, err = webssh.Open(ctx, webssh.Options{
			Addr: devices.SSHAddr(d), Username: cred.Username, Password: cred.Password,
			Profile: d.SSHProfile, AllowLegacy: devices.SSHLegacyAllowed(d), Pin: d.SSHHostKey,
			Timeout: s.SSH.cfg.ConnectTimeout, Cols: start.Cols, Rows: start.Rows, Dial: s.SSH.Dial,
		})
		if err == nil {
			break
		}
		var hk *webssh.HostKeyError
		var ae *webssh.AlgorithmError
		switch {
		case errors.As(err, &hk) && hk.Unknown():
			nonce := s.SSH.offer(d.ID, app.User(r.Context()).ID, hk.Fingerprint)
			send(map[string]any{"t": "hostkey", "fingerprint": hk.Fingerprint, "type": hk.Type, "nonce": nonce})
			return "host key unknown"
		case errors.As(err, &hk):
			s.Audit(r, "ssh.hostkey.mismatch", d.Name, "presented "+hk.Fingerprint+", pinned "+hk.Want)
			send(map[string]any{"t": "hostkey_changed", "fingerprint": hk.Fingerprint, "type": hk.Type, "want": hk.Want})
			return "host key changed"
		case errors.Is(err, webssh.ErrAuth):
			s.Audit(r, "ssh.login_failed", d.Name, "login="+source)
			if attempt >= maxSSHLoginAttempts {
				return fail("ssh.err.login", "")
			}
			stored, why = false, "rejected"
			continue
		case errors.As(err, &ae):
			return fail("ssh.err.algorithms", err.Error())
		case errors.Is(err, webssh.ErrLegacyNotAllowed):
			return fail("ssh.err.legacy", "")
		default:
			return fail("ssh.err.connect", err.Error())
		}
	}
	info := sh.Info()
	opened := time.Now()
	s.Audit(r, "ssh.open", d.Name, fmt.Sprintf("profile=%s kex=%s hostkey=%s cipher=%s mac=%s login=%s",
		info.Profile, info.KeyExchange, info.HostKey, info.Cipher, info.MAC, source))
	s.Log.Info("SSH console opened", "device", d.Name, "profile", info.Profile, "kex", info.KeyExchange,
		"hostkey", info.HostKey, "cipher", info.Cipher, "mac", info.MAC)
	send(map[string]any{"t": "connected", "profile": info.Profile, "kex": info.KeyExchange, "hostkey": info.HostKey,
		"cipher": info.Cipher, "mac": info.MAC, "fingerprint": info.Fingerprint})
	if info.Banner != "" {
		// A terminal needs CR LF; banners often end lines with LF only.
		banner := strings.ReplaceAll(strings.ReplaceAll(info.Banner, "\r\n", "\n"), "\n", "\r\n")
		_ = ws.Write(ctx, websocket.MessageBinary, []byte(banner))
	}

	var last, in, out atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan string, 3)
	end := func(why string) {
		select {
		case done <- why:
		default:
		}
	}
	go func() { // device → browser
		defer func() {
			if p := recover(); p != nil {
				end("internal error")
			}
		}()
		buf := make([]byte, 32<<10)
		for {
			n, err := sh.Read(buf)
			if n > 0 {
				if werr := ws.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					end("browser closed")
					return
				}
				out.Add(int64(n))
				last.Store(time.Now().UnixNano())
			}
			if err != nil {
				end("shell ended")
				return
			}
		}
	}()
	go func() { // browser → device
		defer func() {
			if p := recover(); p != nil {
				end("internal error")
			}
		}()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				end("browser closed")
				return
			}
			last.Store(time.Now().UnixNano())
			if typ == websocket.MessageBinary {
				if _, err := sh.Write(data); err != nil {
					end("shell ended")
					return
				}
				in.Add(int64(len(data)))
				continue
			}
			var m sshMsg
			if json.Unmarshal(data, &m) == nil && m.T == "resize" {
				_ = sh.Resize(m.Cols, m.Rows)
			}
		}
	}()
	idle := s.SSH.cfg.IdleTimeout
	tick := min(idle/4, sshPingInterval)
	t := time.NewTicker(tick)
	defer t.Stop()
	for reason == "" {
		select {
		case reason = <-done:
		case <-t.C:
			switch {
			case time.Since(time.Unix(0, last.Load())) > idle:
				reason = "idle"
			case !s.DB.SessionExists(ctx, sess.ID):
				reason = "logged out"
			default:
				// Keeps reverse proxies from closing a quiet console and
				// notices a browser that went away without closing.
				pctx, pc := context.WithTimeout(ctx, 15*time.Second)
				if err := ws.Ping(pctx); err != nil {
					reason = "browser closed"
				}
				pc()
			}
		}
	}
	send(map[string]any{"t": "closed", "reason": reason})
	_ = sh.Close()
	s.Audit(r, "ssh.close", d.Name, fmt.Sprintf("reason=%s duration=%s in=%d out=%d",
		reason, time.Since(opened).Round(time.Second), in.Load(), out.Load()))
	return reason
}

// readMsg reads the next text message within timeout.
func readMsg(ctx context.Context, ws *websocket.Conn, timeout time.Duration) (sshMsg, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		typ, data, err := ws.Read(rctx)
		if err != nil {
			return sshMsg{}, err
		}
		if typ != websocket.MessageText {
			continue // input typed before the shell exists
		}
		var m sshMsg
		if err := json.Unmarshal(data, &m); err != nil {
			return sshMsg{}, err
		}
		return m, nil
	}
}

// deviceSSH saves a device's SSH console settings and its own SSH login.
func (s *Server) deviceSSH(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	oldPort := d.SSHPort
	d.SSHEnabled = r.PostFormValue("ssh_enabled") == "1"
	d.SSHPort, _ = strconv.Atoi(strings.TrimSpace(r.PostFormValue("ssh_port")))
	d.SSHLogin = r.PostFormValue("ssh_login")
	d.SSHProfile = r.PostFormValue("ssh_profile")
	if err := devices.NormalizeSSH(d); err != nil {
		s.deviceForm(w, r, http.StatusUnprocessableEntity, d, false, fieldKey(err))
		return
	}
	login := devices.Credentials{Username: strings.TrimSpace(r.PostFormValue("ssh_username")), Password: r.PostFormValue("ssh_password")}
	withLogin := login.Username != "" || login.Password != ""
	if withLogin && (login.Username == "" || login.Password == "") {
		s.deviceForm(w, r, http.StatusUnprocessableEntity, d, false, "login_missing")
		return
	}
	if withLogin && !s.confirmPassword(r) {
		s.deviceForm(w, r, http.StatusUnprocessableEntity, d, false, "wrong_password")
		return
	}
	if err := s.DB.SetDeviceSSH(ctx, d.ID, d.SSHEnabled, d.SSHPort, d.SSHLogin, d.SSHProfile); err != nil {
		s.fail(w, r, err)
		return
	}
	details := fmt.Sprintf("enabled=%v port=%d login=%s profile=%s", d.SSHEnabled, d.SSHPort, devices.SSHLogin(d), d.SSHProfile)
	if oldPort != d.SSHPort {
		// The stored SSH login and the pinned key belong to the old port.
		if err := s.DB.SetDeviceSSHCredentials(ctx, d.ID, nil); err != nil {
			s.fail(w, r, err)
			return
		}
		if _, err := s.DB.SetDeviceSSHHostKey(ctx, d.ID, "", d.SSHHostKey); err != nil {
			s.fail(w, r, err)
			return
		}
		details += "; SSH login and host key cleared (port changed)"
	}
	s.Audit(r, "device.ssh", d.Name, details)
	switch {
	case withLogin:
		enc, err := devices.EncryptSSH(s.Key, d, login)
		if err == nil {
			err = s.DB.SetDeviceSSHCredentials(ctx, d.ID, enc)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.Audit(r, "device.ssh.credentials.set", d.Name, "") // never the values
	case r.PostFormValue("ssh_clear_login") == "1" && d.HasSSHCredentials():
		if err := s.DB.SetDeviceSSHCredentials(ctx, d.ID, nil); err != nil {
			s.fail(w, r, err)
			return
		}
		s.Audit(r, "device.ssh.credentials.clear", d.Name, "")
	}
	redirect(w, r, fmt.Sprintf("/admin/devices/%d/edit?ok=saved", d.ID))
}

// deviceSSHHostKeyClear forgets the pinned SSH host key (after a planned key
// change on the device); the next console shows the new key for confirmation.
func (s *Server) deviceSSHHostKeyClear(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if _, err := s.DB.SetDeviceSSHHostKey(r.Context(), d.ID, "", d.SSHHostKey); err != nil {
		s.fail(w, r, err)
		return
	}
	s.Audit(r, "ssh.hostkey.clear", d.Name, d.SSHHostKey)
	redirect(w, r, fmt.Sprintf("/admin/devices/%d/edit?ok=saved", d.ID))
}
