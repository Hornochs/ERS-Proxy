package web

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lan-dot-party/ers-proxy/internal/store"
)

func (h *harness) tryLogin(name, password, clientIP string) int {
	h.t.Helper()
	_, body := h.get("/login")
	m := reLoginCSRF.FindStringSubmatch(body)
	if m == nil {
		h.t.Fatal("no login CSRF token")
	}
	hdr := map[string]string{"Origin": h.srv.URL}
	if clientIP != "" {
		// The test server's peer is 127.0.0.1, a default trusted proxy.
		hdr["X-Forwarded-For"] = clientIP
	}
	return h.post("/login", url.Values{"username": {name}, "password": {password}, "_login_csrf": {m[1]}, "next": {"/"}}, hdr).StatusCode
}

// Failed logins from one client must not lock the account for everyone.
func TestLoginLockoutPerClient(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	for i := 0; i < 5; i++ {
		h.tryLogin("admin", "wrong", "198.51.100.7")
	}
	if code := h.tryLogin("admin", "correct horse battery", "198.51.100.7"); code != http.StatusTooManyRequests {
		t.Fatalf("attacker not locked: %d", code)
	}
	if code := h.tryLogin("admin", "correct horse battery", "192.0.2.10"); code != http.StatusSeeOther {
		t.Fatalf("admin locked out from another address: %d", code)
	}
}

// One client trying many usernames is locked after 20 failures.
func TestLoginLockoutPerIP(t *testing.T) {
	h := newHarness(t)
	h.user("admin", store.RoleAdmin)
	for i := 0; i < 20; i++ {
		h.tryLogin("user"+itoa(int64(i)), "wrong", "198.51.100.8")
	}
	if code := h.tryLogin("admin", "correct horse battery", "198.51.100.8"); code != http.StatusTooManyRequests {
		t.Fatalf("spraying client not locked: %d", code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/dashboard":           "/dashboard",
		"/maps/3?x=1":          "/maps/3?x=1",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"/\t/evil.example":     "/",
		"/\n/evil.example":     "/",
		"/%09/evil.example":    "/%09/evil.example", // still encoded: stays a local path
		"/d/abc/":              "/",
		"/a\\b":                "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
