package webssh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lan-dot-party/ers-proxy/internal/webssh/sshtest"
)

const (
	testUser = sshtest.User
	testPass = sshtest.Password
)

// readUntil reads from s until want appears.
func readUntil(t *testing.T, s *Session, want string) string {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, 256)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in %q", want, out.String())
		}
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, out.String())
		}
	}
	return out.String()
}

func open(t *testing.T, o Options) *Session {
	t.Helper()
	if o.Username == "" {
		o.Username, o.Password = testUser, testPass
	}
	s, err := Open(context.Background(), o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestModernSession(t *testing.T) {
	d := sshtest.New(t, sshtest.Modern)
	s := open(t, Options{Addr: d.Addr, Pin: d.Fingerprint, Cols: 120, Rows: 30})
	if got := <-d.Sizes; got != [2]int{120, 30} {
		t.Errorf("pty size %v", got)
	}
	info := s.Info()
	if info.Profile != ProfileModern || info.HostKey != "ssh-ed25519" || info.Fingerprint != d.Fingerprint {
		t.Errorf("info %+v", info)
	}
	if !strings.Contains(info.Banner, "Ethernet Routing Switch") {
		t.Errorf("banner %q", info.Banner)
	}
	readUntil(t, s, "Ctrl-Y")
	if _, err := s.Write([]byte("show sys-info\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, "echo:show sys-info")
	if err := s.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	if got := <-d.Sizes; got != [2]int{100, 40} {
		t.Errorf("window change %v", got)
	}
	if err := s.Resize(0, 40); err == nil {
		t.Error("invalid size accepted")
	}
	_, _ = s.Write([]byte("exit\r"))
	select {
	case <-s.Done():
		if s.Err() != nil {
			t.Errorf("exit: %v", s.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
}

func TestERSFallsBackToLegacy(t *testing.T) {
	d := sshtest.New(t, sshtest.ERS)
	s := open(t, Options{Addr: d.Addr, Pin: d.Fingerprint, AllowLegacy: true})
	info := s.Info()
	if info.Profile != ProfileLegacy || info.HostKey != "ssh-dss" || info.MAC != "hmac-sha1" ||
		!strings.HasPrefix(info.KeyExchange, "diffie-hellman-group1") {
		t.Errorf("info %+v", info)
	}
	readUntil(t, s, "Ctrl-Y")
}

func TestOldERSWithHMACMD5(t *testing.T) {
	d := sshtest.New(t, sshtest.OldERS)
	s := open(t, Options{Addr: d.Addr, Pin: d.Fingerprint, Profile: ProfileLegacy, AllowLegacy: true})
	if info := s.Info(); info.MAC != "hmac-md5" || info.Cipher != "aes128-cbc" {
		t.Errorf("info %+v", info)
	}
	_, _ = s.Write([]byte("x\r"))
	readUntil(t, s, "echo:x")
}

func TestLegacyNeedsPermission(t *testing.T) {
	d := sshtest.New(t, sshtest.ERS)
	_, err := Open(context.Background(), Options{Addr: d.Addr, Username: testUser, Password: testPass, Pin: d.Fingerprint})
	var ae *AlgorithmError
	if !errors.As(err, &ae) || !strings.Contains(ae.What, "key exchange") ||
		!strings.Contains(strings.Join(ae.Offered, ","), "diffie-hellman-group14-sha1") {
		t.Fatalf("want a key exchange AlgorithmError, got %v", err)
	}
	if !strings.Contains(err.Error(), "the device offers diffie-hellman-group14-sha1") {
		t.Errorf("message %q", err)
	}
	_, err = Open(context.Background(), Options{Addr: d.Addr, Profile: ProfileLegacy})
	if !errors.Is(err, ErrLegacyNotAllowed) {
		t.Errorf("legacy profile without permission: %v", err)
	}
	if d.Logins.Load() != 0 {
		t.Errorf("%d logins sent", d.Logins.Load())
	}
}

func TestMD5OnlyNeedsLegacy(t *testing.T) {
	d := sshtest.New(t, sshtest.MD5Only)
	_, err := Open(context.Background(), Options{Addr: d.Addr, Username: testUser, Password: testPass, Pin: d.Fingerprint})
	var ae *AlgorithmError
	if !errors.As(err, &ae) || !strings.Contains(ae.What, "MAC") || ae.Offered[0] != "hmac-md5" {
		t.Fatalf("want a MAC AlgorithmError, got %v", err)
	}
	s := open(t, Options{Addr: d.Addr, Pin: d.Fingerprint, AllowLegacy: true})
	if s.Info().Profile != ProfileLegacy || s.Info().MAC != "hmac-md5" {
		t.Errorf("info %+v", s.Info())
	}
}

func TestHostKeyChecks(t *testing.T) {
	d := sshtest.New(t, sshtest.Modern)
	before := d.Logins.Load()
	_, err := Open(context.Background(), Options{Addr: d.Addr, Username: testUser, Password: testPass})
	var hk *HostKeyError
	if !errors.As(err, &hk) || !hk.Unknown() || hk.Fingerprint != d.Fingerprint || hk.Type != "ssh-ed25519" {
		t.Fatalf("unknown key: %v", err)
	}
	_, err = Open(context.Background(), Options{Addr: d.Addr, Username: testUser, Password: testPass,
		Pin: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	if !errors.As(err, &hk) || hk.Unknown() || hk.Fingerprint != d.Fingerprint {
		t.Fatalf("changed key: %v", err)
	}
	if n := d.Logins.Load() - before; n != 0 {
		t.Errorf("the password was sent %d times to an unverified device", n)
	}
}

func TestWrongPassword(t *testing.T) {
	d := sshtest.New(t, sshtest.Modern)
	_, err := Open(context.Background(), Options{Addr: d.Addr, Username: testUser, Password: "wrong", Pin: d.Fingerprint})
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
}

func TestKeyboardInteractive(t *testing.T) {
	d := sshtest.New(t, sshtest.KbdInt)
	s := open(t, Options{Addr: d.Addr, Pin: d.Fingerprint})
	readUntil(t, s, "Ctrl-Y")
}

func TestTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }() // accept, then say nothing
		}
	}()
	start := time.Now()
	_, err = Open(context.Background(), Options{Addr: ln.Addr().String(), Timeout: 300 * time.Millisecond})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start = time.Now()
	_, err = Open(ctx, Options{Addr: ln.Addr().String(), Timeout: time.Minute})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: err %v after %v", err, time.Since(start))
	}
}

func TestProfiles(t *testing.T) {
	for _, c := range []struct {
		profile string
		legacy  bool
		want    string
		err     bool
	}{
		{"", false, "modern", false},
		{"auto", true, "modern,legacy", false},
		{"modern", true, "modern", false},
		{"legacy", true, "legacy", false},
		{"legacy", false, "", true},
		{"other", true, "", true},
	} {
		got, err := Profiles(c.profile, c.legacy)
		if (err != nil) != c.err || strings.Join(got, ",") != c.want {
			t.Errorf("Profiles(%q, %v) = %v, %v", c.profile, c.legacy, got, err)
		}
	}
}
