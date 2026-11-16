// Package webssh opens interactive SSH shells on network devices for the
// browser terminal (roadmap phase 9). It only connects and moves bytes: who may
// open a shell, which login is used and how the session reaches the browser is
// decided by the web layer.
//
// Two SSH implementations are used:
//
//   - the current golang.org/x/crypto/ssh for every device: "modern" with its
//     default algorithms, "compat" with its insecure algorithms added (SHA-1
//     key exchanges, ssh-rsa with SHA-1, CBC ciphers) for older switches such
//     as VSPs with VOSS and ERS 5600,
//   - internal/legacyssh, a frozen copy of x/crypto/ssh v0.58.0 with ssh-dss
//     host keys, SHA-1 key exchanges and HMAC-MD5, for old Avaya ERS switches
//     only ("legacy" profile). The caller allows it per device
//     (Options.AllowLegacy); see internal/legacyssh/README.md.
//
// Host keys are pinned by their SHA-256 fingerprint (trust on first use): an
// unknown or changed key aborts the connection before the login is sent.
package webssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Algorithm profiles.
const (
	// ProfileAuto tries modern, then compat and, where allowed, legacy, each
	// only when the device offered no common algorithm.
	ProfileAuto = "auto"
	// ProfileModern uses the current x/crypto with its default algorithms.
	ProfileModern = "modern"
	// ProfileCompat uses the current x/crypto with its insecure algorithms
	// added (SHA-1 key exchanges, ssh-rsa, CBC ciphers).
	ProfileCompat = "compat"
	// ProfileLegacy uses the frozen copy with the insecure algorithms
	// enabled as well (ERS switches only).
	ProfileLegacy = "legacy"
)

// DefaultTimeout bounds connecting and the SSH handshake including the login.
const DefaultTimeout = 15 * time.Second

// maxBanner is the most bytes of pre-login banner kept for the terminal.
const maxBanner = 8 << 10

var (
	// ErrAuth means the device rejected the login.
	ErrAuth = errors.New("ssh: login rejected")
	// ErrLegacyNotAllowed means the legacy profile was asked for a device
	// that may not use it.
	ErrLegacyNotAllowed = errors.New("ssh: the legacy profile is only available for ERS switches")
)

// HostKeyError means the device's host key is not the pinned one. With an
// empty Want no key is pinned yet: the caller shows Fingerprint and asks an
// admin to confirm it. The login has not been sent in either case.
type HostKeyError struct {
	Type        string // key algorithm, e.g. "ssh-ed25519" or "ssh-dss"
	Fingerprint string // SHA-256 fingerprint the device presented ("SHA256:…")
	Want        string // pinned fingerprint, empty when none is pinned
}

// Unknown reports whether no host key was pinned yet.
func (e *HostKeyError) Unknown() bool { return e.Want == "" }

func (e *HostKeyError) Error() string {
	if e.Unknown() {
		return fmt.Sprintf("ssh: unknown host key %s (%s)", e.Fingerprint, e.Type)
	}
	return fmt.Sprintf("ssh: host key changed: device presented %s (%s), pinned is %s", e.Fingerprint, e.Type, e.Want)
}

// AlgorithmError means the device and ERS-Proxy have no algorithm in common
// for What ("key exchange", "host key", "client to server cipher", …).
type AlgorithmError struct {
	What      string
	Offered   []string // by the device
	Supported []string // by ERS-Proxy in the profile that was tried
}

func (e *AlgorithmError) Error() string {
	return fmt.Sprintf("ssh: no common algorithm for %s: the device offers %s; ERS-Proxy supports %s",
		e.What, strings.Join(e.Offered, ", "), strings.Join(e.Supported, ", "))
}

