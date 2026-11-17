package edmproxy

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/devices"
	"github.com/lan-dot-party/ers-proxy/internal/mockswitch"
	"github.com/lan-dot-party/ers-proxy/internal/store"
)

// fakeWebFig serves synthetic stand-ins for RouterOS's WebFig: a login page
// and an application page with absolute paths, a script that navigates with
// location.replace, and the binary /jsproxy endpoint. It records requests.
type fakeWebFig struct {
	mu    sync.Mutex
	paths []string
	body  []byte // last /jsproxy body
	ctype string // its Content-Type
}

func (f *fakeWebFig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	w.Header().Set("X-Frame-Options", "sameorigin")
	switch r.URL.Path {
	case "/":
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><link rel="stylesheet" href="/assets/style.css"><script src="/assets/script.js"></script>`+
			`<form id="login"></form><a href="/graphs">Graphs</a><a href="https://example.com/help">Help</a>`)
	case "/assets/script.js":
		w.Header().Set("Content-Type", "application/x-javascript")
		_, _ = io.WriteString(w, "function openWebFig() { window.location.replace(`/webfig/${window.location.hash}`); }")
	case "/webfig/":
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><head><script>if (location.pathname.endsWith("/webfig")) location.href = "/webfig/";</script>`+
			`<script src="master.js" type="module"></script></head><img src="/logo.png">`)
	case "/webfig/menu.jg":
		// Like RouterOS: only gzip-compressed.
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			http.Error(w, "not acceptable", http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		_, _ = io.WriteString(zw, "({menu:1})")
		_ = zw.Close()
	case "/jsproxy":
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.body, f.ctype = data, r.Header.Get("Content-Type")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte{0, 1, 2, 0xff})
	default:
		http.NotFound(w, r)
	}
}

func (e *env) addRouterOS(withLogin bool) (*fakeWebFig, int64) {
	e.t.Helper()
	fake := &fakeWebFig{}
	srv := httptest.NewServer(fake)
	e.t.Cleanup(srv.Close)
	ctx := context.Background()
	d := &store.Device{Name: "crs", Kind: store.KindRouterOS, URL: srv.URL, OpenMode: store.OpenInherit,
		Isolation: store.IsolationSandbox, TLSMode: store.TLSAuto}
	if err := e.db.CreateDevice(ctx, d); err != nil {
		e.t.Fatal(err)
	}
	if withLogin {
		enc, err := devices.EncryptCredentials(e.key, d, devices.Credentials{Username: "admin", Password: "pw"})
		if err != nil {
			e.t.Fatal(err)
		}
		if err := e.db.SetDeviceCredentials(ctx, d.ID, enc); err != nil {
			e.t.Fatal(err)
		}
	}
	return fake, d.ID
}

func TestRouterOSWebFigThroughProxy(t *testing.T) {
	e := setup(t, mockswitch.Options{})
	fake, id := e.addRouterOS(true)
	resp, _ := e.openID(id)
	base := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(base, "/d/") {
		t.Fatalf("open: %d %q", resp.StatusCode, base)
	}
	// No auto-login: opening sends nothing to the device.
	fake.mu.Lock()
	if len(fake.paths) != 0 {
		t.Errorf("requests on open: %v", fake.paths)
	}
	fake.mu.Unlock()

	resp, body := e.do("GET", base, "", doc)
	if resp.StatusCode != 200 || resp.Header.Get("X-Frame-Options") != "" ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox allow-scripts") {
		t.Fatalf("login page: %d %v", resp.StatusCode, resp.Header)
	}
	for _, want := range []string{`href="` + base + `assets/style.css"`, `src="` + base + `assets/script.js"`,
		`href="` + base + `graphs"`, `href="https://example.com/help"`, base + "__ersproxy/shim.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("login page lacks %q:\n%s", want, body)
		}
	}
	_, body = e.do("GET", base+"assets/script.js", "", map[string]string{"Sec-Fetch-Dest": "script"})
	if !strings.Contains(body, "window.location.replace(`"+base+"webfig/${window.location.hash}`)") {
		t.Errorf("script navigation not rebased: %s", body)
	}
	_, body = e.do("GET", base+"webfig/", "", doc)
	if !strings.Contains(body, `location.href = "`+base+`webfig/"`) || !strings.Contains(body, `src="`+base+`logo.png"`) ||
		!strings.Contains(body, `src="master.js"`) {
		t.Errorf("webfig page: %s", body)
	}
	// Menu files only come gzip-compressed; the browser unpacks them.
	req0, _ := http.NewRequest("GET", e.portal.URL+base+"webfig/menu.jg", nil)
	req0.Header.Set("Sec-Fetch-Dest", "empty")
	req0.Header.Set("Accept-Encoding", "gzip")
	raw := *e.client // same cookies, but no transparent gunzip
	raw.Transport = &http.Transport{DisableCompression: true}
	r0, err := raw.Do(req0)
	if err != nil {
		t.Fatal(err)
	}
	if r0.StatusCode != 200 || r0.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("menu.jg: %d %v", r0.StatusCode, r0.Header)
	}
	zr, err := gzip.NewReader(r0.Body)
	if err != nil {
		t.Fatal(err)
	}
	menu, _ := io.ReadAll(zr)
	r0.Body.Close()
	if string(menu) != "({menu:1})" {
		t.Fatalf("menu.jg body %q", menu)
	}
	// The shim maps XHR and fetch paths and element URLs for WebFig.
	_, shim := e.do("GET", base+"__ersproxy/shim.js", "", map[string]string{"Sec-Fetch-Dest": "script"})
	if !strings.Contains(shim, "if (true && window.XMLHttpRequest)") || !strings.Contains(shim, "var webApp = true;") {
		t.Fatalf("shim:\n%.800s", shim)
	}
	// WebFig's encrypted protocol: binary bodies pass through unchanged.
	req, _ := http.NewRequest("POST", e.portal.URL+base+"jsproxy", strings.NewReader("\x00\x01\xfe\xffbinary"))
	req.Header.Set("Content-Type", "msg")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Origin", "null")
	r2, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if r2.StatusCode != 200 || string(got) != "\x00\x01\x02\xff" || string(fake.body) != "\x00\x01\xfe\xffbinary" || fake.ctype != "msg" {
		t.Fatalf("jsproxy: %d %q, device got %q (%s)", r2.StatusCode, got, fake.body, fake.ctype)
	}
	if r2.Header.Get("Access-Control-Allow-Origin") != "null" {
		t.Errorf("jsproxy headers %v", r2.Header)
	}
}

// Without a stored login WebFig still opens (the user logs in there); the
// login only feeds the REST data source.
func TestRouterOSOpensWithoutStoredLogin(t *testing.T) {
	e := setup(t, mockswitch.Options{})
	_, id := e.addRouterOS(false)
	resp, _ := e.openID(id)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/d/") {
		t.Fatalf("open: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
