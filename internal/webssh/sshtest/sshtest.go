// Package sshtest provides fake SSH devices for tests of the SSH console. They
// are built on internal/legacyssh, the only implementation that can still offer
// the ssh-dss host keys of old ERS switches. Never use it outside tests.
package sshtest

import (
	"crypto/dsa" //nolint:staticcheck // ERS switches use DSA host keys; the fake device needs one
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	legacy "github.com/lan-dot-party/ers-proxy/internal/legacyssh"
)

// Login of every fake device.
const (
	User     = "admin"
	Password = "secret"
)

// Kind selects the algorithms a fake device offers.
type Kind int

const (
	// Modern: ed25519 host key, default algorithms.
	Modern Kind = iota
	// ERS: like an ERS 5600 – ssh-dss host key, diffie-hellman-group14/1-sha1,
	// aes128-cbc or 3des-cbc, hmac-sha1.
	ERS
	// OldERS: like ERS, but hmac-md5 only.
	OldERS
	// MD5Only: modern key exchange and cipher, hmac-md5 only.
	MD5Only
	// KbdInt: modern, keyboard-interactive login only.
	KbdInt
	// VOSS: like a VSP with VOSS – SHA-1 key exchanges only, RSA host key
	// signed with SHA-1 only (ssh-rsa), aes128-ctr.
	VOSS
)

var (
	dsaOnce   sync.Once
	dsaSigner legacy.Signer
	dsaErr    error
)

func dsaKey() (legacy.Signer, error) {
	dsaOnce.Do(func() {
		var k dsa.PrivateKey                                                                           //nolint:staticcheck // see import
		if dsaErr = dsa.GenerateParameters(&k.Parameters, rand.Reader, dsa.L1024N160); dsaErr != nil { //nolint:staticcheck // see import
			return
		}
		if dsaErr = dsa.GenerateKey(&k, rand.Reader); dsaErr != nil { //nolint:staticcheck // see import
			return
		}
		dsaSigner, dsaErr = legacy.NewSignerFromKey(&k)
	})
	return dsaSigner, dsaErr
}

// Device is a running fake SSH device. Its shell greets with "Enter Ctrl-Y to
// begin.", answers every input line (ended by CR or LF) with "echo:<line>"
// and ends on the line "exit".
type Device struct {
	Addr        string
	Fingerprint string       // SHA-256 fingerprint of its host key
	Logins      atomic.Int32 // password and keyboard-interactive attempts
	Sizes       chan [2]int  // pty-req and window-change sizes (cols, rows)
	ln          net.Listener
}

// Close stops the device.
func (d *Device) Close() error { return d.ln.Close() }

// Start starts a fake device on 127.0.0.1 (addr "" = a free port).
func Start(kind Kind, addr string) (*Device, error) {
	d := &Device{Sizes: make(chan [2]int, 64)}
	cfg := &legacy.ServerConfig{BannerCallback: func(legacy.ConnMetadata) string { return "Ethernet Routing Switch\r\n" }}
	var hostKey legacy.Signer
	switch kind {
	case ERS, OldERS:
		k, err := dsaKey()
		if err != nil {
			return nil, err
		}
		hostKey = k
		cfg.KeyExchanges = []string{"diffie-hellman-group14-sha1", "diffie-hellman-group1-sha1"}
		cfg.Ciphers = []string{"aes128-cbc", "3des-cbc"}
		cfg.MACs = []string{"hmac-sha1"}
		if kind == OldERS {
			cfg.MACs = []string{"hmac-md5"}
		}
	case VOSS:
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		s, err := legacy.NewSignerFromKey(priv)
		if err != nil {
			return nil, err
		}
		as, ok := s.(legacy.AlgorithmSigner)
		if !ok {
			return nil, errors.New("RSA signer without algorithms")
		}
		if hostKey, err = legacy.NewSignerWithAlgorithms(as, []string{legacy.KeyAlgoRSA}); err != nil {
			return nil, err
		}
		cfg.KeyExchanges = []string{"diffie-hellman-group14-sha1", "diffie-hellman-group1-sha1"}
		cfg.Ciphers = []string{"aes128-ctr"}
	default:
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		if hostKey, err = legacy.NewSignerFromKey(priv); err != nil {
			return nil, err
		}
		if kind == MD5Only {
			cfg.Ciphers = []string{"aes128-ctr"}
			cfg.MACs = []string{"hmac-md5"}
		}
	}
	cfg.AddHostKey(hostKey)
	d.Fingerprint = legacy.FingerprintSHA256(hostKey.PublicKey())
	check := func(user, pass string) (*legacy.Permissions, error) {
		d.Logins.Add(1)
		if user == User && pass == Password {
			return nil, nil
		}
		return nil, errors.New("denied")
	}
	if kind == KbdInt {
		cfg.KeyboardInteractiveCallback = func(c legacy.ConnMetadata, ask legacy.KeyboardInteractiveChallenge) (*legacy.Permissions, error) {
			a, err := ask("", "", []string{"Password: "}, []bool{false})
			if err != nil || len(a) != 1 {
				return nil, errors.New("denied")
			}
			return check(c.User(), a[0])
		}
	} else {
		cfg.PasswordCallback = func(c legacy.ConnMetadata, p []byte) (*legacy.Permissions, error) {
			return check(c.User(), string(p))
		}
	}
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	d.ln = ln
	d.Addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(c, cfg)
		}
	}()
	return d, nil
}

// New starts a fake device for a test and stops it at the end.
func New(t testing.TB, kind Kind) *Device {
	t.Helper()
	d, err := Start(kind, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func (d *Device) serve(c net.Conn, cfg *legacy.ServerConfig) {
	defer func() { _ = c.Close() }()
	_, chans, reqs, err := legacy.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go legacy.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(legacy.UnknownChannelType, "")
			continue
		}
		ch, in, err := nc.Accept()
		if err != nil {
			return
		}
		go d.session(ch, in)
	}
}

func (d *Device) size(cols, rows uint32) {
	select {
	case d.Sizes <- [2]int{int(cols), int(rows)}:
	default:
	}
}

func (d *Device) session(ch legacy.Channel, in <-chan *legacy.Request) {
	go func() {
		for r := range in {
			switch r.Type {
			case "pty-req": // string term, uint32 cols, uint32 rows, …
				if len(r.Payload) >= 4 {
					n := binary.BigEndian.Uint32(r.Payload)
					if uint64(len(r.Payload)) >= 4+uint64(n)+8 {
						p := r.Payload[4+n:]
						d.size(binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:]))
					}
				}
				_ = r.Reply(true, nil)
			case "window-change":
				if len(r.Payload) >= 8 {
					d.size(binary.BigEndian.Uint32(r.Payload), binary.BigEndian.Uint32(r.Payload[4:]))
				}
			case "shell":
				_ = r.Reply(true, nil)
			default:
				_ = r.Reply(false, nil)
			}
		}
	}()
	_, _ = io.WriteString(ch, "Enter Ctrl-Y to begin.\r\n")
	buf := make([]byte, 256)
	var line strings.Builder
	for {
		n, err := ch.Read(buf)
		if err != nil {
			return
		}
		for _, b := range buf[:n] {
			if b != '\r' && b != '\n' {
				if line.Len() < 4096 {
					line.WriteByte(b)
				}
				continue
			}
			if line.Len() == 0 {
				continue
			}
			if line.String() == "exit" {
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				_ = ch.Close()
				return
			}
			_, _ = io.WriteString(ch, "echo:"+line.String()+"\r\n")
			line.Reset()
		}
	}
}