// Options describe one connection.
type Options struct {
	Addr     string // host:port
	Username string
	Password string
	// Profile is ProfileAuto (default), ProfileModern or ProfileLegacy.
	Profile string
	// AllowLegacy permits the legacy profile (set only for ERS switches).
	AllowLegacy bool
	// Pin is the pinned host key fingerprint ("SHA256:…"), empty if none.
	Pin string
	// Timeout for connecting, handshake and login (default DefaultTimeout).
	Timeout time.Duration
	// Cols and Rows of the terminal (default 80×24).
	Cols, Rows int
	// Dial connects to Addr; nil uses a net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Info describes an open session.
type Info struct {
	Profile     string // the profile that connected
	KeyExchange string
	HostKey     string // host key algorithm
	Cipher      string // client to server
	MAC         string // client to server (empty for AEAD ciphers)
	Fingerprint string // SHA-256 fingerprint of the host key
	Banner      string // pre-login banner, at most 8 KiB
}

// Profiles returns the profiles Open tries, in order.
func Profiles(profile string, allowLegacy bool) ([]string, error) {
	switch profile {
	case "", ProfileAuto:
		if allowLegacy {
			return []string{ProfileModern, ProfileCompat, ProfileLegacy}, nil
		}
		return []string{ProfileModern, ProfileCompat}, nil
	case ProfileModern:
		return []string{ProfileModern}, nil
	case ProfileCompat:
		return []string{ProfileCompat}, nil
	case ProfileLegacy:
		if !allowLegacy {
			return nil, ErrLegacyNotAllowed
		}
		return []string{ProfileLegacy}, nil
	}
	return nil, fmt.Errorf("ssh: unknown profile %q", profile)
}

// Open connects, logs in and starts a shell on a pseudo terminal. With
// ProfileAuto it falls back to the legacy profile (where allowed) only when
// the modern one finds no common algorithm.
func Open(ctx context.Context, o Options) (*Session, error) {
	profiles, err := Profiles(o.Profile, o.AllowLegacy)
	if err != nil {
		return nil, err
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Cols <= 0 || o.Rows <= 0 {
		o.Cols, o.Rows = 80, 24
	}
	for i, p := range profiles {
		var s *Session
		switch p {
		case ProfileLegacy:
			s, err = dialLegacy(ctx, &o, true)
		case ProfileCompat:
			s, err = dialModern(ctx, &o, true)
		default:
			s, err = dialModern(ctx, &o, false)
		}
		if err == nil {
			s.info.Profile = p
			return s, nil
		}
		var ae *AlgorithmError
		if errors.As(err, &ae) && i+1 < len(profiles) {
			continue
		}
		return nil, err
	}
	return nil, err
}

// Session is an interactive shell. Read returns the terminal output (stdout
// and stderr together, as a pseudo terminal delivers them), Write sends input.
// Read returns io.EOF when the shell ends.
type Session struct {
	in      io.WriteCloser
	out     *io.PipeReader
	resize  func(cols, rows int) error
	closeFn func() error

	info Info
	done chan struct{}
	once sync.Once
	err  error
}

func newSession(in io.WriteCloser, out *io.PipeReader, info Info, resize func(cols, rows int) error, closeFn func() error) *Session {
	return &Session{in: in, out: out, info: info, resize: resize, closeFn: closeFn, done: make(chan struct{})}
}

// Read implements io.Reader.
func (s *Session) Read(p []byte) (int, error) { return s.out.Read(p) }

// Write implements io.Writer.
func (s *Session) Write(p []byte) (int, error) { return s.in.Write(p) }

// Resize tells the device the new terminal size.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return fmt.Errorf("ssh: invalid terminal size %dx%d", cols, rows)
	}
	return s.resize(cols, rows)
}

// Info describes the session.
func (s *Session) Info() Info { return s.info }

// Done is closed when the shell has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is the reason the shell ended (nil for a normal exit); valid after Done.
func (s *Session) Err() error { return s.err }

// Close ends the session and the connection.
func (s *Session) Close() error {
	err := s.closeFn()
	s.finish(nil)
	return err
}

func (s *Session) finish(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
	})
}

// dial opens the TCP connection.
func dial(ctx context.Context, o *Options) (net.Conn, error) {
	d := o.Dial
	if d == nil {
		d = (&net.Dialer{Timeout: o.Timeout}).DialContext
	}
	return d(ctx, "tcp", o.Addr)
}

// checkHostKey compares a presented fingerprint with the pin.
func checkHostKey(keyType, fingerprint, pin string) error {
	if pin == "" || fingerprint != pin {
		return &HostKeyError{Type: keyType, Fingerprint: fingerprint, Want: pin}
	}
	return nil
}

// answers fills keyboard-interactive prompts: echoed questions get the user
// name, hidden ones the password.
func answers(o *Options, questions []string, echos []bool) []string {
	out := make([]string, len(questions))
	for i := range questions {
		if i < len(echos) && echos[i] {
			out[i] = o.Username
		} else {
			out[i] = o.Password
		}
	}
	return out
}

// appendNew appends the elements of extra that are not in list yet.
func appendNew(list, extra []string) []string {
	out := append([]string(nil), list...)
	for _, e := range extra {
		found := false
		for _, l := range out {
			if l == e {
				found = true
				break
			}
		}
		if !found {
			out = append(out, e)
		}
	}
	return out
}

// bannerBuffer keeps the first maxBanner bytes of the pre-login banner.
type bannerBuffer struct{ b strings.Builder }

func (bb *bannerBuffer) add(m string) error {
	if room := maxBanner - bb.b.Len(); room > 0 {
		if len(m) > room {
			m = m[:room]
		}
		bb.b.WriteString(m)
	}
	return nil
}

// isAuthError reports whether err is the client's "no method left" error.
func isAuthError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unable to authenticate")
}
